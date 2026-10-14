package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type RootBudget struct {
	AutoRetry        bool `json:"autoRetry"`
	MaxSeconds       int  `json:"maxSeconds"`
	MaxModelCalls    int  `json:"maxModelCalls"`
	MaxToolCalls     int  `json:"maxToolCalls"`
	MaxTokens        int  `json:"maxTokens"`
	MaxPlannerRounds int  `json:"maxPlannerRounds"`
	MaxAttempts      int  `json:"maxAttempts"`
	MaxRetries       int  `json:"maxRetries"`
}

func (b RootBudget) Effective() RootBudget {
	if b.MaxSeconds == 0 {
		b.MaxSeconds = 600
	}
	if b.MaxModelCalls == 0 {
		b.MaxModelCalls = 64
	}
	if b.MaxToolCalls == 0 {
		b.MaxToolCalls = 128
	}
	if b.MaxPlannerRounds == 0 {
		b.MaxPlannerRounds = 12
	}
	if b.MaxAttempts == 0 {
		b.MaxAttempts = 24
	}
	if b.MaxRetries == 0 {
		b.MaxRetries = 2
	}
	return b
}
func (b RootBudget) Validate() error {
	for _, n := range []int{b.MaxSeconds, b.MaxModelCalls, b.MaxToolCalls, b.MaxTokens, b.MaxPlannerRounds, b.MaxAttempts, b.MaxRetries} {
		if n < 0 {
			return errors.New("根预算不能为负数")
		}
	}
	b = b.Effective()
	if b.MaxSeconds > 3600 || b.MaxModelCalls > 1000 || b.MaxToolCalls > 10000 || b.MaxTokens > 10000000 || b.MaxPlannerRounds > 100 || b.MaxAttempts > 200 || b.MaxRetries > 5 {
		return errors.New("根预算超过宿主上限")
	}
	return nil
}

type BudgetState struct {
	Limits       RootBudget `json:"limits"`
	ModelCalls   int        `json:"modelCalls"`
	ToolCalls    int        `json:"toolCalls"`
	Tokens       int        `json:"tokens"`
	UnknownUsage int        `json:"unknownUsage"`
	Deadline     string     `json:"deadline"`
}
type CoordinationState struct {
	EventSeq   int    `json:"eventSeq"`
	PlannedSeq int    `json:"plannedSeq"`
	Rounds     int    `json:"rounds"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
}
type Artifact struct {
	ID           string `json:"id"`
	TaskID       string `json:"taskId"`
	RunID        string `json:"runId"`
	MessageID    string `json:"messageId"`
	SHA256       string `json:"sha256"`
	Verification string `json:"verification"`
}

func rootInTx(ctx context.Context, tx *sql.Tx, run string) (string, error) {
	seen := map[string]bool{}
	for {
		if seen[run] {
			return "", errors.New("父运行链循环")
		}
		seen[run] = true
		var parent sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT parent_run_id FROM runs WHERE id=?`, run).Scan(&parent); err != nil {
			return "", err
		}
		if !parent.Valid || parent.String == "" {
			return run, nil
		}
		run = parent.String
	}
}
func (s *Store) RootRunID(ctx context.Context, run string) (string, error) {
	var root string
	err := s.withTx(ctx, func(tx *sql.Tx) error { var err error; root, err = rootInTx(ctx, tx, run); return err })
	return root, err
}
func initCoordination(ctx context.Context, tx *sql.Tx, run string, b RootBudget) error {
	b = b.Effective()
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	deadline := time.Now().UTC().Add(time.Duration(b.MaxSeconds) * time.Second).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO root_budgets(root_run_id,limits,deadline) VALUES(?,?,?)`, run, string(raw), deadline); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_coordination(root_run_id,updated_at) VALUES(?,?)`, run, now())
	return err
}
func budgetInTx(ctx context.Context, tx *sql.Tx, root string) (BudgetState, error) {
	var b BudgetState
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT limits,model_calls,tool_calls,tokens,unknown_usage,deadline FROM root_budgets WHERE root_run_id=?`, root).Scan(&raw, &b.ModelCalls, &b.ToolCalls, &b.Tokens, &b.UnknownUsage, &b.Deadline)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &b.Limits)
		b.Limits = b.Limits.Effective()
	}
	return b, err
}
func coordinationInTx(ctx context.Context, tx *sql.Tx, root string) (CoordinationState, error) {
	var c CoordinationState
	err := tx.QueryRowContext(ctx, `SELECT event_seq,planned_seq,rounds,state,reason FROM task_coordination WHERE root_run_id=?`, root).Scan(&c.EventSeq, &c.PlannedSeq, &c.Rounds, &c.State, &c.Reason)
	return c, err
}
func signalCoordination(ctx context.Context, tx *sql.Tx, root, kind string) error {
	var seq int
	if err := tx.QueryRowContext(ctx, `UPDATE task_coordination SET event_seq=event_seq+1,updated_at=? WHERE root_run_id=? RETURNING event_seq`, now(), root).Scan(&seq); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events(root_run_id,seq,kind,created_at) VALUES(?,?,?,?)`, root, seq, kind, now())
	return err
}
func (s *Store) BeginPlannerRound(ctx context.Context, root string) (TaskGraph, error) {
	var g TaskGraph
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		g, err = taskGraphInTx(ctx, tx, root)
		if err != nil {
			return err
		}
		if g.RunStatus != "running" {
			return errors.New("根任务未运行")
		}
		if g.Coordination.Rounds >= g.Budget.Limits.MaxPlannerRounds {
			return errors.New("根任务规划轮次预算耗尽")
		}
		_, err = tx.ExecContext(ctx, `UPDATE task_coordination SET rounds=rounds+1,state='planning',updated_at=? WHERE root_run_id=?`, now(), root)
		return err
	})
	return g, err
}
func (s *Store) AcknowledgePlanner(ctx context.Context, root string, seq int, state, reason string) error {
	switch state {
	case "continue", "waiting", "blocked", "failed", "complete":
	default:
		return errors.New("无效规划结论")
	}
	if len(reason) > 4000 {
		return errors.New("规划说明过长")
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE task_coordination SET planned_seq=MAX(planned_seq,?),state=?,reason=?,updated_at=? WHERE root_run_id=? AND event_seq>=?`, seq, state, reason, now(), root, seq)
		return err
	})
}
func taskSatisfied(g TaskGraph, id string) bool {
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, item := range g.Tasks {
			if item.Spec.ID == id {
				if item.State == "succeeded" {
					return true
				}
				if item.State != "failed" {
					return false
				}
			}
		}
		for _, item := range g.Tasks {
			if item.Spec.Replaces == id && visit(item.Spec.ID) {
				return true
			}
		}
		return false
	}
	return visit(id)
}
func TaskSatisfied(g TaskGraph, id string) bool { return taskSatisfied(g, id) }
func ReadyTasks(g TaskGraph) []TaskItem {
	var ready []TaskItem
	for _, item := range g.Tasks {
		if item.State != "pending" {
			continue
		}
		ok := true
		for _, dep := range item.Spec.DependsOn {
			if !taskSatisfied(g, dep) {
				ok = false
			}
		}
		if ok {
			ready = append(ready, item)
		}
	}
	return ready
}
func validateTaskContracts(ctx context.Context, tx *sql.Tx, g TaskGraph, r RunResources, specs []TaskSpec) error {
	replacements := map[string]string{}
	for _, spec := range specs {
		if spec.Replaces != "" {
			if old, ok := replacements[spec.Replaces]; ok && old != spec.ID {
				return errors.New("同一失败任务只能有一个后继重试")
			}
			replacements[spec.Replaces] = spec.ID
		}
	}
	existing := map[string]TaskItem{}
	for _, item := range g.Tasks {
		existing[item.Spec.ID] = item
	}
	for _, spec := range specs {
		switch spec.AccessMode {
		case "", "read", "write", "unknown":
		default:
			return errors.New("accessMode 必须为 read、write 或 unknown")
		}
		if len(spec.Resources) > 16 {
			return errors.New("任务资源声明最多 16 项")
		}
		for _, key := range spec.Resources {
			if strings.TrimSpace(key) == "" || len(key) > 200 {
				return errors.New("无效资源声明")
			}
		}
		if spec.Replaces != "" {
			if !g.Budget.Limits.AutoRetry {
				return errors.New("起始配置未允许自动重试")
			}
			old, ok := existing[spec.Replaces]
			if !ok || old.State != "failed" || old.Spec.Acceptance != spec.Acceptance {
				return errors.New("重试只能替代已失败任务，并保留验收条件")
			}
			depth := 1
			for old.Spec.Replaces != "" {
				old = existing[old.Spec.Replaces]
				depth++
			}
			if depth > g.Budget.Limits.MaxRetries {
				return errors.New("根任务重试次数耗尽")
			}
			var effects int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tool_calls WHERE run_id IN(SELECT run_id FROM task_attempts WHERE root_run_id=? AND task_id=?) AND approved_at<>'' AND tool_name NOT IN ('local__current_time','local__read_file','local__load_skill','local__search_worker_traces','local__list_worker_traces','local__get_worker_trace','local__record_observation')`, g.RootRunID, spec.Replaces).Scan(&effects); err != nil {
				return err
			}
			if effects > 0 {
				return errors.New("失败任务可能已有副作用，不能自动重试")
			}
		}
	}
	return nil
}
func validateClaim(ctx context.Context, tx *sql.Tx, g TaskGraph, spec TaskSpec, run string) error {
	if len(g.Attempts) >= g.Budget.Limits.MaxAttempts {
		return errors.New("根任务执行尝试预算耗尽")
	}
	var unknown int
	if err := tx.QueryRowContext(ctx, `WITH RECURSIVE tree(id) AS(SELECT ? UNION ALL SELECT r.id FROM runs r JOIN tree ON r.parent_run_id=tree.id) SELECT count(*) FROM tool_calls WHERE run_id IN tree AND state='execution_unknown'`, g.RootRunID).Scan(&unknown); err != nil {
		return err
	}
	if unknown > 0 {
		return errors.New("根任务仍有未知工具效果，禁止继续执行")
	}
	for _, item := range g.Tasks {
		if item.State == "running" && (spec.AccessMode != "read" || item.Spec.AccessMode != "read") {
			return errors.New("写入或未知副作用任务需要串行")
		}
	}
	return nil
}
func (s *Store) RunTaskSpec(ctx context.Context, run string) (TaskSpec, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT i.spec FROM task_items i JOIN task_attempts a ON a.root_run_id=i.root_run_id AND a.task_id=i.id WHERE a.run_id=?`, run).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskSpec{}, false, nil
	}
	var spec TaskSpec
	if err == nil {
		err = json.Unmarshal([]byte(raw), &spec)
	}
	return spec, err == nil, err
}
func recordArtifact(ctx context.Context, tx *sql.Tx, root, task, run, status string) error {
	if status != "succeeded" {
		return nil
	}
	var msg, content string
	err := tx.QueryRowContext(ctx, `SELECT id,content FROM messages WHERE run_id=? AND role='assistant' AND tool_calls IN ('','[]') ORDER BY seq DESC LIMIT 1`, run).Scan(&msg, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(content))
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_artifacts(id,root_run_id,task_id,run_id,message_id,sha256,verification,created_at) VALUES(?,?,?,?,?,?,'unreviewed',?) ON CONFLICT(run_id) DO NOTHING`, id, root, task, run, msg, hex.EncodeToString(sum[:]), now())
	return err
}
func artifactsInTx(ctx context.Context, tx *sql.Tx, root string) ([]Artifact, error) {
	items := []Artifact{}
	rows, err := tx.QueryContext(ctx, `SELECT id,task_id,run_id,message_id,sha256,verification FROM task_artifacts WHERE root_run_id=? ORDER BY created_at,id`, root)
	if err != nil {
		return items, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.TaskID, &a.RunID, &a.MessageID, &a.SHA256, &a.Verification); err != nil {
			return items, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func checkBudget(b BudgetState) error {
	deadline, err := time.Parse(time.RFC3339Nano, b.Deadline)
	if err != nil {
		return err
	}
	if time.Now().After(deadline) {
		return errors.New("根任务时间预算耗尽")
	}
	return nil
}
func reserveModel(ctx context.Context, tx *sql.Tx, run, step, payload string) error {
	root, err := rootInTx(ctx, tx, run)
	if err != nil {
		return err
	}
	b, err := budgetInTx(ctx, tx, root)
	if err != nil {
		return err
	}
	var ended int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE id IN (?,?) AND status<>'running'`, root, run).Scan(&ended); err != nil {
		return err
	}
	if ended > 0 {
		return errors.New("当前或根任务已结束，不能消耗预算")
	}
	if err := checkBudget(b); err != nil {
		return err
	}
	if b.ModelCalls >= b.Limits.MaxModelCalls {
		return errors.New("根模型调用预算耗尽")
	}
	reserved := len(payload) + 8192
	if b.Limits.MaxTokens > 0 && b.Tokens+reserved > b.Limits.MaxTokens {
		return errors.New("根 token 预算不足以预留本次请求")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE root_budgets SET model_calls=model_calls+1,tokens=tokens+? WHERE root_run_id=?`, reserved, root); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO model_usage(step_id,root_run_id,reserved) VALUES(?,?,?)`, step, root, reserved)
	return err
}
func reserveTool(ctx context.Context, tx *sql.Tx, call string) error {
	var run string
	if err := tx.QueryRowContext(ctx, `SELECT run_id FROM tool_calls WHERE id=?`, call).Scan(&run); err != nil {
		return err
	}
	root, err := rootInTx(ctx, tx, run)
	if err != nil {
		return err
	}
	b, err := budgetInTx(ctx, tx, root)
	if err != nil {
		return err
	}
	var ended int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE id IN (?,?) AND status<>'running'`, root, run).Scan(&ended); err != nil {
		return err
	}
	if ended > 0 {
		return errors.New("当前或根任务已结束，不能消耗预算")
	}
	if err := checkBudget(b); err != nil {
		return err
	}
	if b.ToolCalls >= b.Limits.MaxToolCalls {
		return errors.New("根工具调用预算耗尽")
	}
	_, err = tx.ExecContext(ctx, `UPDATE root_budgets SET tool_calls=tool_calls+1 WHERE root_run_id=?`, root)
	return err
}
func (s *Store) RecordUsage(ctx context.Context, run string, tokens int) error {
	var exceeded bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var step, root string
		var reserved int
		err := tx.QueryRowContext(ctx, `SELECT u.step_id,u.root_run_id,u.reserved FROM model_usage u JOIN steps s ON s.id=u.step_id WHERE s.run_id=? AND u.settled=0 ORDER BY s.seq DESC LIMIT 1`, run).Scan(&step, &root, &reserved)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		known := 0
		if tokens > 0 {
			known = 1
		} else {
			tokens = reserved
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_usage SET tokens=?,known=?,settled=1 WHERE step_id=?`, tokens, known, step); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE root_budgets SET tokens=tokens+?,unknown_usage=unknown_usage+? WHERE root_run_id=?`, tokens-reserved, 1-known, root)
		if err != nil {
			return err
		}
		budget, err := budgetInTx(ctx, tx, root)
		if err != nil {
			return err
		}
		exceeded = budget.Limits.MaxTokens > 0 && budget.Tokens > budget.Limits.MaxTokens
		return nil
	})
	if err != nil {
		return err
	}
	if exceeded {
		return errors.New("实际模型用量超过根 token 预算")
	}
	return nil
}
func (s *Store) NotifyCoordination(ctx context.Context, root, kind string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return signalCoordination(ctx, tx, root, kind) })
}

func settleUnknownUsage(ctx context.Context, tx *sql.Tx, step string) error {
	var root string
	err := tx.QueryRowContext(ctx, `SELECT root_run_id FROM model_usage WHERE step_id=? AND settled=0`, step).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_usage SET settled=1,known=0,tokens=reserved WHERE step_id=?`, step); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE root_budgets SET unknown_usage=unknown_usage+1 WHERE root_run_id=?`, root)
	return err
}

func recoverCoordination(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT step_id FROM model_usage WHERE settled=0`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := settleUnknownUsage(ctx, tx, id); err != nil {
			return err
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT c.root_run_id FROM task_coordination c JOIN runs r ON r.id=c.root_run_id WHERE r.status='interrupted' AND c.state NOT IN ('blocked','failed','waiting')`)
	if err != nil {
		return err
	}
	ids = nil
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE task_coordination SET state='blocked',reason='宿主中断，已保留事件和尝试；先处置未知结果，再明确继续任务',updated_at=? WHERE root_run_id=?`, now(), id); err != nil {
			return err
		}
		if err := signalCoordination(ctx, tx, id, "recovery"); err != nil {
			return err
		}
	}
	return nil
}
func signalContextChange(ctx context.Context, tx *sql.Tx, localContext, kind string) error {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE tree(root,id) AS(SELECT t.run_id,t.run_id FROM task_roots t JOIN runs r ON r.id=t.run_id WHERE r.status='running' UNION ALL SELECT tree.root,r.id FROM runs r JOIN tree ON r.parent_run_id=tree.id) SELECT DISTINCT tree.root FROM tree JOIN runs r ON r.id=tree.id WHERE (SELECT context_id FROM sessions WHERE id=r.session_id)=?`, localContext)
	if err != nil {
		return err
	}
	var roots []string
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			rows.Close()
			return err
		}
		roots = append(roots, root)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, root := range roots {
		if err := signalCoordination(ctx, tx, root, kind); err != nil {
			return err
		}
	}
	return nil
}

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Development schema: initialize current storage directly, with no upgrade chain.
const schemaSQL = `CREATE TABLE context_memories (
		id TEXT PRIMARY KEY,
		context_id TEXT NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		source TEXT NOT NULL CHECK(source IN ('model','user')),
		scope TEXT NOT NULL,
		source_session_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
CREATE TABLE contexts (id TEXT PRIMARY KEY, name TEXT NOT NULL, policy TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE exfil_grants(context_id TEXT NOT NULL REFERENCES contexts(id) ON DELETE CASCADE, destination TEXT NOT NULL, scopes TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(context_id,destination));
CREATE TABLE mcp_endpoints (name TEXT PRIMARY KEY, command TEXT NOT NULL, binary_sha256 TEXT NOT NULL, schema_sha256 TEXT NOT NULL, version TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL);
CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, role TEXT NOT NULL CHECK(role IN ('user','assistant','tool')), content TEXT NOT NULL, created_at TEXT NOT NULL, run_id TEXT NOT NULL DEFAULT '', seq INTEGER NOT NULL DEFAULT 0, tool_calls TEXT NOT NULL DEFAULT '', provider_state TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(provider_state)));
CREATE TABLE model_usage(step_id TEXT PRIMARY KEY REFERENCES steps(id) ON DELETE CASCADE,root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,reserved INTEGER NOT NULL,tokens INTEGER NOT NULL DEFAULT 0,known INTEGER NOT NULL DEFAULT 0,settled INTEGER NOT NULL DEFAULT 0);
CREATE TABLE profile_revisions(profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,revision INTEGER NOT NULL,name TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),created_at TEXT NOT NULL,PRIMARY KEY(profile_id,revision));
CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, revision INTEGER NOT NULL, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, archived_at TEXT,config TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(config)));
CREATE TABLE review_requests(run_id TEXT PRIMARY KEY REFERENCES task_roots(run_id) ON DELETE CASCADE,target_run_id TEXT NOT NULL REFERENCES task_roots(run_id));
CREATE TABLE root_budgets(root_run_id TEXT PRIMARY KEY REFERENCES task_roots(run_id) ON DELETE CASCADE,limits TEXT NOT NULL CHECK(json_valid(limits)),model_calls INTEGER NOT NULL DEFAULT 0,tool_calls INTEGER NOT NULL DEFAULT 0,tokens INTEGER NOT NULL DEFAULT 0,unknown_usage INTEGER NOT NULL DEFAULT 0,deadline TEXT NOT NULL);
CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, status TEXT NOT NULL CHECK(status IN ('running','completed','cancelled','failed','interrupted')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', profile_revision INTEGER NOT NULL DEFAULT 1, profile_id TEXT NOT NULL DEFAULT '', profile_snapshot TEXT NOT NULL DEFAULT '', parent_run_id TEXT, depth INTEGER NOT NULL DEFAULT 0, component TEXT NOT NULL DEFAULT 'main' CHECK(component IN ('main','worker','planner','reviewer')), component_version INTEGER NOT NULL DEFAULT 1, resources_snapshot TEXT NOT NULL DEFAULT '', worker_name TEXT NOT NULL DEFAULT '');
CREATE TABLE session_context_scopes(session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, scope TEXT NOT NULL, PRIMARY KEY(session_id,scope));
CREATE TABLE session_plan_revisions(session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,revision INTEGER NOT NULL,plan TEXT NOT NULL CHECK(json_valid(plan)),created_at TEXT NOT NULL,PRIMARY KEY(session_id,revision));
CREATE TABLE session_plans (
		session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
		plan TEXT NOT NULL CHECK(json_valid(plan)),
		updated_at TEXT NOT NULL
	, revision INTEGER NOT NULL DEFAULT 1);
CREATE TABLE session_summaries (
		session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
		covers_seq INTEGER NOT NULL,
		covers_calls INTEGER NOT NULL DEFAULT 0,
		summary TEXT NOT NULL,
		scope TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL, context_id TEXT NOT NULL REFERENCES contexts(id), kind TEXT NOT NULL CHECK(kind IN ('normal','delegation')), profile_id TEXT NOT NULL REFERENCES profiles(id), options TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(options)));
CREATE TABLE steps (id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE, seq INTEGER NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('model','tool')), status TEXT NOT NULL CHECK(status IN ('pending','running','succeeded','failed','cancelled','paused')), error TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(run_id, seq));
CREATE TABLE task_artifacts(id TEXT PRIMARY KEY,root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,task_id TEXT NOT NULL,run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,sha256 TEXT NOT NULL,verification TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE task_attempts(id TEXT PRIMARY KEY,root_run_id TEXT NOT NULL,task_id TEXT NOT NULL,run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','cancelled','interrupted','execution_unknown')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,FOREIGN KEY(root_run_id,task_id) REFERENCES task_items(root_run_id,id) ON DELETE CASCADE,UNIQUE(root_run_id,task_id));
CREATE TABLE task_coordination(root_run_id TEXT PRIMARY KEY REFERENCES task_roots(run_id) ON DELETE CASCADE,event_seq INTEGER NOT NULL DEFAULT 1,planned_seq INTEGER NOT NULL DEFAULT 0,rounds INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL DEFAULT 'pending',reason TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);
CREATE TABLE task_events(root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,seq INTEGER NOT NULL,kind TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(root_run_id,seq));
CREATE TABLE task_items(root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,id TEXT NOT NULL,position INTEGER NOT NULL,spec TEXT NOT NULL CHECK(json_valid(spec)),state TEXT NOT NULL CHECK(state IN ('pending','running','succeeded','failed','cancelled','interrupted','execution_unknown')),PRIMARY KEY(root_run_id,id));
CREATE TABLE task_plan_revisions(root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,revision INTEGER NOT NULL,plan TEXT NOT NULL CHECK(json_valid(plan)),created_at TEXT NOT NULL,PRIMARY KEY(root_run_id,revision));
CREATE TABLE task_reviews(id TEXT PRIMARY KEY,root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,reviewer_run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,binding TEXT NOT NULL,result TEXT NOT NULL CHECK(json_valid(result)),created_at TEXT NOT NULL);
CREATE TABLE task_roots(run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,goal TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL DEFAULT 0);
CREATE TABLE tool_calls (id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE, step_id TEXT NOT NULL UNIQUE REFERENCES steps(id), message_id TEXT NOT NULL REFERENCES messages(id), call_id TEXT NOT NULL, tool_name TEXT NOT NULL, arguments TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('pending','approved','executing','succeeded','failed','cancelled','execution_unknown')), result TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, approval_source TEXT NOT NULL DEFAULT '', approved_at TEXT NOT NULL DEFAULT '', args_hash TEXT NOT NULL DEFAULT '', source_scope TEXT NOT NULL DEFAULT '', UNIQUE(run_id, call_id));
CREATE TABLE search_requests(id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,step_id TEXT NOT NULL REFERENCES steps(id) ON DELETE CASCADE,provider TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('executing','completed','execution_unknown')),reserved INTEGER NOT NULL,payload TEXT NOT NULL DEFAULT '{"events":[]}' CHECK(json_valid(payload)),error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL);
CREATE TABLE worker_observations(id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,content TEXT NOT NULL,source_scope TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX context_memories_order ON context_memories(context_id, created_at, id);
CREATE INDEX messages_session_order ON messages(session_id, created_at, id);
CREATE UNIQUE INDEX messages_session_seq ON messages(session_id, seq);
CREATE INDEX runs_session_order ON runs(session_id, created_at, id);
CREATE INDEX runs_parent_order ON runs(parent_run_id, created_at, id);
CREATE INDEX sessions_context_kind_order ON sessions(context_id, kind, created_at, id);
CREATE INDEX sessions_context_order ON sessions(context_id, created_at, id);
CREATE INDEX steps_run_order ON steps(run_id, seq);
CREATE INDEX task_reviews_root ON task_reviews(root_run_id,created_at);
CREATE INDEX tool_calls_run_order ON tool_calls(run_id, created_at, id);
CREATE INDEX tool_calls_session ON tool_calls(message_id);
CREATE INDEX tool_calls_state ON tool_calls(state);
CREATE TRIGGER sessions_context_reassign BEFORE UPDATE OF context_id ON sessions WHEN EXISTS (SELECT 1 FROM messages WHERE session_id=OLD.id) OR EXISTS (SELECT 1 FROM runs WHERE session_id=OLD.id) BEGIN SELECT RAISE(ABORT, 'session with history cannot change context'); END;
CREATE TRIGGER runs_parent_context BEFORE INSERT ON runs WHEN NEW.parent_run_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM runs p JOIN sessions ps ON ps.id=p.session_id JOIN sessions cs ON cs.id=NEW.session_id WHERE p.id=NEW.parent_run_id AND ps.context_id=cs.context_id) BEGIN SELECT RAISE(ABORT,'child run context mismatch'); END;
CREATE TRIGGER runs_identity_immutable BEFORE UPDATE OF session_id,parent_run_id ON runs WHEN NEW.session_id IS NOT OLD.session_id OR NEW.parent_run_id IS NOT OLD.parent_run_id BEGIN SELECT RAISE(ABORT,'run ownership is immutable'); END;
CREATE TRIGGER messages_run_session BEFORE INSERT ON messages WHEN NEW.run_id<>'' AND NOT EXISTS(SELECT 1 FROM runs WHERE id=NEW.run_id AND session_id=NEW.session_id) BEGIN SELECT RAISE(ABORT,'message run session mismatch'); END;
CREATE TRIGGER messages_identity_immutable BEFORE UPDATE OF session_id,run_id ON messages WHEN NEW.session_id IS NOT OLD.session_id OR NEW.run_id IS NOT OLD.run_id BEGIN SELECT RAISE(ABORT,'message ownership is immutable'); END;
PRAGMA user_version=21;`

func (s *Store) initialize() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == currentVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("数据库格式 %d 不再支持；开发阶段不迁移旧数据，请使用新的数据目录（当前格式 %d）", version, currentVersion)
	}
	return s.withTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(schemaSQL); err != nil {
			return err
		}
		d := normalizeProfile(DefaultProfile())
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO profiles(id,name,revision,is_default,created_at,updated_at,config) VALUES(?,?,1,1,?,?,?)`, DefaultProfileID, "默认配置", now(), now(), string(raw)); err != nil {
			return err
		}
		if err := saveProfileRevision(context.Background(), tx, DefaultProfileID, 1, "默认配置", d); err != nil {
			return err
		}
		return nil
	})
}
func saveProfileConfig(ctx context.Context, tx *sql.Tx, id string, data ProfileData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE profiles SET config=? WHERE id=?`, string(raw), id)
	return err
}
func saveProfileRevision(ctx context.Context, tx *sql.Tx, id string, revision int, name string, data ProfileData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO profile_revisions(profile_id,revision,name,payload,created_at) VALUES(?,?,?,?,?)`, id, revision, name, string(raw), now())
	return err
}

const (
	SessionKindNormal     = "normal"
	SessionKindDelegation = "delegation"
)

func DefaultProfile() ProfileData {
	d := BlankProfile()
	d.Description = "通用助手"
	d.SystemPrompt = "你是 ICEPick 助手。使用可用工具完成用户任务；无法完成时如实说明。"
	d.AllMCPServers = true
	d.AllSkills = true
	d.ContextBudget = 8192
	d.MemoryEnabled = true
	d.PlanEnabled = true
	d.Delegation = DelegationPolicy{Enabled: true, MaxDepth: 1}
	return d
}

package ipc

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"icepick/internal/app"
	"icepick/internal/storage"
)

// Server owns the host side of the IPC: one Unix socket per data directory,
// token-based handshake with a same-UID peer check, request dispatch to the
// application service and event broadcast. Clients never touch the database
// or MCP processes directly, and a disconnect never cancels a run.
type Server struct {
	svc        *app.Service
	socketPath string
	tokenPath  string
	token      string

	mu       sync.Mutex
	closed   bool
	conns    map[*connBox]bool
	listener net.Listener
}

type connBox struct {
	conn   net.Conn
	write  sync.Mutex
	events chan app.Event
	stop   func()
	done   chan struct{}
}

// Start prepares the socket and token inside dataDir and begins accepting.
// The host lock already guarantees a single host per data directory, so a
// stale socket file from a crashed host can be removed safely.
func Start(svc *app.Service, dataDir string) (*Server, error) {
	if svc == nil {
		return nil, errors.New("应用服务未就绪")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("生成宿主令牌: %w", err)
	}
	server := &Server{
		svc:        svc,
		socketPath: filepath.Join(dataDir, SocketName),
		tokenPath:  filepath.Join(dataDir, TokenName),
		token:      hex.EncodeToString(tokenBytes),
		conns:      map[*connBox]bool{},
	}
	if err := os.WriteFile(server.tokenPath, []byte(server.token+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("写入宿主令牌: %w", err)
	}
	os.Remove(server.socketPath)
	listener, err := net.Listen("unix", server.socketPath)
	if err != nil {
		return nil, fmt.Errorf("监听 IPC 套接字: %w", err)
	}
	if err := os.Chmod(server.socketPath, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("收紧 IPC 套接字权限: %w", err)
	}
	server.listener = listener
	go server.acceptLoop()
	return server, nil
}

// Close stops accepting, disconnects clients and removes the socket and the
// token file.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	listener := s.listener
	boxes := make([]*connBox, 0, len(s.conns))
	for box := range s.conns {
		boxes = append(boxes, box)
	}
	s.conns = map[*connBox]bool{}
	s.mu.Unlock()

	var err error
	if listener != nil {
		err = listener.Close()
	}
	for _, box := range boxes {
		box.conn.Close()
		box.stop()
		<-box.done
	}
	os.Remove(s.socketPath)
	os.Remove(s.tokenPath)
	return err
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReaderSize(conn, 1<<20)
	var hello Hello
	line, err := readLine(reader)
	if err != nil {
		conn.Close()
		return
	}
	if err := json.Unmarshal(line, &hello); err != nil || hello.Hello != "icepick" || hello.Version != ProtocolVersion || hello.Token != s.token || !samePeer(conn) {
		writeLine(conn, Response{Error: "IPC 身份校验失败"})
		conn.Close()
		return
	}
	conn.SetDeadline(time.Time{})
	if err := writeLine(conn, Welcome{Welcome: "icepick", Version: ProtocolVersion}); err != nil {
		conn.Close()
		return
	}

	box := &connBox{conn: conn, events: make(chan app.Event, 256), done: make(chan struct{})}
	box.stop = s.svc.Subscribe(func(evt app.Event) {
		select {
		case box.events <- evt:
		default:
			// The client reconciles from persisted state; a dropped event
			// only costs immediacy, never correctness.
		}
	})
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		box.stop()
		conn.Close()
		return
	}
	s.conns[box] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, box)
		s.mu.Unlock()
		box.stop()
		close(box.done)
		conn.Close()
	}()
	go box.pump()

	for {
		line, err := readLine(reader)
		if err != nil {
			return
		}
		var request Request
		if err := json.Unmarshal(line, &request); err != nil {
			_ = box.writeResponse(Response{Error: "请求格式无效"})
			continue
		}
		result, handlerErr := s.dispatch(context.Background(), request)
		response := Response{ID: request.ID}
		if handlerErr != nil {
			response.Error = handlerErr.Error()
		} else if result != nil {
			encoded, err := json.Marshal(result)
			if err != nil {
				response.Error = fmt.Sprintf("编码结果失败: %v", err)
			} else {
				response.Result = encoded
			}
		}
		if err := box.writeResponse(response); err != nil {
			return
		}
	}
}

func (b *connBox) pump() {
	for evt := range b.events {
		encoded, err := json.Marshal(EventLine{Event: evt})
		if err != nil {
			continue
		}
		if !b.writeLine(encoded) {
			return
		}
	}
}

func (b *connBox) writeResponse(response Response) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if !b.writeLine(encoded) {
		return io.EOF
	}
	return nil
}

func (b *connBox) writeLine(payload []byte) bool {
	b.write.Lock()
	defer b.write.Unlock()
	b.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := b.conn.Write(append(payload, '\n')); err != nil {
		return false
	}
	return true
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line[:len(line)-1], nil
}

func writeLine(conn net.Conn, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = conn.Write(encoded)
	return err
}

// dispatch routes one request to the application service. Every method maps
// to the exact call the desktop UI makes, so ownership checks, approval
// rules and exfiltration gates behave identically over IPC.
func (s *Server) dispatch(ctx context.Context, request Request) (any, error) {
	switch request.Method {
	case "status":
		return s.svc.GetStatus(), nil

	case "profiles.list":
		return s.svc.ListProfiles()
	case "profiles.get":
		var params struct {
			ID string `json:"id"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.GetProfile(params.ID)
	case "profiles.create":
		var params struct {
			Name   string `json:"name"`
			FromID string `json:"fromId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.CreateProfile(params.Name, params.FromID)
	case "profiles.update":
		var params struct {
			Profile storage.Profile `json:"profile"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.UpdateProfile(params.Profile)
	case "profiles.restore":
		var params struct {
			Revision int `json:"revision"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.RestoreProfileDefault(params.Revision)
	case "profiles.delete":
		var params struct {
			ID string `json:"id"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return nil, s.svc.DeleteProfile(params.ID)
	case "profiles.export":
		var params struct {
			ID string `json:"id"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ExportProfile(params.ID)
	case "profiles.importPreview":
		var params struct {
			Data string `json:"data"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(params.Data)
		if err != nil {
			return nil, fmt.Errorf("分享包数据必须是 base64: %w", err)
		}
		return s.svc.PreviewProfileImport(raw)
	case "profiles.import":
		var params struct {
			Data string `json:"data"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(params.Data)
		if err != nil {
			return nil, fmt.Errorf("分享包数据必须是 base64: %w", err)
		}
		created, preview, err := s.svc.ImportProfile(raw)
		if err != nil {
			return nil, err
		}
		return map[string]any{"profile": created, "preview": preview}, nil

	case "agents.list":
		return s.svc.ListAgents()
	case "agents.profile":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.GetAgentProfile(params.AgentID)
	case "agents.create":
		var params struct {
			Name      string `json:"name"`
			ProfileID string `json:"profileId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.CreateAgent(params.Name, params.ProfileID)
	case "agents.update":
		var params struct {
			AgentID string              `json:"agentId"`
			Name    string              `json:"name"`
			Policy  storage.AgentPolicy `json:"policy"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.UpdateAgent(params.AgentID, params.Name, params.Policy)
	case "agents.switchProfile":
		var params struct {
			AgentID   string `json:"agentId"`
			ProfileID string `json:"profileId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.SwitchAgentProfile(params.AgentID, params.ProfileID)
	case "agents.delete":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return nil, s.svc.DeleteAgent(params.AgentID)

	case "sessions.list":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListAgentSessions(params.AgentID)
	case "sessions.create":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.CreateAgentSession(params.AgentID)
	case "sessions.messages":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListMessages(params.AgentID, params.SessionID)

	case "runs.list":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListRuns(params.AgentID, params.SessionID)
	case "runs.get":
		var params struct {
			AgentID string `json:"agentId"`
			RunID   string `json:"runId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.GetRun(params.AgentID, params.RunID)
	case "runs.children":
		var params struct {
			AgentID string `json:"agentId"`
			RunID   string `json:"runId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListChildRuns(params.AgentID, params.RunID)

	case "tasks.start":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
			Input     string `json:"input"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.StartAgentTask(params.AgentID, params.SessionID, params.Input)
	case "tasks.cancel":
		var params struct {
			AgentID string `json:"agentId"`
			RunID   string `json:"runId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return nil, s.svc.CancelTask(params.AgentID, params.RunID)
	case "stream.pending":
		var params struct {
			AgentID string `json:"agentId"`
			RunID   string `json:"runId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.PendingStreamText(params.AgentID, params.RunID)

	case "calls.list":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListToolCalls(params.AgentID, params.SessionID)
	case "calls.approve":
		var params struct {
			AgentID string `json:"agentId"`
			CallID  string `json:"callId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ApproveToolCall(params.AgentID, params.CallID)
	case "calls.deny":
		var params struct {
			AgentID string `json:"agentId"`
			CallID  string `json:"callId"`
			Note    string `json:"note"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.DenyToolCall(params.AgentID, params.CallID, params.Note)
	case "approvals.pending":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.PendingApprovals(params.AgentID)
	case "calls.resolve":
		var params struct {
			AgentID string `json:"agentId"`
			CallID  string `json:"callId"`
			State   string `json:"state"`
			Result  string `json:"result"`
			Note    string `json:"note"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ResolveToolCall(params.AgentID, params.CallID, params.State, params.Result, params.Note)

	case "exfil.list":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListExfilGrants(params.AgentID)
	case "exfil.grant":
		var params struct {
			AgentID     string   `json:"agentId"`
			Destination string   `json:"destination"`
			Scopes      []string `json:"scopes"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.GrantExfil(params.AgentID, params.Destination, params.Scopes)
	case "exfil.revoke":
		var params struct {
			AgentID     string `json:"agentId"`
			Destination string `json:"destination"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return nil, s.svc.RevokeExfil(params.AgentID, params.Destination)

	case "credentials.status":
		return s.svc.ListCredentialStatus()

	case "memory.list":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ListMemories(params.AgentID)
	case "memory.add":
		var params struct {
			AgentID string `json:"agentId"`
			Content string `json:"content"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.AddMemory(params.AgentID, params.Content)
	case "memory.update":
		var params struct {
			AgentID string `json:"agentId"`
			ID      string `json:"id"`
			Content string `json:"content"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.UpdateMemory(params.AgentID, params.ID, params.Content)
	case "memory.delete":
		var params struct {
			AgentID string `json:"agentId"`
			ID      string `json:"id"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return nil, s.svc.DeleteMemory(params.AgentID, params.ID)
	case "memory.export":
		var params struct {
			AgentID string `json:"agentId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.ExportMemories(params.AgentID)

	case "summaries.get":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.GetSessionSummary(params.AgentID, params.SessionID)

	case "plans.get":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return s.svc.GetSessionPlan(params.AgentID, params.SessionID)
	case "plans.clear":
		var params struct {
			AgentID   string `json:"agentId"`
			SessionID string `json:"sessionId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		return nil, s.svc.ClearSessionPlan(params.AgentID, params.SessionID)

	default:
		return nil, fmt.Errorf("未知 IPC 方法 %q", request.Method)
	}
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}

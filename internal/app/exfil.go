package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

// guardedChat wraps the model client so every request — every turn of the
// loop, not just the first — re-checks the session's data exfiltration
// authorization against the actual endpoint. Tool results and policy changes
// since the previous turn are accounted for automatically. The check runs
// before the inner call on both the plain and the streaming path, so a
// denied send never reaches an adapter.
type guardedChat struct {
	inner agent.ChatModel
	check func(context.Context) error
}

var (
	_ agent.ChatModel          = guardedChat{}
	_ agent.StreamingChatModel = guardedChat{}
)

func (g guardedChat) Chat(ctx context.Context, source config.Model, messages []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	if err := g.check(ctx); err != nil {
		return model.ChatResponse{}, err
	}
	return g.inner.Chat(ctx, source, messages, tools)
}

func (g guardedChat) ChatStream(ctx context.Context, source config.Model, messages []model.Message, tools []model.Tool, onDelta func(string) error) (model.ChatResponse, error) {
	if err := g.check(ctx); err != nil {
		return model.ChatResponse{}, err
	}
	if streamer, ok := g.inner.(agent.StreamingChatModel); ok {
		return streamer.ChatStream(ctx, source, messages, tools, onDelta)
	}
	return g.inner.Chat(ctx, source, messages, tools)
}

// endpointOrigin normalizes a model base URL to scheme://host:port and
// reports whether the endpoint is on this machine (loopback), which is not
// an exfiltration at all.
func endpointOrigin(baseURL string) (string, bool, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false, fmt.Errorf("无法解析模型地址 %q", baseURL)
	}
	host := parsed.Hostname()
	loopback := host == "localhost" || host == "::1" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	return parsed.Scheme + "://" + parsed.Host, loopback, nil
}

// exfilCheck decides whether this session's accumulated context may be sent
// to the model endpoint. Local loopback models are exempt: the data never
// leaves the machine. Remote endpoints need a grant whose scopes cover every
// scope accumulated in the session; an unknown scope can only be covered by
// a grant that explicitly includes it.
func (s *Service) exfilCheck(ctx context.Context, sessionID string, source config.Model, profile storage.AgentProfile) error {
	// Remote providers may leave BaseURL empty to mean the official
	// endpoint; the check compares against the effective origin, never the
	// provider name.
	origin, loopback, err := endpointOrigin(source.EffectiveBaseURL())
	if err != nil {
		return err
	}
	if loopback {
		return nil
	}
	scopes, err := s.sessionScopes(ctx, sessionID, profile)
	if err != nil {
		return err
	}
	if len(scopes) == 0 {
		return nil
	}
	grant, found, err := s.store.GetExfilGrant(ctx, origin)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("数据外发被拒绝：未授权向 %s 发送会话数据（累计范围：%s）。请先授权该目的地，或改用本机模型", origin, strings.Join(scopes, "、"))
	}
	allowed := map[string]bool{}
	for _, scope := range grant.Scopes {
		allowed[scope] = true
	}
	missing := []string{}
	for _, scope := range scopes {
		if !allowed[scope] {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("数据外发被拒绝：%s 未包含在向 %s 的授权范围（%s）内。请调整授权或改用本机模型", strings.Join(missing, "、"), origin, strings.Join(grant.Scopes, "、"))
	}
	return nil
}

// sessionScopes accumulates the data scopes present in a session's context:
// user input is personal, successful tool results carry the sensitivity
// their tool access touched (tagged at persistence time), and the system
// prompt's skills summary is workspace-scoped.
func (s *Service) sessionScopes(ctx context.Context, sessionID string, profile storage.AgentProfile) ([]string, error) {
	hasUser, toolScopes, err := s.store.SessionDataScopes(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	if hasUser {
		set[storage.ScopePersonal] = true
	}
	for _, scope := range toolScopes {
		set[scope] = true
	}
	if profile.AllowsTool("local__load_skill") {
		summary := s.skills.SummaryFor(profile.EnabledSkills)
		if profile.AllSkills {
			summary = s.skills.Summary()
		}
		if summary != "" {
			set[storage.ScopeWorkspace] = true
		}
	}
	scopes := make([]string, 0, len(set))
	for scope := range set {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes, nil
}

// GrantExfil records (or tightens/extends) the persistent authorization for
// one destination. The destination is normalized to its endpoint origin; a
// provider name is never sufficient — the actual endpoint is what the send
// check compares against.
func (s *Service) GrantExfil(destination string, scopes []string) (storage.ExfilGrant, error) {
	if err := s.checkReady(); err != nil {
		return storage.ExfilGrant{}, err
	}
	origin, _, err := endpointOrigin(strings.TrimSpace(destination))
	if err != nil {
		return storage.ExfilGrant{}, fmt.Errorf("目的地必须是 http(s) 端点地址: %w", err)
	}
	cleaned := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if !storage.ValidScope(scope) {
			return storage.ExfilGrant{}, fmt.Errorf("未知的数据范围 %q", scope)
		}
		cleaned = append(cleaned, scope)
	}
	grant := storage.ExfilGrant{Destination: origin, Scopes: cleaned, CreatedAt: ""}
	if err := s.store.SetExfilGrant(context.Background(), grant); err != nil {
		return storage.ExfilGrant{}, err
	}
	saved, _, err := s.store.GetExfilGrant(context.Background(), origin)
	return saved, err
}

func (s *Service) ListExfilGrants() ([]storage.ExfilGrant, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListExfilGrants(context.Background())
}

func (s *Service) RevokeExfil(destination string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	origin, _, err := endpointOrigin(strings.TrimSpace(destination))
	if err != nil {
		return fmt.Errorf("目的地必须是 http(s) 端点地址: %w", err)
	}
	return s.store.DeleteExfilGrant(context.Background(), origin)
}

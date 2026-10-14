package app

import (
	"context"
	"errors"
	"fmt"

	"icepick/internal/config"
	"icepick/internal/storage"
)

func restrictPolicy(a, b storage.AgentPolicy) storage.AgentPolicy {
	a.ApprovalMode = stricterMode(a.ApprovalMode, b.ApprovalMode)
	a.Permissions = intersectPermissions(a.Permissions, b.Permissions)
	a.Memory = a.Memory && b.Memory
	return a
}

func (s *Service) runActors(ctx context.Context, runID string) ([]storage.Run, error) {
	ids, err := s.store.AncestorRunIDs(ctx, runID)
	if err != nil {
		return nil, err
	}
	runs := []storage.Run{}
	for _, id := range ids {
		run, err := s.store.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// Frozen permission ceilings and every live ancestor restriction apply to
// descendants.
func (s *Service) restrictRunPolicy(ctx context.Context, runID string, p storage.AgentProfile) (storage.AgentPolicy, error) {
	policy := p.Policy
	runs, err := s.runActors(ctx, runID)
	if err != nil {
		return policy, err
	}
	resources, err := s.resourcesFor(ctx, p, runID)
	if err != nil {
		return policy, err
	}
	for _, run := range runs {
		actor, err := s.store.GetAgent(ctx, run.AgentID)
		if err != nil {
			return policy, err
		}
		policy = restrictPolicy(policy, actor.Policy)
		{
			if run.AgentID == resources.RootContextID {
				policy = restrictPolicy(policy, resources.RootPolicy)
			} else {
				return policy, errors.New("运行身份不在起始依赖中")
			}
		}

	}
	return policy, nil
}

func (s *Service) exfilCheckRun(ctx context.Context, runID, sessionID string, source config.Model, p storage.AgentProfile) error {
	if _, err := s.restrictRunPolicy(ctx, runID, p); err != nil {
		return err
	}
	runs, err := s.runActors(ctx, runID)
	if err != nil {
		return err
	}
	origin, local, err := endpointOrigin(source.EffectiveBaseURL())
	if err != nil {
		return err
	}
	if local {
		return nil
	}
	scopes, err := s.sessionScopes(ctx, sessionID, p)
	if err != nil {
		return err
	}
	resources, err := s.resourcesFor(ctx, p, runID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, run := range runs {
		if !seen[run.AgentID] {
			seen[run.AgentID] = true
			{
				allowed := map[string]bool{}
				for _, scope := range resources.Grants[run.AgentID][origin].Scopes {
					allowed[scope] = true
				}
				for _, scope := range scopes {
					if !allowed[scope] {
						return fmt.Errorf("数据外发超出根任务开始时的授权上限：%s / %s；新增授权用于新任务", origin, scope)
					}
				}
			}
			if err := requireGrantCoverage(ctx, s.store, run.AgentID, origin, scopes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) runActorIDs(ctx context.Context, runID string) ([]string, error) {
	runs, err := s.runActors(ctx, runID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, run := range runs {
		if !seen[run.AgentID] {
			seen[run.AgentID] = true
			ids = append(ids, run.AgentID)
		}
	}
	return ids, nil
}

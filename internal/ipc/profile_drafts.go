package ipc

import "icepick/internal/storage"

func (s *Server) dispatchProfileDraft(request Request) (any, error) {
	var p struct {
		EvidenceIDs []string                `json:"evidenceIds"`
		ID          string                  `json:"id"`
		SessionID   string                  `json:"sessionId"`
		Target      string                  `json:"target"`
		Request     string                  `json:"request"`
		Revision    int                     `json:"revision"`
		Proposal    storage.ProfileProposal `json:"proposal"`
	}
	if err := decodeParams(request.Params, &p); err != nil {
		return nil, err
	}
	switch request.Method {
	case "drafts.start":
		return s.svc.StartProfileDraft(p.SessionID, p.Target, p.Request, p.EvidenceIDs)
	case "drafts.list":
		return s.svc.ListProfileDrafts()
	case "drafts.edit":
		return s.svc.EditProfileDraft(p.ID, p.Revision, p.Proposal)
	case "drafts.apply":
		return s.svc.ApplyProfileDraft(p.ID, p.Revision)
	case "drafts.discard":
		return nil, s.svc.DiscardProfileDraft(p.ID, p.Revision)
	case "drafts.revert":
		return s.svc.RevertProfileDraft(p.ID, p.Revision)
	}
	return nil, nil
}

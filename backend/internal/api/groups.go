package api

import (
	"net/http"
	"strconv"
	"strings"

	"gerrit-go/internal/store"
)

// canEditAccess reports whether the caller may modify a project's access rules:
// administrators, or any group holding the editAccess permission on refs/*.
func (s *Server) canEditAccess(acct *store.Account, project string) bool {
	if acct != nil && acct.Admin {
		return true
	}
	return s.can(acct, project, "refs/*", PermEditAccess)
}

func groupInfo(g *store.Group) map[string]any {
	return map[string]any{
		"id":          strconv.FormatInt(g.ID, 10),
		"name":        g.Name,
		"description": g.Description,
		"system":      g.System,
	}
}

// ---------- groups ----------

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if s.optionalAccount(r) == nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	groups, err := s.db.ListGroups()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make(map[string]any, len(groups))
	for _, g := range groups {
		out[g.Name] = groupInfo(g)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		forbid(w, "createGroup")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, err := s.db.GetGroupByName(strings.TrimSpace(req.Name)); err == nil {
		writeErr(w, http.StatusConflict, "group already exists")
		return
	}
	g := &store.Group{Name: strings.TrimSpace(req.Name), Description: req.Description}
	if err := s.db.CreateGroup(g); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, groupInfo(g))
}

func (s *Server) resolveGroup(idStr string) (*store.Group, error) {
	idStr = strings.TrimSpace(idStr)
	if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
		return s.db.GetGroup(id)
	}
	return s.db.GetGroupByName(idStr)
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	if s.optionalAccount(r) == nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	g, err := s.resolveGroup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	writeJSON(w, http.StatusOK, groupInfo(g))
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		forbid(w, "deleteGroup")
		return
	}
	g, err := s.resolveGroup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	if g.System {
		writeErr(w, http.StatusBadRequest, "cannot delete a system group")
		return
	}
	if err := s.db.DeleteGroup(g.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	if s.optionalAccount(r) == nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	g, err := s.resolveGroup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	members, err := s.db.ListGroupMembers(g.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make(map[string]any, len(members))
	for _, m := range members {
		out[m.Username] = map[string]any{
			"_account_id": m.ID,
			"username":    m.Username,
			"name":        m.FullName,
			"email":       m.Email,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		forbid(w, "editGroupMembers")
		return
	}
	g, err := s.resolveGroup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	target, err := s.resolveAccount(r.PathValue("account"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.db.AddGroupMember(g.ID, target.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"_account_id": target.ID,
		"username":    target.Username,
		"name":        target.FullName,
		"email":       target.Email,
	})
}

func (s *Server) handleDeleteGroupMember(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		forbid(w, "editGroupMembers")
		return
	}
	g, err := s.resolveGroup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	target, err := s.resolveAccount(r.PathValue("account"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.db.RemoveGroupMember(g.ID, target.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- project access ----------

func (s *Server) handleGetAccess(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.ensureProjectRead(w, r, name) {
		return
	}
	rules, err := s.db.ListAccessRules(name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"local":    rules,
		"can_edit": s.canEditAccess(s.optionalAccount(r), name),
	})
}

func (s *Server) handleSetAccess(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	name := r.PathValue("name")
	if _, err := s.db.GetProject(name); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.canEditAccess(acct, name) {
		forbid(w, PermEditAccess)
		return
	}
	var req struct {
		Rules []*store.AccessRule `json:"rules"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for _, rule := range req.Rules {
		rule.Project = name
		if rule.Action == "" {
			rule.Action = "ALLOW"
		}
	}
	if err := s.db.SetAccessRules(name, req.Rules); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rules, _ := s.db.ListAccessRules(name)
	writeJSON(w, http.StatusOK, map[string]any{
		"local":    rules,
		"can_edit": true,
	})
}

var validSubmitTypes = map[string]bool{
	"FAST_FORWARD_ONLY":   true,
	"REBASE_IF_NECESSARY": true,
	"REBASE_ALWAYS":       true,
	"MERGE_IF_NECESSARY":  true,
	"MERGE_ALWAYS":        true,
	"CHERRY_PICK":         true,
}

// handleSetProjectConfig updates a project's submit strategy, whole-topic flag
// and submit requirements. Gated by the editAccess permission.
func (s *Server) handleSetProjectConfig(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	name := r.PathValue("name")
	p, err := s.db.GetProject(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.canEditAccess(acct, name) {
		forbid(w, PermEditAccess)
		return
	}
	var req struct {
		SubmitType         *string                     `json:"submit_type"`
		SubmitWholeTopic   *bool                       `json:"submit_whole_topic"`
		SubmitRequirements *[]*store.SubmitRequirement `json:"submit_requirements"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	submitType := p.SubmitType
	if req.SubmitType != nil {
		if !validSubmitTypes[*req.SubmitType] {
			writeErr(w, http.StatusBadRequest, "invalid submit_type")
			return
		}
		submitType = *req.SubmitType
	}
	wholeTopic := p.SubmitWholeTopic
	if req.SubmitWholeTopic != nil {
		wholeTopic = *req.SubmitWholeTopic
	}
	if err := s.db.SetProjectSubmitType(name, submitType, wholeTopic); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.SubmitRequirements != nil {
		reqs := *req.SubmitRequirements
		for _, sr := range reqs {
			sr.Project = name
		}
		if err := s.db.SetSubmitRequirements(name, reqs); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	updated, _ := s.db.ListSubmitRequirements(name)
	writeJSON(w, http.StatusOK, map[string]any{
		"submit_type":         submitType,
		"submit_whole_topic":  wholeTopic,
		"submit_requirements": updated,
	})
}

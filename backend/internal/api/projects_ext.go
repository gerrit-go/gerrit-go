package api

import (
	"errors"
	"net/http"
	"strings"

	"gerrit-go/internal/gitsvc"
)

// mapGitErr translates a gitsvc sentinel error into an HTTP status code.
func mapGitErr(err error) int {
	switch {
	case errors.Is(err, gitsvc.ErrProjectMissing):
		return http.StatusNotFound
	case errors.Is(err, gitsvc.ErrRefMissing):
		return http.StatusNotFound
	case errors.Is(err, gitsvc.ErrRefExists):
		return http.StatusConflict
	case errors.Is(err, gitsvc.ErrInvalidRef):
		return http.StatusBadRequest
	case errors.Is(err, gitsvc.ErrNoChanges):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// ---------- branches ----------

func (s *Server) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	var req struct {
		Ref      string `json:"ref"`
		Branch   string `json:"branch"`
		Revision string `json:"revision"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	branch := firstNonEmpty(req.Branch, req.Ref)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if branch == "" {
		writeErr(w, http.StatusBadRequest, "branch is required")
		return
	}
	if !s.can(acct, project, "refs/heads/"+branch, PermPush) {
		forbid(w, PermPush)
		return
	}
	sha, err := s.git.CreateBranch(project, branch, req.Revision)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ref": "refs/heads/" + branch, "revision": sha})
}

func (s *Server) handleDeleteBranch(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	branch := strings.TrimPrefix(r.PathValue("branch"), "refs/heads/")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.can(acct, project, "refs/heads/"+branch, PermPush) {
		forbid(w, PermPush)
		return
	}
	if err := s.git.DeleteBranch(project, branch); err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- tags ----------

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("name")
	if !s.ensureProjectRead(w, r, project) {
		return
	}
	tags, err := s.git.ListTags(project)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	var req struct {
		Ref      string `json:"ref"`
		Tag      string `json:"tag"`
		Revision string `json:"revision"`
		Message  string `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tag := firstNonEmpty(req.Tag, req.Ref)
	tag = strings.TrimPrefix(tag, "refs/tags/")
	if tag == "" {
		writeErr(w, http.StatusBadRequest, "tag is required")
		return
	}
	if !s.can(acct, project, "refs/tags/"+tag, PermPush) {
		forbid(w, PermPush)
		return
	}
	sha, err := s.git.CreateTag(project, tag, req.Revision, req.Message)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ref": "refs/tags/" + tag, "revision": sha, "message": req.Message})
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	tag := strings.TrimPrefix(r.PathValue("tag"), "refs/tags/")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.can(acct, project, "refs/tags/"+tag, PermPush) {
		forbid(w, PermPush)
		return
	}
	if err := s.git.DeleteTag(project, tag); err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- web file edit ----------

// handleEditFile commits a single-file edit directly to a branch, bypassing
// review (the caller must hold push permission on that branch).
func (s *Server) handleEditFile(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	var req struct {
		Branch  string `json:"branch"`
		Path    string `json:"path"`
		File    string `json:"file"`
		Content string `json:"content"`
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	branch := strings.TrimPrefix(req.Branch, "refs/heads/")
	filePath := firstNonEmpty(req.Path, req.File)
	if branch == "" || filePath == "" {
		writeErr(w, http.StatusBadRequest, "branch and path are required")
		return
	}
	if !s.can(acct, project, "refs/heads/"+branch, PermPush) {
		forbid(w, PermPush)
		return
	}
	authorName := orDefault(acct.FullName, acct.Username)
	authorEmail := acct.Email
	if authorEmail == "" {
		authorEmail = acct.Username + "@localhost"
	}
	sha, err := s.git.CommitFileEdit(project, branch, filePath, []byte(req.Content), req.Message, authorName, authorEmail)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commit": sha, "branch": branch, "path": filePath})
}

// ---------- project state / delete ----------

// handleSetProjectState archives (READ_ONLY/HIDDEN) or restores (ACTIVE) a
// project. Only access editors may change the state.
func (s *Server) handleSetProjectState(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.canEditAccess(acct, project) {
		forbid(w, PermEditAccess)
		return
	}
	var req struct {
		State string `json:"state"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	state := strings.ToUpper(strings.TrimSpace(req.State))
	switch state {
	case "ACTIVE", "READ_ONLY", "HIDDEN":
	default:
		writeErr(w, http.StatusBadRequest, "state must be ACTIVE, READ_ONLY or HIDDEN")
		return
	}
	if err := s.db.SetProjectState(project, state); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": project, "state": state})
}

// handleDeleteProject permanently removes a project, its repository and all of
// its changes. This is destructive and restricted to administrators.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if acct == nil || !acct.Admin {
		forbid(w, "deleteProject")
		return
	}
	if _, err := s.db.GetProject(project); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if err := s.git.DeleteProject(project); err != nil && !errors.Is(err, gitsvc.ErrProjectMissing) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.DeleteProject(project); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

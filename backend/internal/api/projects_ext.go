package api

import (
	"errors"
	"net/http"
	"strconv"
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

// handleBlame returns per-line blame annotations for a file at a revision.
func (s *Server) handleBlame(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("name")
	if !s.ensureProjectRead(w, r, project) {
		return
	}
	lines, err := s.git.Blame(project, r.URL.Query().Get("revision"), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

// handleFileLog returns the commit history of a single file at a revision.
func (s *Server) handleFileLog(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("name")
	if !s.ensureProjectRead(w, r, project) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("n"))
	entries, err := s.git.FileLog(project, r.URL.Query().Get("revision"), r.URL.Query().Get("path"), limit)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleGC runs git gc on a project repository (admin only).
func (s *Server) handleGC(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	out, err := s.git.GC(r.PathValue("name"))
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": out})
}

// handleFsck runs git fsck on a project repository and returns the report
// lines (admin only).
func (s *Server) handleFsck(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	lines, err := s.git.Fsck(r.PathValue("name"))
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": lines, "healthy": len(lines) == 0})
}

// handleCommitDiff returns the file diffs introduced by a single commit.
func (s *Server) handleCommitDiff(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("name")
	if !s.ensureProjectRead(w, r, project) {
		return
	}
	sha := r.URL.Query().Get("sha")
	if sha == "" {
		writeErr(w, http.StatusBadRequest, "sha is required")
		return
	}
	diffs, err := s.git.CommitDiff(project, sha)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, diffs)
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
		s.forbid(w, r, PermPush)
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
		s.forbid(w, r, PermPush)
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
		s.forbid(w, r, PermPush)
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
		s.forbid(w, r, PermPush)
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
		s.forbid(w, r, PermPush)
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
		s.forbid(w, r, PermEditAccess)
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
	s.audit(acct, "project-state", "project", project, state)
	writeJSON(w, http.StatusOK, map[string]any{"name": project, "state": state})
}

// handleDeleteProject permanently removes a project, its repository and all of
// its changes. This is destructive and restricted to administrators.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	project := r.PathValue("name")
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "deleteProject")
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
	s.audit(acct, "project-delete", "project", project, "")
	w.WriteHeader(http.StatusNoContent)
}

// validProjectName rejects traversal-prone and malformed project names,
// mirroring the gitsvc.CreateProject guards.
func validProjectName(name string) bool {
	return name != "" && !strings.Contains(name, "..") && !strings.HasPrefix(name, "/")
}

// handleRenameProject moves a project to a new name (typically into another
// namespace). It updates every database reference and moves the repository
// directory; the old name stops resolving immediately (no redirect shim).
// Admin only.
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	oldName := r.PathValue("name")
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newName := strings.TrimSuffix(strings.TrimSpace(req.Name), ".git")
	if !validProjectName(newName) {
		writeErr(w, http.StatusBadRequest, "invalid project name")
		return
	}
	if newName == oldName {
		writeErr(w, http.StatusBadRequest, "new name must differ from the current name")
		return
	}
	if _, err := s.db.GetProject(oldName); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if _, err := s.db.GetProject(newName); err == nil {
		writeErr(w, http.StatusConflict, "target project already exists")
		return
	}
	if err := s.git.MoveRepo(oldName, newName); err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	if err := s.db.RenameProject(oldName, newName); err != nil {
		if rbErr := s.git.MoveRepo(newName, oldName); rbErr != nil {
			writeErr(w, http.StatusInternalServerError,
				"rename failed and repository rollback failed: "+err.Error()+" / "+rbErr.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(acct, "project-rename", "project", newName, oldName+" -> "+newName)
	writeJSON(w, http.StatusOK, map[string]any{"name": newName, "from": oldName})
}

// handleBulkLabels applies (or deletes, for null values) a set of labels on
// many projects at once. Admin only.
func (s *Server) handleBulkLabels(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if acct == nil || !acct.Admin {
		s.forbid(w, r, "admin")
		return
	}
	var req struct {
		Projects []string          `json:"projects"`
		Labels   map[string]*string `json:"labels"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Projects) == 0 || len(req.Labels) == 0 {
		writeErr(w, http.StatusBadRequest, "projects and labels are required")
		return
	}
	for _, name := range req.Projects {
		if _, err := s.db.GetProject(name); err != nil {
			writeErr(w, http.StatusNotFound, "project not found: "+name)
			return
		}
	}
	for _, name := range req.Projects {
		for label, value := range req.Labels {
			var err error
			if value == nil {
				err = s.db.DeleteProjectLabel(name, label)
			} else {
				err = s.db.SetProjectLabel(name, label, *value)
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, name+": "+err.Error())
				return
			}
		}
	}
	s.audit(acct, "labels-bulk", "project", strings.Join(req.Projects, ","), formatLabelOps(req.Labels))
	writeJSON(w, http.StatusOK, map[string]any{"projects": len(req.Projects), "labels": req.Labels})
}

// formatLabelOps renders a label mutation map as "k=v" pairs ("k-" when the
// label is being deleted) so audit details stay human-readable.
func formatLabelOps(labels map[string]*string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		if v == nil {
			parts = append(parts, k+"-")
		} else {
			parts = append(parts, k+"="+*v)
		}
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

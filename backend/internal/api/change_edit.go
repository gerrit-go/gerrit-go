package api

import (
	"errors"
	"net/http"

	"gerrit-go/internal/gitsvc"
	"gerrit-go/internal/i18n"
	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

// editContext resolves the change and caller for an edit endpoint and enforces
// the shared preconditions: the change exists, is readable, is still open, and
// the caller may upload a patch set to it (owner or push on refs/for/<branch>).
// It writes the error response and returns ok=false on any failure.
func (s *Server) editContext(w http.ResponseWriter, r *http.Request) (*store.Change, *store.Account, bool) {
	acct := s.account(r)
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return nil, nil, false
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return nil, nil, false
	}
	if !s.ensureChangeRead(w, r, c) {
		return nil, nil, false
	}
	if c.Status != "NEW" {
		writeErr(w, http.StatusConflict, "change is not open")
		return nil, nil, false
	}
	if !s.canUploadPatchSet(acct, c) {
		s.forbid(w, r, PermPush)
		return nil, nil, false
	}
	return c, acct, true
}

// editInfoJSON renders an EditInfo, adding a stale flag when the edit's base
// patch set is no longer the change's current one.
func editInfoJSON(info *gitsvc.EditInfo, c *store.Change, currentSHA string) map[string]any {
	return map[string]any{
		"commit":      info.SHA,
		"base_commit": info.BaseSHA,
		"base_ps":     info.BasePS,
		"stale":       info.BaseSHA != "" && info.BaseSHA != currentSHA,
	}
}

func (s *Server) currentPSCommit(c *store.Change) string {
	if ps, err := s.db.GetPatchSet(c.Number, c.CurrentPS); err == nil {
		return ps.CommitSHA
	}
	return ""
}

func (s *Server) handleGetEdit(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.editContext(w, r)
	if !ok {
		return
	}
	info, err := s.git.GetEdit(c.Project, c.Number)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no open edit")
		return
	}
	writeJSON(w, http.StatusOK, editInfoJSON(info, c, s.currentPSCommit(c)))
}

func (s *Server) handleCreateEdit(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.editContext(w, r)
	if !ok {
		return
	}
	info, err := s.git.CreateEdit(c.Project, c.Number)
	if err != nil {
		if errors.Is(err, gitsvc.ErrRefExists) {
			writeErr(w, http.StatusConflict, "an edit is already open on this change")
			return
		}
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, editInfoJSON(info, c, s.currentPSCommit(c)))
}

func (s *Server) handleDeleteEdit(w http.ResponseWriter, r *http.Request) {
	c, _, ok := s.editContext(w, r)
	if !ok {
		return
	}
	if err := s.git.DeleteEdit(c.Project, c.Number); err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) handlePutEditFile(w http.ResponseWriter, r *http.Request) {
	c, acct, ok := s.editContext(w, r)
	if !ok {
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	// Convenience: auto-create the edit when none is open so the UI has a
	// single save action.
	if !s.git.EditExists(c.Project, c.Number) {
		if _, err := s.git.CreateEdit(c.Project, c.Number); err != nil && !errors.Is(err, gitsvc.ErrRefExists) {
			writeErr(w, mapGitErr(err), err.Error())
			return
		}
	}
	info, err := s.git.PutEditFile(c.Project, c.Number, req.Path, []byte(req.Content), acct.FullName, acct.Email)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, editInfoJSON(info, c, s.currentPSCommit(c)))
}

func (s *Server) handleDeleteEditFile(w http.ResponseWriter, r *http.Request) {
	c, acct, ok := s.editContext(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	info, err := s.git.DeleteEditFile(c.Project, c.Number, path, acct.FullName, acct.Email)
	if err != nil {
		writeErr(w, mapGitErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, editInfoJSON(info, c, s.currentPSCommit(c)))
}

func (s *Server) handlePublishEdit(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFrom(r.Context())
	c, acct, ok := s.editContext(w, r)
	if !ok {
		return
	}
	nc, err := s.git.PublishEdit(c.Project, c.Number)
	if err != nil {
		switch {
		case errors.Is(err, gitsvc.ErrRefMissing):
			writeErr(w, http.StatusConflict, "no open edit")
		case errors.Is(err, gitsvc.ErrNoChanges):
			writeErr(w, http.StatusBadRequest, "edit has no changes")
		case errors.Is(err, gitsvc.ErrEditStale):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "stale": true})
		default:
			writeErr(w, mapGitErr(err), err.Error())
		}
		return
	}
	if err := s.recordPatchSet(c.Number, acct, nc, i18n.T(lang, "msg.psEditPublished", nc.NewPatchSet)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.notifyChange(c, acct.ID, notify.Event{
		Type:             "patchset-uploaded",
		Lang:             lang,
		Message:          i18n.T(lang, "msg.psEditPublishedNotify", acct.FullName, nc.NewPatchSet),
		NotifyOwner:      true,
		IncludeReviewers: true,
	})
	updated, err := s.db.GetChange(c.Number)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, changeInfo(updated))
}

package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/store"
)

func draftJSON(d *store.CommentDraft) map[string]any {
	return map[string]any{
		"id":          d.ID,
		"patch_set":   d.PatchSet,
		"path":        d.File,
		"line":        d.Line,
		"message":     d.Message,
		"in_reply_to": d.InReplyTo,
		"updated":     d.Created.Format(time.RFC3339),
	}
}

func (s *Server) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if !s.ensureChangeRead(w, r, c) {
		return
	}
	drafts, err := s.db.ListDrafts(c.Number, acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(drafts))
	for _, d := range drafts {
		out = append(out, draftJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutDraft(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if !s.ensureChangeRead(w, r, c) {
		return
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermComment) {
		s.forbid(w, r, PermComment)
		return
	}
	var req struct {
		ID        int64  `json:"id"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Message   string `json:"message"`
		InReplyTo int64  `json:"in_reply_to"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeErr(w, http.StatusBadRequest, "draft message is empty")
		return
	}
	if req.ID > 0 {
		if err := s.db.UpdateDraft(acct.ID, req.ID, req.Message, req.InReplyTo); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		drafts, _ := s.db.ListDrafts(c.Number, acct.ID)
		for _, d := range drafts {
			if d.ID == req.ID {
				writeJSON(w, http.StatusOK, draftJSON(d))
				return
			}
		}
		writeErr(w, http.StatusNotFound, "draft not found")
		return
	}
	d := &store.CommentDraft{
		ChangeNum: c.Number, PatchSet: c.CurrentPS, AccountID: acct.ID,
		File: req.Path, Line: req.Line, Message: req.Message, InReplyTo: req.InReplyTo,
	}
	if err := s.db.CreateDraft(d); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, draftJSON(d))
}

func (s *Server) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid draft id")
		return
	}
	if err := s.db.DeleteDraft(acct.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if !s.ensureChangeRead(w, r, c) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	cm, err := s.db.GetComment(id)
	if err != nil || cm.ChangeNum != c.Number {
		writeErr(w, http.StatusNotFound, "comment not found")
		return
	}
	var req struct {
		Resolved bool `json:"resolved"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.db.SetCommentResolved(id, req.Resolved); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	cm.Resolved = req.Resolved
	writeJSON(w, http.StatusOK, map[string]any{"id": cm.ID, "resolved": cm.Resolved})
}

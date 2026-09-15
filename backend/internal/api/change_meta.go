package api

import (
	"fmt"
	"net/http"
	"strconv"

	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

// accountBrief renders the minimal account payload Gerrit clients expect.
func accountBrief(a *store.Account) map[string]any {
	if a == nil {
		return nil
	}
	return map[string]any{
		"_account_id": a.ID,
		"name":        orDefault(a.FullName, a.Username),
		"username":    a.Username,
		"email":       a.Email,
	}
}

// loadChange resolves the change number from the path and enforces read access.
func (s *Server) loadChange(w http.ResponseWriter, r *http.Request) (*store.Change, bool) {
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return nil, false
	}
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return nil, false
	}
	if !s.ensureChangeRead(w, r, c) {
		return nil, false
	}
	return c, true
}

// ---------- hashtags ----------

func (s *Server) handleListHashtags(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	tags, err := s.db.ListHashtags(c.Number)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// handleSetHashtags applies an {add:[], remove:[]} edit to a change's hashtags
// and returns the resulting full set.
func (s *Server) handleSetHashtags(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermEditTopic) {
		forbid(w, PermEditTopic)
		return
	}
	var req struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for _, t := range req.Remove {
		if err := s.db.DeleteHashtag(c.Number, t); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	for _, t := range req.Add {
		if err := s.db.AddHashtag(c.Number, t); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	tags, err := s.db.ListHashtags(c.Number)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tags == nil {
		tags = []string{}
	}
	s.db.TouchChange(c.Number)
	writeJSON(w, http.StatusOK, tags)
}

// ---------- assignee ----------

func (s *Server) handleGetAssignee(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	a, err := s.db.GetAssignee(c.Number)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, accountBrief(a))
}

func (s *Server) handleSetAssignee(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermEditTopic) {
		forbid(w, PermEditTopic)
		return
	}
	var req struct {
		Assignee string `json:"assignee"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target, err := s.resolveAccount(req.Assignee)
	if err != nil {
		writeErr(w, http.StatusNotFound, "assignee not found")
		return
	}
	if err := s.db.SetAssignee(c.Number, target.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := orDefault(target.FullName, target.Username)
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "assignee", AuthorID: acct.ID,
		Message: fmt.Sprintf("Assignee set to %s.", name),
	})
	s.db.AddAttention(c.Number, target.ID, "Assigned to you")
	s.notifyChange(c, acct.ID, notify.Event{
		Type:            "assignee",
		Message:         acct.FullName + " assigned this change to you.",
		ExtraRecipients: []int64{target.ID},
	})
	writeJSON(w, http.StatusOK, accountBrief(target))
}

func (s *Server) handleDeleteAssignee(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermEditTopic) {
		forbid(w, PermEditTopic)
		return
	}
	if err := s.db.SetAssignee(c.Number, 0); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "assignee", AuthorID: acct.ID, Message: "Assignee removed.",
	})
	s.db.TouchChange(c.Number)
	writeJSON(w, http.StatusOK, nil)
}

// ---------- attention set ----------

func (s *Server) handleListAttention(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	list, err := s.db.ListAttention(c.Number)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, attentionInfo(list))
}

// handleAddAttention adds an account to the change's attention set.
func (s *Server) handleAddAttention(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	if c.OwnerID != acct.ID && !s.can(acct, c.Project, branchRef(c.Branch), PermComment) {
		forbid(w, PermComment)
		return
	}
	var req struct {
		User   string `json:"user"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target, err := s.resolveAccount(req.User)
	if err != nil {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.db.AddAttention(c.Number, target.ID, req.Reason); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := orDefault(target.FullName, target.Username)
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "attention", AuthorID: acct.ID,
		Message: fmt.Sprintf("Added %s to the attention set.", name),
	})
	s.notifyChange(c, acct.ID, notify.Event{
		Type:            "attention",
		Message:         acct.FullName + " added you to the attention set.",
		ExtraRecipients: []int64{target.ID},
	})
	list, _ := s.db.ListAttention(c.Number)
	writeJSON(w, http.StatusOK, attentionInfo(list))
}

// handleRemoveAttention drops an account from the change's attention set.
func (s *Server) handleRemoveAttention(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	c, ok := s.loadChange(w, r)
	if !ok {
		return
	}
	if c.OwnerID != acct.ID && !s.can(acct, c.Project, branchRef(c.Branch), PermComment) {
		forbid(w, PermComment)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid account id")
		return
	}
	if err := s.db.RemoveAttention(c.Number, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := strconv.FormatInt(id, 10)
	if a, err := s.db.GetAccount(id); err == nil {
		name = orDefault(a.FullName, a.Username)
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "attention", AuthorID: acct.ID,
		Message: fmt.Sprintf("Removed %s from the attention set.", name),
	})
	list, _ := s.db.ListAttention(c.Number)
	writeJSON(w, http.StatusOK, attentionInfo(list))
}

func attentionInfo(list []*store.AttentionEntry) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, map[string]any{
			"account": map[string]any{
				"_account_id": e.AccountID,
				"name":        orDefault(e.Name, e.Username),
				"username":    e.Username,
				"email":       e.Email,
			},
			"reason": e.Reason,
		})
	}
	return out
}

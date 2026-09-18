package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/store"
)

// Gerrit REST API compatibility shim.
//
// Implements the subset of the real Gerrit REST API that git-review,
// repo upload, and Jenkins Gerrit Trigger actually call. Responses use
// Gerrit's JSON shapes and the XSSI prefix ()]}') via the existing
// writeJSON helper.
//
// Auth: /a/ prefixed routes require authentication (session cookie or
// HTTP Basic). Non-/a/ routes allow anonymous access where the underlying
// handler permits it.

// compatRoutes registers Gerrit-compatible API routes on the mux.
func (s *Server) compatRoutes() {
	// Server info (no auth needed for version discovery).
	s.mux.HandleFunc("GET /a/config/server/version", s.compatVersion)
	s.mux.HandleFunc("GET /a/config/server/info", s.compatServerInfo)
	s.mux.HandleFunc("GET /config/server/version", s.compatVersion)

	// Account.
	s.mux.HandleFunc("GET /a/accounts/self", s.compatAccountSelf)

	// Changes.
	s.mux.HandleFunc("GET /a/changes/", s.compatListChanges)
	s.mux.HandleFunc("GET /a/changes/{id}", s.compatGetChange)
	s.mux.HandleFunc("GET /a/changes/{id}/detail", s.compatGetChange)
	s.mux.HandleFunc("POST /a/changes/{id}/revisions/{rev}/review", s.compatPostReview)
	s.mux.HandleFunc("GET /a/changes/{id}/revisions/{rev}/files", s.compatListFiles)
	s.mux.HandleFunc("GET /a/changes/{id}/submitted_together", s.compatSubmittedTogether)

	// Projects.
	s.mux.HandleFunc("GET /a/projects/{name}", s.compatGetProject)
}

// compatWriteJSON writes a JSON response with the Gerrit XSSI prefix.
// It delegates to the existing writeJSON which already includes the prefix.
func compatWriteJSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, v)
}

// compatAuth resolves the account from session or HTTP Basic auth.
// Returns nil if unauthenticated.
func (s *Server) compatAuth(r *http.Request) *store.Account {
	return s.optionalAccount(r)
}

// compatRequireAuth returns the authenticated account or writes 401.
func (s *Server) compatRequireAuth(w http.ResponseWriter, r *http.Request) *store.Account {
	acct := s.compatAuth(r)
	if acct == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="Gerrit"`)
		compatWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return nil
	}
	return acct
}

// ---------- server info ----------

func (s *Server) compatVersion(w http.ResponseWriter, r *http.Request) {
	compatWriteJSON(w, http.StatusOK, "gerrit-go 0.2.0")
}

func (s *Server) compatServerInfo(w http.ResponseWriter, r *http.Request) {
	compatWriteJSON(w, http.StatusOK, map[string]any{
		"version": "gerrit-go 0.2.0",
		"gerrit": map[string]any{
			"all_projects_name": "All-Projects",
			"all_users_name":    "All-Users",
		},
		"download": map[string]any{
			"schemes": map[string]any{
				"http": map[string]any{"url": r.Host + "/git/${project}"},
				"ssh":  map[string]any{"url": "ssh://" + r.Host + ":29418/${project}"},
			},
		},
	})
}

// ---------- accounts ----------

func (s *Server) compatAccountSelf(w http.ResponseWriter, r *http.Request) {
	acct := s.compatRequireAuth(w, r)
	if acct == nil {
		return
	}
	compatWriteJSON(w, http.StatusOK, map[string]any{
		"_account_id": acct.ID,
		"name":        orDefault(acct.FullName, acct.Username),
		"username":    acct.Username,
		"email":       acct.Email,
	})
}

// ---------- changes ----------

// compatListChanges handles GET /a/changes/?q=...&n=...&o=...
func (s *Server) compatListChanges(w http.ResponseWriter, r *http.Request) {
	acct := s.compatAuth(r)
	q := r.URL.Query().Get("q")
	limit := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 {
		limit = n
	}
	offset := 0
	if start, err := strconv.Atoi(r.URL.Query().Get("start")); err == nil && start > 0 {
		offset = start
	} else if sStart, err := strconv.Atoi(r.URL.Query().Get("S")); err == nil && sStart > 0 {
		offset = sStart
	}

	root := store.ParseQuery(q, acct)
	changes, _, err := s.db.SearchChangesParsed(root, limit, offset)
	if err != nil {
		compatWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	visible := make([]*store.Change, 0, len(changes))
	for _, c := range changes {
		if s.canReadChange(acct, c) {
			visible = append(visible, c)
		}
	}
	nums := make([]int64, len(visible))
	for i, c := range visible {
		nums[i] = c.Number
	}

	patchsets, _ := s.db.GetCurrentPatchSetsBatch(visible)
	votesByChange, _ := s.db.ListVotesBatch(nums)
	reviewersByChange, _ := s.db.ListReviewersBatch(nums)
	var starred map[int64]bool
	if acct != nil {
		starred, _ = s.db.ListStarredBatch(acct.ID, nums)
	}

	out := make([]map[string]any, 0, len(visible))
	for _, c := range visible {
		info := s.compatChangeInfo(c, patchsets[c.Number], votesByChange[c.Number], reviewersByChange[c.Number], starred[c.Number], r)
		out = append(out, info)
	}
	compatWriteJSON(w, http.StatusOK, out)
}

// compatGetChange handles GET /a/changes/{id} and /a/changes/{id}/detail.
func (s *Server) compatGetChange(w http.ResponseWriter, r *http.Request) {
	acct := s.compatAuth(r)
	c, ok := s.compatResolveChange(w, r)
	if !ok {
		return
	}
	if !s.canReadChange(acct, c) {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "change not found"})
		return
	}

	ps, _ := s.db.GetPatchSet(c.Number, c.CurrentPS)
	votes, _ := s.db.ListVotes(c.Number)
	reviewers, _ := s.db.ListReviewers(c.Number)
	isStarred := false
	if acct != nil {
		isStarred = s.db.IsStarred(acct.ID, c.Number)
	}

	info := s.compatChangeInfo(c, ps, votes, reviewers, isStarred, r)

	// Add detail fields.
	messages, _ := s.db.ListChangeMessages(c.Number)
	msgOut := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		mi := map[string]any{
			"id":      strconv.FormatInt(m.ID, 10),
			"message": m.Message,
			"date":    m.Created.Format(time.RFC3339),
		}
		if m.AuthorID > 0 {
			if a, err := s.db.GetAccount(m.AuthorID); err == nil {
				mi["author"] = map[string]any{
					"_account_id": a.ID,
					"name":        orDefault(a.FullName, a.Username),
					"username":    a.Username,
				}
			}
		}
		msgOut = append(msgOut, mi)
	}
	info["messages"] = msgOut

	compatWriteJSON(w, http.StatusOK, info)
}

// compatChangeInfo builds a Gerrit-compatible ChangeInfo map.
func (s *Server) compatChangeInfo(c *store.Change, ps *store.PatchSet, votes []*store.VoteInfo, reviewers []*store.Reviewer, starred bool, r *http.Request) map[string]any {
	info := map[string]any{
		"id":                fmt.Sprintf("%s~%s~%s", c.Project, c.Branch, c.ChangeID),
		"project":           c.Project,
		"branch":            c.Branch,
		"change_id":         c.ChangeID,
		"subject":           c.Subject,
		"status":            c.Status,
		"_number":           c.Number,
		"created":           c.Created.Format(time.RFC3339),
		"updated":           c.Updated.Format(time.RFC3339),
		"work_in_progress":  c.WorkInProgress,
		"is_private":        c.Private,
		"owner":             s.compatAccountRef(c.OwnerID, c.OwnerName, c.OwnerUser, c.OwnerEmail),
		"labels":            s.compatLabels(votes),
		"reviewers":         s.compatReviewers(reviewers),
		"current_patch_set": c.CurrentPS,
	}
	if c.Topic != "" {
		info["topic"] = c.Topic
	}
	if ps != nil {
		info["current_revision"] = ps.CommitSHA
		info["revisions"] = map[string]any{
			ps.CommitSHA: map[string]any{
				"_number": ps.Number,
				"ref":     fmt.Sprintf("refs/changes/%02d/%d/%d", c.Number%100, c.Number, ps.Number),
				"commit": map[string]any{
					"commit": ps.CommitSHA,
				},
			},
		}
	}
	if starred {
		info["starred"] = true
	}
	return info
}

func (s *Server) compatAccountRef(id int64, name, username, email string) map[string]any {
	return map[string]any{
		"_account_id": id,
		"name":        orDefault(name, username),
		"username":    username,
		"email":       email,
	}
}

func (s *Server) compatLabels(votes []*store.VoteInfo) map[string]any {
	labels := map[string]any{}
	for _, v := range votes {
		l, ok := labels[v.Label].(map[string]any)
		if !ok {
			l = map[string]any{"all": []any{}}
			labels[v.Label] = l
		}
		l["all"] = append(l["all"].([]any), map[string]any{
			"_account_id": v.AccountID,
			"name":        orDefault(v.AccountName, v.AccountUser),
			"username":    v.AccountUser,
			"value":       v.Value,
			"date":        time.Now().Format(time.RFC3339), // votes table has no timestamp; use now
		})
	}
	return labels
}

func (s *Server) compatReviewers(reviewers []*store.Reviewer) map[string]any {
	out := map[string]any{
		"REVIEWER": []any{},
	}
	for _, rv := range reviewers {
		out["REVIEWER"] = append(out["REVIEWER"].([]any), map[string]any{
			"_account_id": rv.AccountID,
			"name":        rv.Name,
			"username":    rv.Username,
			"email":       rv.Email,
		})
	}
	return out
}

// compatResolveChange resolves a change by number, project~branch~change_id, or bare Change-Id.
func (s *Server) compatResolveChange(w http.ResponseWriter, r *http.Request) (*store.Change, bool) {
	id := r.PathValue("id")
	// Try numeric first.
	if num, err := strconv.ParseInt(id, 10, 64); err == nil && num > 0 {
		c, err := s.db.GetChange(num)
		if err == nil {
			return c, true
		}
	}
	// Try project~branch~change_id.
	parts := strings.SplitN(id, "~", 3)
	if len(parts) == 3 {
		c, err := s.db.GetChangeByChangeID(parts[0], parts[1], parts[2])
		if err == nil {
			return c, true
		}
	}
	// Try bare change_id.
	c, err := s.db.GetChangeByChangeIDOnly(id)
	if err != nil {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "change not found"})
		return nil, false
	}
	return c, true
}

// ---------- review ----------

// compatPostReview handles POST /a/changes/{id}/revisions/{rev}/review.
// This is the endpoint Jenkins Gerrit Trigger and git-review use to vote.
func (s *Server) compatPostReview(w http.ResponseWriter, r *http.Request) {
	acct := s.compatRequireAuth(w, r)
	if acct == nil {
		return
	}
	c, ok := s.compatResolveChange(w, r)
	if !ok {
		return
	}
	if !s.canReadChange(acct, c) {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "change not found"})
		return
	}

	var req struct {
		Message  string         `json:"message"`
		Labels   map[string]int `json:"labels"`
		Comments map[string][]struct {
			Line    int    `json:"line"`
			Message string `json:"message"`
		} `json:"comments"`
		Tag      string `json:"tag"`
		Notify   string `json:"notify"`
		OnBehalfOf int64 `json:"on_behalf_of"`
	}
	if err := decodeJSON(r, &req); err != nil {
		compatWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ref := branchRef(c.Branch)

	// Check comment permission.
	if strings.TrimSpace(req.Message) != "" || len(req.Comments) > 0 {
		if !s.can(acct, c.Project, ref, PermComment) {
			compatWriteJSON(w, http.StatusForbidden, map[string]string{"error": "comment not permitted"})
			return
		}
	}

	// Add as reviewer.
	s.db.AddReviewer(c.Number, acct.ID)

	// Process labels.
	for label, value := range req.Labels {
		acc := s.checkAccess(acct, c.Project, ref, "label-"+label)
		if !acc.allowed {
			compatWriteJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("label %s: not permitted", label),
			})
			return
		}
		if value < acc.min || value > acc.max {
			compatWriteJSON(w, http.StatusForbidden, map[string]string{
				"error": fmt.Sprintf("label %s value %d out of range [%d, %d]", label, value, acc.min, acc.max),
			})
			return
		}
		v := &store.Vote{ChangeNumber: c.Number, PatchSet: c.CurrentPS, AccountID: acct.ID, Label: label, Value: value}
		if value == 0 {
			s.db.DeleteVote(v.ChangeNumber, v.PatchSet, v.AccountID, v.Label)
		} else {
			s.db.SetVote(v)
		}
	}

	// Post message.
	if strings.TrimSpace(req.Message) != "" {
		msg := &store.ChangeMessage{
			ChangeNum: c.Number,
			PatchSet:  c.CurrentPS,
			Type:      "comment",
			AuthorID:  acct.ID,
			Message:   req.Message,
		}
		s.db.AddChangeMessage(msg)
	}

	// Post inline comments.
	for file, comments := range req.Comments {
		for _, cc := range comments {
			comment := &store.Comment{
				ChangeNum: c.Number,
				PatchSet:  c.CurrentPS,
				File:      file,
				Line:      cc.Line,
				Message:   cc.Message,
				AuthorID:  acct.ID,
			}
			s.db.CreateComment(comment)
		}
	}

	// Return updated change info.
	ps, _ := s.db.GetPatchSet(c.Number, c.CurrentPS)
	votes, _ := s.db.ListVotes(c.Number)
	reviewers, _ := s.db.ListReviewers(c.Number)
	info := s.compatChangeInfo(c, ps, votes, reviewers, false, r)
	compatWriteJSON(w, http.StatusOK, info)
}

// ---------- files ----------

func (s *Server) compatListFiles(w http.ResponseWriter, r *http.Request) {
	acct := s.compatAuth(r)
	c, ok := s.compatResolveChange(w, r)
	if !ok {
		return
	}
	if !s.canReadChange(acct, c) {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "change not found"})
		return
	}

	rev := r.PathValue("rev")
	psNum := c.CurrentPS
	if rev != "current" {
		if n, err := strconv.Atoi(rev); err == nil && n > 0 {
			psNum = n
		}
	}

	files, err := s.db.ListPatchSetFiles(c.Number, psNum)
	if err != nil {
		compatWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := map[string]any{}
	for _, f := range files {
		out[f] = map[string]any{
			"status":      "M",
			"binary":      false,
			"old_path":    nil,
			"lines_inserted": 0,
			"lines_deleted": 0,
		}
	}
	compatWriteJSON(w, http.StatusOK, out)
}

// ---------- submitted_together ----------

func (s *Server) compatSubmittedTogether(w http.ResponseWriter, r *http.Request) {
	acct := s.compatAuth(r)
	c, ok := s.compatResolveChange(w, r)
	if !ok {
		return
	}
	if !s.canReadChange(acct, c) {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "change not found"})
		return
	}
	// For now, return just the change itself. Full topic/relation-chain
	// traversal can be added later.
	ps, _ := s.db.GetPatchSet(c.Number, c.CurrentPS)
	votes, _ := s.db.ListVotes(c.Number)
	reviewers, _ := s.db.ListReviewers(c.Number)
	info := s.compatChangeInfo(c, ps, votes, reviewers, false, r)
	compatWriteJSON(w, http.StatusOK, []map[string]any{info})
}

// ---------- projects ----------

func (s *Server) compatGetProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Gerrit uses URL-encoded project names (e.g. "my%2Fproject").
	name = strings.ReplaceAll(name, "%2F", "/")
	if !s.canReadProject(s.compatAuth(r), name) {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	p, err := s.db.GetProject(name)
	if err != nil {
		compatWriteJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	out := map[string]any{
		"id":          p.Name,
		"name":        p.Name,
		"description": p.Description,
		"state":       "ACTIVE",
		"branches":    map[string]any{},
	}
	if p.Parent != "" {
		out["parent"] = p.Parent
	}
	compatWriteJSON(w, http.StatusOK, out)
}

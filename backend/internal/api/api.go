package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/auth"
	"gerrit-go/internal/gitsvc"
	"gerrit-go/internal/store"
)

type Server struct {
	db     *store.DB
	auth   *auth.Service
	git    *gitsvc.Service
	static string
	mux    *http.ServeMux
}

func NewRouter(db *store.DB, authSvc *auth.Service, gitSvc *gitsvc.Service, staticDir string) http.Handler {
	s := &Server{db: db, auth: authSvc, git: gitSvc, static: staticDir, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	mux := s.mux

	// Git smart HTTP (clone / fetch / push, incl. refs/for magic branch),
	// served by the system `git http-backend` for full protocol compatibility.
	gitHandler := s.gitMiddleware()
	mux.Handle("GET /git/", gitHandler)
	mux.Handle("POST /git/", gitHandler)

	// Auth.
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// Accounts.
	mux.HandleFunc("GET /accounts/self", s.handleAccountSelf)
	mux.HandleFunc("PUT /accounts/self/password", s.handleSetPassword)
	mux.HandleFunc("GET /accounts/", s.requireAuth(s.handleListAccounts))
	mux.HandleFunc("POST /accounts/", s.requireAuth(s.handleCreateAccount))

	// Projects.
	mux.HandleFunc("GET /projects/", s.handleListProjects)
	mux.HandleFunc("POST /projects/", s.requireAuth(s.handleCreateProject))
	mux.HandleFunc("GET /projects/{name}", s.handleGetProject)
	mux.HandleFunc("GET /projects/{name}/branches", s.handleListBranches)
	mux.HandleFunc("GET /projects/{name}/commits", s.handleListCommits)
	mux.HandleFunc("GET /projects/{name}/tree", s.handleListTree)
	mux.HandleFunc("GET /projects/{name}/file", s.handleFileContent)

	// Changes.
	mux.HandleFunc("GET /changes/", s.handleListChanges)
	mux.HandleFunc("GET /changes/{num}", s.handleChangeDetail)
	mux.HandleFunc("GET /changes/{num}/revisions", s.handleListRevisions)
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/files", s.handleRevisionFiles)
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/file", s.handleRevisionFileContent)
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/patch", s.handleRevisionPatch)
	mux.HandleFunc("GET /changes/{num}/comments", s.handleListComments)
	mux.HandleFunc("POST /changes/{num}/review", s.requireAuth(s.handleReview))
	mux.HandleFunc("POST /changes/{num}/submit", s.requireAuth(s.handleSubmit))
	mux.HandleFunc("POST /changes/{num}/abandon", s.requireAuth(s.handleAbandon))
	mux.HandleFunc("POST /changes/{num}/restore", s.requireAuth(s.handleRestore))

	// Gerrit-compatible authenticated alias prefix: /a/...
	mux.Handle("/a/", http.StripPrefix("/a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct, err := s.auth.CurrentAccount(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		s.mux.ServeHTTP(w, r.WithContext(withAccount(r.Context(), acct)))
	})))

	// Static frontend + SPA fallback.
	mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ---------- helpers ----------

// writeJSON emits Gerrit-style JSON with the XSSI ")]}'" prefix so that
// existing Gerrit tooling / clients can consume the responses unchanged.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(code)
	io.WriteString(w, ")]}'\n")
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acct, err := s.auth.CurrentAccount(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		r = r.WithContext(withAccount(r.Context(), acct))
		next(w, r)
	}
}

func (s *Server) account(r *http.Request) *store.Account {
	return accountFrom(r.Context())
}

func decodeJSON(r *http.Request, v any) error {
	body := io.LimitReader(r.Body, 1<<20)
	dec := json.NewDecoder(body)
	return dec.Decode(v)
}

// ---------- auth handlers ----------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	acct, err := s.auth.Authenticate(req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	id, err := s.auth.CreateSession(acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	s.auth.SetSessionCookie(w, id)
	writeJSON(w, http.StatusOK, accountInfo(acct))
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		FullName string `json:"full_name"`
		Email    string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	acct, err := s.auth.Register(req.Username, req.Password, req.FullName, req.Email)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	id, _ := s.auth.CreateSession(acct.ID)
	s.auth.SetSessionCookie(w, id)
	writeJSON(w, http.StatusCreated, accountInfo(acct))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(r)
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleAccountSelf(w http.ResponseWriter, r *http.Request) {
	acct, err := s.auth.CurrentAccount(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	writeJSON(w, http.StatusOK, accountInfo(acct))
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "new_password is required")
		return
	}
	if _, err := s.auth.Authenticate(acct.Username, req.OldPassword); err != nil {
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash := hashPassword(req.NewPassword)
	if err := s.db.UpdateAccountPassword(acct.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update password")
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !acct.Admin {
		writeErr(w, http.StatusForbidden, "admin only")
		return
	}
	list, err := s.db.ListAccounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, accountInfo(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	actor := s.account(r)
	if !actor.Admin {
		writeErr(w, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		FullName string `json:"name"`
		Email    string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	acct, err := s.auth.Register(req.Username, req.Password, req.FullName, req.Email)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, accountInfo(acct))
}

func accountInfo(a *store.Account) map[string]any {
	return map[string]any{
		"_account_id": a.ID,
		"username":    a.Username,
		"name":        orDefault(a.FullName, a.Username),
		"email":       a.Email,
		"admin":       a.Admin,
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ---------- git smart HTTP ----------

// projectFromPath extracts the project name from a git URL path such as
// "/my/project.git/info/refs" or "/my/project.git".
func projectFromPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	for _, suffix := range []string{"/info/refs", "/git-upload-pack", "/git-receive-pack"} {
		p = strings.TrimSuffix(p, suffix)
	}
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimSuffix(p, ".git")
	if p == "" || strings.Contains(p, "..") {
		return ""
	}
	return p
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (s *Server) gitMiddleware() http.Handler {
	backend := s.git.Backend()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/git/")
		project := projectFromPath(rest)
		if project == "" || !s.git.ProjectExists(project) {
			http.Error(w, "repository not found", http.StatusNotFound)
			return
		}
		// PATH_INFO as git http-backend expects it: "/<project>.git/<service>".
		suffix := strings.TrimPrefix(strings.TrimPrefix(rest, project), ".git")
		pathInfo := "/" + project + ".git" + suffix

		isPush := strings.HasSuffix(r.URL.Path, "git-receive-pack") ||
			r.URL.Query().Get("service") == "git-receive-pack"

		remoteUser := ""
		var pusher *store.Account
		if isPush {
			acct, err := s.auth.CurrentAccount(r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="gerrit-go"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			pusher = acct
			remoteUser = acct.Username
		}

		var rw http.ResponseWriter = w
		if isPush {
			rw = &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		}
		if err := backend.ServeHTTP(rw, r, project, pathInfo, remoteUser); err != nil {
			fmt.Fprintf(os.Stderr, "git http-backend (%s): %v\n", project, err)
		}
		if isPush {
			if sr, ok := rw.(*statusRecorder); ok && sr.status == http.StatusOK {
				if err := s.git.ProcessReceivePack(project, pusher); err != nil {
					fmt.Fprintf(os.Stderr, "post-receive processing for %s: %v\n", project, err)
				}
			}
		}
	})
}

// ---------- projects ----------

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListProjects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make(map[string]any, len(list))
	for _, p := range list {
		out[p.Name] = map[string]any{
			"name":        p.Name,
			"description": p.Description,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Name = strings.TrimSuffix(req.Name, ".git")
	if err := s.git.CreateProject(req.Name, req.Description); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, gitsvc.ErrProjectExists) {
			code = http.StatusConflict
		}
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "description": req.Description})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.handleStatic(w, r)
		return
	}
	name := r.PathValue("name")
	p, err := s.db.GetProject(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": p.Name, "description": p.Description})
}

func (s *Server) handleListBranches(w http.ResponseWriter, r *http.Request) {
	branches, err := s.git.ListBranches(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleListCommits(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	commits, err := s.git.ListCommits(r.PathValue("name"), r.URL.Query().Get("revision"), limit)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

func (s *Server) handleListTree(w http.ResponseWriter, r *http.Request) {
	entries, err := s.git.ListTree(r.PathValue("name"), r.URL.Query().Get("revision"), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	q := r.URL.Query()
	content, err := s.git.FileContent(name, q.Get("revision"), q.Get("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if q.Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(content)
		return
	}
	writeJSON(w, http.StatusOK, base64.StdEncoding.EncodeToString(content))
}

// ---------- changes ----------

func changeInfo(c *store.Change) map[string]any {
	return map[string]any{
		"id":        fmt.Sprintf("%s~%s~%s", c.Project, c.Branch, c.ChangeID),
		"_number":   c.Number,
		"project":   c.Project,
		"branch":    c.Branch,
		"change_id": c.ChangeID,
		"subject":   c.Subject,
		"status":    c.Status,
		"owner": map[string]any{
			"_account_id": c.OwnerID,
			"name":        orDefault(c.OwnerName, c.OwnerUser),
			"username":    c.OwnerUser,
			"email":       c.OwnerEmail,
		},
		"created":  c.Created.Format(time.RFC3339),
		"updated":  c.Updated.Format(time.RFC3339),
	}
}

func (s *Server) parseChangeNum(r *http.Request) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue("num"), 10, 64)
	return n, err == nil && n > 0
}

func (s *Server) parsePS(r *http.Request, current int) int {
	v := r.PathValue("ps")
	if v == "current" || v == "" {
		return current
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return current
	}
	return n
}

func (s *Server) handleListChanges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	status := ""
	text := ""
	for _, part := range strings.Fields(q) {
		switch {
		case strings.HasPrefix(part, "status:"):
			switch strings.ToLower(strings.TrimPrefix(part, "status:")) {
			case "open":
				status = "NEW"
			case "merged":
				status = "MERGED"
			case "abandoned":
				status = "ABANDONED"
			}
		case strings.HasPrefix(part, "project:"):
			text = strings.TrimPrefix(part, "project:")
		default:
			text = part
		}
	}
	changes, err := s.db.ListChanges(status, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(changes))
	for _, c := range changes {
		if text != "" &&
			!strings.Contains(c.Project, text) &&
			!strings.Contains(c.Subject, text) &&
			!strings.Contains(strconv.FormatInt(c.Number, 10), text) {
			continue
		}
		info := changeInfo(c)
		if ps, err := s.db.GetPatchSet(c.Number, c.CurrentPS); err == nil {
			info["current_revision"] = ps.CommitSHA
		}
		info["current_ps"] = c.CurrentPS
		info["labels"] = s.labelsFor(c.Number)
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) labelsFor(changeNum int64) map[string]map[string]any {
	votes, err := s.db.ListVotes(changeNum)
	if err != nil {
		return map[string]map[string]any{}
	}
	labelInfo := map[string]map[string]any{}
	for _, v := range votes {
		l, ok := labelInfo[v.Label]
		if !ok {
			l = map[string]any{"all": []any{}}
			labelInfo[v.Label] = l
		}
		l["all"] = append(l["all"].([]any), map[string]any{
			"_account_id": v.AccountID,
			"name":        orDefault(v.AccountName, v.AccountUser),
			"username":    v.AccountUser,
			"value":       v.Value,
			"patch_set":   v.PatchSet,
		})
	}
	return labelInfo
}

func (s *Server) handleChangeDetail(w http.ResponseWriter, r *http.Request) {
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
	info := changeInfo(c)
	info["current_ps"] = c.CurrentPS
	if c.Submitted != nil {
		info["submitted"] = c.Submitted.Format(time.RFC3339)
	}

	pss, _ := s.db.ListPatchSets(c.Number)
	if len(pss) > 0 {
		info["current_revision"] = pss[len(pss)-1].CommitSHA
		revs := map[string]any{}
		for _, ps := range pss {
			revs[ps.CommitSHA] = map[string]any{
				"_number": ps.Number,
				"commit":  ps.CommitSHA,
				"created": ps.Created.Format(time.RFC3339),
				"author": map[string]any{
					"name":  ps.AuthorName,
					"email": ps.AuthorEmail,
				},
				"message": ps.Message,
			}
		}
		info["revisions"] = revs
	}

	info["labels"] = s.labelsFor(c.Number)

	// Submittable hint: has Code-Review +2 and status NEW.
	submittable := c.Status == "NEW"
	if submittable {
		hasPlus2 := false
		if votes, err := s.db.ListVotes(c.Number); err == nil {
			for _, v := range votes {
				if v.Label == "Code-Review" && v.Value == 2 {
					hasPlus2 = true
				}
			}
		}
		submittable = hasPlus2
	}
	info["submittable"] = submittable

	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	pss, err := s.db.ListPatchSets(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	out := make([]map[string]any, 0, len(pss))
	for _, ps := range pss {
		out = append(out, map[string]any{
			"_number": ps.Number,
			"commit":  ps.CommitSHA,
			"created": ps.Created.Format(time.RFC3339),
			"author":  map[string]any{"name": ps.AuthorName, "email": ps.AuthorEmail},
			"message": ps.Message,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevisionFiles(w http.ResponseWriter, r *http.Request) {
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
	psNum := s.parsePS(r, c.CurrentPS)
	diffs, err := s.git.PatchSetDiff(num, psNum)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, diffs)
}

func (s *Server) handleRevisionFileContent(w http.ResponseWriter, r *http.Request) {
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
	psNum := s.parsePS(r, c.CurrentPS)
	content, err := s.git.FileAtPatchSet(num, psNum, r.URL.Query().Get("path"), r.URL.Query().Get("side"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(content)
		return
	}
	writeJSON(w, http.StatusOK, base64.StdEncoding.EncodeToString(content))
}

func (s *Server) handleRevisionPatch(w http.ResponseWriter, r *http.Request) {
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
	psNum := s.parsePS(r, c.CurrentPS)
	patch, err := s.git.PatchText(num, psNum)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, base64.StdEncoding.EncodeToString([]byte(patch)))
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	comments, err := s.db.ListComments(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	out := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		out = append(out, map[string]any{
			"id":        c.ID,
			"patch_set": c.PatchSet,
			"path":      c.File,
			"line":      c.Line,
			"message":   c.Message,
			"in_reply_to": c.InReplyTo,
			"updated":   c.Created.Format(time.RFC3339),
			"author": map[string]any{
				"_account_id": c.AuthorID,
				"name":        orDefault(c.AuthorName, c.AuthorUser),
				"username":    c.AuthorUser,
			},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Labels   map[string]int `json:"labels"`
		Message  string         `json:"message"`
		Comments map[string][]struct {
			Line      int    `json:"line"`
			Message   string `json:"message"`
			InReplyTo int64  `json:"in_reply_to"`
		} `json:"comments"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	allowed := map[string][2]int{"Code-Review": {-2, 2}, "Verified": {-1, 1}}
	for label, value := range req.Labels {
		bounds, ok := allowed[label]
		if !ok {
			writeErr(w, http.StatusBadRequest, "label not permitted: "+label)
			return
		}
		if value < bounds[0] || value > bounds[1] {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("%s value out of range", label))
			return
		}
		v := &store.Vote{ChangeNumber: c.Number, PatchSet: c.CurrentPS, AccountID: acct.ID, Label: label, Value: value}
		if value == 0 {
			if err := s.db.DeleteVote(v.ChangeNumber, v.PatchSet, v.AccountID, v.Label); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			continue
		}
		if err := s.db.SetVote(v); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for file, list := range req.Comments {
		for _, cm := range list {
			if strings.TrimSpace(cm.Message) == "" {
				continue
			}
			s.db.CreateComment(&store.Comment{
				ChangeNum: c.Number, PatchSet: c.CurrentPS, File: file,
				Line: cm.Line, Message: cm.Message, AuthorID: acct.ID, InReplyTo: cm.InReplyTo,
			})
		}
	}

	if strings.TrimSpace(req.Message) != "" {
		s.db.CreateComment(&store.Comment{
			ChangeNum: c.Number, PatchSet: c.CurrentPS, File: "",
			Line: 0, Message: req.Message, AuthorID: acct.ID,
		})
	}

	s.db.TouchChange(c.Number)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
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
	if c.Status != "NEW" {
		writeErr(w, http.StatusConflict, "change is not open")
		return
	}
	votes, _ := s.db.ListVotes(c.Number)
	hasPlus2 := false
	for _, v := range votes {
		if v.Label == "Code-Review" && v.Value == 2 {
			hasPlus2 = true
		}
	}
	if !hasPlus2 {
		writeErr(w, http.StatusConflict, "change requires Code-Review +2 before it can be submitted")
		return
	}
	if err := s.git.Submit(c.Number); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "MERGED"})
}

func (s *Server) handleAbandon(w http.ResponseWriter, r *http.Request) {
	s.setChangeStatus(w, r, "ABANDONED", "NEW")
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	s.setChangeStatus(w, r, "NEW", "ABANDONED")
}

func (s *Server) setChangeStatus(w http.ResponseWriter, r *http.Request, to, requireFrom string) {
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
	if c.Status != requireFrom {
		writeErr(w, http.StatusConflict, "unexpected change status: "+c.Status)
		return
	}
	if err := s.db.UpdateChangeStatus(c.Number, to, nil); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": to})
}

// ---------- static frontend ----------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == "" {
		http.Error(w, "gerrit-go backend running (no frontend bundle configured)", http.StatusOK)
		return
	}
	urlPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if urlPath == "." || urlPath == "/" {
		urlPath = "index.html"
	}
	full := filepath.Join(s.static, filepath.FromSlash(urlPath))
	if !strings.HasPrefix(full, filepath.Clean(s.static)+string(os.PathSeparator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	// SPA fallback.
	http.ServeFile(w, r, filepath.Join(s.static, "index.html"))
}

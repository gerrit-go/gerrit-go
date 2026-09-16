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
	"sort"
	"strconv"
	"strings"
	"time"

	"gerrit-go/internal/auth"
	"gerrit-go/internal/gitsvc"
	"gerrit-go/internal/i18n"
	"gerrit-go/internal/metrics"
	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
	"gerrit-go/internal/webhook"
)

type Server struct {
	db            *store.DB
	auth          *auth.Service
	git           *gitsvc.Service
	notify        *notify.Notifier
	hook          *webhook.Dispatcher
	metrics       *metrics.Registry
	static        string
	mux           *http.ServeMux
	allowRegister bool
	limiter       *loginLimiter
}

func NewRouter(db *store.DB, authSvc *auth.Service, gitSvc *gitsvc.Service, notifier *notify.Notifier, staticDir string, allowRegister bool) http.Handler {
	return NewServer(db, authSvc, gitSvc, notifier, staticDir, allowRegister).Handler()
}

// NewServer builds the API server. Callers that only need the HTTP handler can
// use NewRouter; NewServer exposes the *Server so auxiliary listeners (e.g. the
// git+ssh daemon) can share the same dependencies and permission checks.
func NewServer(db *store.DB, authSvc *auth.Service, gitSvc *gitsvc.Service, notifier *notify.Notifier, staticDir string, allowRegister bool) *Server {
	reg := metrics.New()
	hook := webhook.New(db, 10*time.Second)
	hook.SetCounters(reg.IncWebhookSent, reg.IncWebhookFail)
	s := &Server{
		db:            db,
		auth:          authSvc,
		git:           gitSvc,
		notify:        notifier,
		hook:          hook,
		metrics:       reg,
		static:        staticDir,
		mux:           http.NewServeMux(),
		allowRegister: allowRegister,
		limiter:       newLoginLimiter(5, 10*time.Minute, 15*time.Minute),
	}
	s.routes()
	return s
}

// Handler returns the HTTP handler (with i18n middleware) for the server.
func (s *Server) Handler() http.Handler {
	return i18n.Middleware(s)
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
	mux.HandleFunc("GET /config", s.handleConfig)
	mux.HandleFunc("GET /login/oauth", s.handleOAuthLogin)
	mux.HandleFunc("GET /oauth/callback", s.handleOAuthCallback)

	// Accounts.
	mux.HandleFunc("GET /accounts/self", s.handleAccountSelf)
	mux.HandleFunc("PUT /accounts/self", s.requireAuth(s.handleUpdateSelf))
	mux.HandleFunc("PUT /accounts/self/password", s.handleSetPassword)
	mux.HandleFunc("GET /accounts/self/http-password", s.requireAuth(s.handleHTTPPasswordStatus))
	mux.HandleFunc("PUT /accounts/self/http-password", s.requireAuth(s.handleGenerateHTTPPassword))
	mux.HandleFunc("DELETE /accounts/self/http-password", s.requireAuth(s.handleClearHTTPPassword))
	mux.HandleFunc("GET /accounts/self/sshkeys", s.requireAuth(s.handleListSSHKeys))
	mux.HandleFunc("POST /accounts/self/sshkeys", s.requireAuth(s.handleAddSSHKey))
	mux.HandleFunc("DELETE /accounts/self/sshkeys/{id}", s.requireAuth(s.handleDeleteSSHKey))
	mux.HandleFunc("GET /accounts/self/notifications", s.requireAuth(s.handleListNotifications))
	mux.HandleFunc("POST /accounts/self/notifications/read", s.requireAuth(s.handleMarkNotificationsRead))
	mux.HandleFunc("GET /accounts/self/watched", s.requireAuth(s.handleListWatched))
	mux.HandleFunc("GET /accounts/self/2fa", s.requireAuth(s.handleGet2FA))
	mux.HandleFunc("POST /accounts/self/2fa/enroll", s.requireAuth(s.handleEnroll2FA))
	mux.HandleFunc("POST /accounts/self/2fa/enable", s.requireAuth(s.handleEnable2FA))
	mux.HandleFunc("POST /accounts/self/2fa/disable", s.requireAuth(s.handleDisable2FA))
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
	mux.HandleFunc("GET /projects/{name}/access", s.handleGetAccess)
	mux.HandleFunc("PUT /projects/{name}/access", s.requireAuth(s.handleSetAccess))
	mux.HandleFunc("PUT /projects/{name}/config", s.requireAuth(s.handleSetProjectConfig))
	mux.HandleFunc("PUT /projects/{name}/watch", s.requireAuth(s.handleWatchProject))
	mux.HandleFunc("DELETE /projects/{name}/watch", s.requireAuth(s.handleUnwatchProject))
	mux.HandleFunc("POST /projects/{name}/branches", s.requireAuth(s.handleCreateBranch))
	mux.HandleFunc("DELETE /projects/{name}/branches/{branch}", s.requireAuth(s.handleDeleteBranch))
	mux.HandleFunc("GET /projects/{name}/tags", s.handleListTags)
	mux.HandleFunc("POST /projects/{name}/tags", s.requireAuth(s.handleCreateTag))
	mux.HandleFunc("DELETE /projects/{name}/tags/{tag}", s.requireAuth(s.handleDeleteTag))
	mux.HandleFunc("PUT /projects/{name}/edit", s.requireAuth(s.handleEditFile))
	mux.HandleFunc("PUT /projects/{name}/state", s.requireAuth(s.handleSetProjectState))
	mux.HandleFunc("DELETE /projects/{name}", s.requireAuth(s.handleDeleteProject))

	// Groups.
	mux.HandleFunc("GET /groups/", s.handleListGroups)
	mux.HandleFunc("POST /groups/", s.requireAuth(s.handleCreateGroup))
	mux.HandleFunc("GET /groups/{id}", s.handleGetGroup)
	mux.HandleFunc("DELETE /groups/{id}", s.requireAuth(s.handleDeleteGroup))
	mux.HandleFunc("GET /groups/{id}/members", s.handleListGroupMembers)
	mux.HandleFunc("PUT /groups/{id}/members/{account}", s.requireAuth(s.handleAddGroupMember))
	mux.HandleFunc("DELETE /groups/{id}/members/{account}", s.requireAuth(s.handleDeleteGroupMember))

	// Changes.
	mux.HandleFunc("GET /changes/", s.handleListChanges)
	mux.HandleFunc("GET /changes/{num}", s.handleChangeDetail)
	mux.HandleFunc("GET /changes/{num}/revisions", s.handleListRevisions)
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/files", s.handleRevisionFiles)
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/file", s.handleRevisionFileContent)
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/patch", s.handleRevisionPatch)
	mux.HandleFunc("GET /changes/{num}/comments", s.handleListComments)
	mux.HandleFunc("PUT /changes/{num}/comments/{id}/resolve", s.requireAuth(s.handleResolveComment))
	mux.HandleFunc("GET /changes/{num}/drafts", s.requireAuth(s.handleListDrafts))
	mux.HandleFunc("PUT /changes/{num}/drafts", s.requireAuth(s.handlePutDraft))
	mux.HandleFunc("DELETE /changes/{num}/drafts/{id}", s.requireAuth(s.handleDeleteDraft))
	mux.HandleFunc("GET /changes/{num}/messages", s.handleListMessages)
	mux.HandleFunc("POST /changes/{num}/review", s.requireAuth(s.handleReview))
	mux.HandleFunc("POST /changes/{num}/submit", s.requireAuth(s.handleSubmit))
	mux.HandleFunc("POST /changes/{num}/rebase", s.requireAuth(s.handleRebase))
	mux.HandleFunc("GET /changes/{num}/rebase/conflicts", s.requireAuth(s.handleRebaseConflicts))
	mux.HandleFunc("POST /changes/{num}/rebase/resolve", s.requireAuth(s.handleResolveRebase))
	mux.HandleFunc("GET /changes/{num}/edit", s.requireAuth(s.handleGetEdit))
	mux.HandleFunc("PUT /changes/{num}/edit", s.requireAuth(s.handleCreateEdit))
	mux.HandleFunc("DELETE /changes/{num}/edit", s.requireAuth(s.handleDeleteEdit))
	mux.HandleFunc("PUT /changes/{num}/edit/file", s.requireAuth(s.handlePutEditFile))
	mux.HandleFunc("DELETE /changes/{num}/edit/file", s.requireAuth(s.handleDeleteEditFile))
	mux.HandleFunc("POST /changes/{num}/edit:publish", s.requireAuth(s.handlePublishEdit))
	mux.HandleFunc("POST /changes/{num}/cherry_pick", s.requireAuth(s.handleCherryPick))
	mux.HandleFunc("POST /changes/{num}/revert", s.requireAuth(s.handleRevert))
	mux.HandleFunc("POST /changes/{num}/abandon", s.requireAuth(s.handleAbandon))
	mux.HandleFunc("POST /changes/{num}/restore", s.requireAuth(s.handleRestore))
	mux.HandleFunc("POST /changes/{num}/reviewers", s.requireAuth(s.handleAddReviewer))
	mux.HandleFunc("DELETE /changes/{num}/reviewers/{id}", s.requireAuth(s.handleDeleteReviewer))
	mux.HandleFunc("PUT /changes/{num}/topic", s.requireAuth(s.handleSetTopic))
	mux.HandleFunc("DELETE /changes/{num}/topic", s.requireAuth(s.handleDeleteTopic))
	mux.HandleFunc("PUT /changes/{num}/wip", s.requireAuth(s.handleSetWIP))
	mux.HandleFunc("DELETE /changes/{num}/wip", s.requireAuth(s.handleClearWIP))
	mux.HandleFunc("PUT /changes/{num}/star", s.requireAuth(s.handleStar))
	mux.HandleFunc("DELETE /changes/{num}/star", s.requireAuth(s.handleUnstar))
	mux.HandleFunc("GET /changes/{num}/hashtags", s.handleListHashtags)
	mux.HandleFunc("PUT /changes/{num}/hashtags", s.requireAuth(s.handleSetHashtags))
	mux.HandleFunc("GET /changes/{num}/assignee", s.handleGetAssignee)
	mux.HandleFunc("PUT /changes/{num}/assignee", s.requireAuth(s.handleSetAssignee))
	mux.HandleFunc("DELETE /changes/{num}/assignee", s.requireAuth(s.handleDeleteAssignee))
	mux.HandleFunc("GET /changes/{num}/attention", s.handleListAttention)
	mux.HandleFunc("PUT /changes/{num}/attention", s.requireAuth(s.handleAddAttention))
	mux.HandleFunc("DELETE /changes/{num}/attention/{id}", s.requireAuth(s.handleRemoveAttention))

	// Checks API.
	mux.HandleFunc("GET /changes/{num}/revisions/{ps}/checkruns", s.handleListCheckRuns)
	mux.HandleFunc("POST /changes/{num}/revisions/{ps}/checkruns", s.requireAuth(s.handleUpsertCheckRun))
	mux.HandleFunc("DELETE /changes/{num}/revisions/{ps}/checkruns/{name}", s.requireAuth(s.handleDeleteCheckRun))

	// Webhooks: project-scoped and global (admin).
	mux.HandleFunc("GET /projects/{name}/webhooks", s.requireAuth(s.handleListProjectWebhooks))
	mux.HandleFunc("POST /projects/{name}/webhooks", s.requireAuth(s.handleCreateProjectWebhook))
	mux.HandleFunc("DELETE /projects/{name}/webhooks/{id}", s.requireAuth(s.handleDeleteProjectWebhook))
	mux.HandleFunc("GET /config/webhooks", s.requireAuth(s.handleListGlobalWebhooks))
	mux.HandleFunc("POST /config/webhooks", s.requireAuth(s.handleCreateGlobalWebhook))
	mux.HandleFunc("DELETE /config/webhooks/{id}", s.requireAuth(s.handleDeleteGlobalWebhook))

	// Audit log (admin).
	mux.HandleFunc("GET /admin/audit", s.requireAuth(s.handleListAudit))

	// Prometheus metrics.
	mux.HandleFunc("GET /metrics", s.metrics.Handler(s.db))

	// Gerrit-compatible authenticated alias prefix: /a/...
	mux.Handle("/a/", http.StripPrefix("/a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct, err := s.auth.CurrentAccount(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, i18n.T(i18n.LangFrom(r.Context()), "err.authRequired"))
			return
		}
		s.mux.ServeHTTP(w, r.WithContext(withAccount(r.Context(), acct)))
	})))

	// Static frontend + SPA fallback.
	mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.metrics.IncHTTP()
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
			writeErr(w, http.StatusUnauthorized, i18n.T(i18n.LangFrom(r.Context()), "err.authRequired"))
			return
		}
		r = r.WithContext(withAccount(r.Context(), acct))
		next(w, r)
	}
}

func (s *Server) account(r *http.Request) *store.Account {
	return accountFrom(r.Context())
}

// optionalAccount resolves the caller when present but does not require
// authentication; it returns nil for anonymous requests. Used by read
// endpoints to apply access control and private-change visibility.
func (s *Server) optionalAccount(r *http.Request) *store.Account {
	if acct := accountFrom(r.Context()); acct != nil {
		return acct
	}
	if acct, err := s.auth.CurrentAccount(r); err == nil {
		return acct
	}
	return nil
}

// forbid writes a localized 403 and reports false so callers can `return` early.
func (s *Server) forbid(w http.ResponseWriter, r *http.Request, perm string) bool {
	writeErr(w, http.StatusForbidden, i18n.T(i18n.LangFrom(r.Context()), "err.forbidden", perm))
	return false
}

// ensureChangeRead verifies the caller may view the change; on failure it
// writes a 404 (to avoid leaking existence) and returns false.
func (s *Server) ensureChangeRead(w http.ResponseWriter, r *http.Request, c *store.Change) bool {
	if !s.canReadChange(s.optionalAccount(r), c) {
		writeErr(w, http.StatusNotFound, i18n.T(i18n.LangFrom(r.Context()), "err.changeNotFound"))
		return false
	}
	return true
}

// canReadProject reports whether the caller may browse a project at all
// (read permission on refs/*).
func (s *Server) canReadProject(acct *store.Account, project string) bool {
	return s.can(acct, project, "refs/*", PermRead)
}

func (s *Server) ensureProjectRead(w http.ResponseWriter, r *http.Request, project string) bool {
	if !s.canReadProject(s.optionalAccount(r), project) {
		writeErr(w, http.StatusNotFound, i18n.T(i18n.LangFrom(r.Context()), "err.projectNotFound"))
		return false
	}
	return true
}

func decodeJSON(r *http.Request, v any) error {
	body := io.LimitReader(r.Body, 1<<20)
	dec := json.NewDecoder(body)
	return dec.Decode(v)
}

// ---------- auth handlers ----------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFrom(r.Context())
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, i18n.T(lang, "err.invalidBody"))
		return
	}
	key := loginKey(r, req.Username)
	if locked, wait := s.limiter.blocked(key); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
		writeErr(w, http.StatusTooManyRequests, i18n.T(lang, "err.tooManyAttempts"))
		return
	}
	acct, err := s.auth.Authenticate(req.Username, req.Password)
	if err != nil {
		s.hitLimiter(w, key)
		writeErr(w, http.StatusUnauthorized, i18n.T(lang, "err.invalidCredentials"))
		return
	}
	if secret, enabled, _ := s.db.GetTOTP(acct.ID); enabled {
		if req.TOTP == "" {
			// Password is correct; prompt the UI for the second factor. This is
			// the normal first step, not a failed attempt, so it is not counted.
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":         i18n.T(lang, "err.totpRequired"),
				"totp_required": true,
			})
			return
		}
		if !auth.ValidateTOTP(secret, req.TOTP) {
			s.hitLimiter(w, key)
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":         i18n.T(lang, "err.totpInvalid"),
				"totp_required": true,
			})
			return
		}
	}
	s.limiter.success(key)
	id, err := s.auth.CreateSession(acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	s.auth.SetSessionCookie(w, id)
	writeJSON(w, http.StatusOK, accountInfo(acct))
}

// hitLimiter records a failed login for key and, when the failure trips the
// lockout threshold, advertises the wait via a Retry-After header.
func (s *Server) hitLimiter(w http.ResponseWriter, key string) {
	if d := s.limiter.fail(key); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(d/time.Second)))
	}
}

// loginKey builds the throttle bucket key from the client IP and username.
func loginKey(r *http.Request, username string) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host + "|" + strings.ToLower(username)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFrom(r.Context())
	if !s.allowRegister {
		writeErr(w, http.StatusForbidden, i18n.T(lang, "err.registerDisabled"))
		return
	}
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
			// Gate receive-pack on the right to create changes (push to
			// refs/for/*). Per-ref direct-branch push ACL is enforced by the
			// project's push rules during post-receive processing.
			if !s.can(acct, project, "refs/for/master", PermPush) {
				http.Error(w, "push not permitted", http.StatusForbidden)
				return
			}
			pusher = acct
			remoteUser = acct.Username
		} else if !s.canReadProject(s.optionalAccount(r), project) {
			http.Error(w, "repository not found", http.StatusNotFound)
			return
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
	acct := s.optionalAccount(r)
	out := make(map[string]any, len(list))
	for _, p := range list {
		if !s.canReadProject(acct, p.Name) {
			continue
		}
		out[p.Name] = map[string]any{
			"name":        p.Name,
			"description": p.Description,
			"state":       p.State,
			"parent":      p.Parent,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	if !s.canCapability(acct, PermCreateProject) {
		s.forbid(w, r, PermCreateProject)
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parent      string `json:"parent"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Name = strings.TrimSuffix(req.Name, ".git")
	if err := s.validateParent(req.Name, req.Parent); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.git.CreateProject(req.Name, req.Description); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, gitsvc.ErrProjectExists) {
			code = http.StatusConflict
		}
		writeErr(w, code, err.Error())
		return
	}
	if req.Parent != "" {
		if err := s.db.SetProjectParent(req.Name, req.Parent); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.seedProjectAccess(req.Name, acct)
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "description": req.Description, "parent": req.Parent})
}

// validateParent checks that parent is a valid parent for the project named
// name: empty (global-only) is always allowed; otherwise it must name an
// existing project, must not be the project itself or the '*' sentinel, and
// must not already have name among its own ancestors (which would create a
// cycle).
func (s *Server) validateParent(name, parent string) error {
	if parent == "" {
		return nil
	}
	if parent == name {
		return errors.New("a project cannot be its own parent")
	}
	if parent == "*" {
		return errors.New("'*' is reserved; use an empty parent for global defaults")
	}
	if _, err := s.db.GetProject(parent); err != nil {
		return errors.New("parent project not found")
	}
	chain, err := s.db.ProjectParentChain(parent)
	if err != nil {
		return err
	}
	for _, ancestor := range chain {
		if ancestor == name {
			return errors.New("parent would create a cycle")
		}
	}
	return nil
}

// seedProjectAccess creates the per-project "<name> Owners" group, adds the
// creator, and installs the default access rules (owners may push branches,
// submit, abandon and vote the full Code-Review/Verified range).
func (s *Server) seedProjectAccess(project string, creator *store.Account) {
	owners := &store.Group{Name: project + " Owners", Description: "Owners of " + project}
	if err := s.db.CreateGroup(owners); err != nil {
		// Group name collision: reuse the existing one.
		if g, gerr := s.db.GetGroupByName(owners.Name); gerr == nil {
			owners = g
		} else {
			return
		}
	}
	if creator != nil {
		s.db.AddGroupMember(owners.ID, creator.ID)
	}
	rules := []*store.AccessRule{
		{RefPattern: "refs/heads/*", Permission: PermPush, GroupID: owners.ID, Action: "ALLOW"},
		{RefPattern: "refs/tags/*", Permission: PermPush, GroupID: owners.ID, Action: "ALLOW"},
		{RefPattern: "refs/heads/*", Permission: PermSubmit, GroupID: owners.ID, Action: "ALLOW"},
		{RefPattern: "refs/heads/*", Permission: PermAbandon, GroupID: owners.ID, Action: "ALLOW"},
		{RefPattern: "refs/*", Permission: "label-Code-Review", GroupID: owners.ID, Action: "ALLOW", Min: -2, Max: 2},
		{RefPattern: "refs/*", Permission: "label-Verified", GroupID: owners.ID, Action: "ALLOW", Min: -1, Max: 1},
		{RefPattern: "refs/*", Permission: PermEditTopic, GroupID: owners.ID, Action: "ALLOW"},
		{RefPattern: "refs/*", Permission: PermEditAccess, GroupID: owners.ID, Action: "ALLOW"},
	}
	s.db.SetAccessRules(project, rules)
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
	if !s.ensureProjectRead(w, r, name) {
		return
	}
	reqs, _ := s.db.ListSubmitRequirements(name)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":                p.Name,
		"description":         p.Description,
		"state":               p.State,
		"parent":              p.Parent,
		"submit_type":         p.SubmitType,
		"submit_whole_topic":  p.SubmitWholeTopic,
		"submit_requirements": reqs,
	})
}

func (s *Server) handleListBranches(w http.ResponseWriter, r *http.Request) {
	if !s.ensureProjectRead(w, r, r.PathValue("name")) {
		return
	}
	branches, err := s.git.ListBranches(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleListCommits(w http.ResponseWriter, r *http.Request) {
	if !s.ensureProjectRead(w, r, r.PathValue("name")) {
		return
	}
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
	if !s.ensureProjectRead(w, r, r.PathValue("name")) {
		return
	}
	entries, err := s.git.ListTree(r.PathValue("name"), r.URL.Query().Get("revision"), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !s.ensureProjectRead(w, r, name) {
		return
	}
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
	info := map[string]any{
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
		"created":          c.Created.Format(time.RFC3339),
		"updated":          c.Updated.Format(time.RFC3339),
		"work_in_progress": c.WorkInProgress,
		"private":          c.Private,
	}
	if c.Topic != "" {
		info["topic"] = c.Topic
	}
	return info
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
	acct := s.optionalAccount(r)
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

	root := store.ParseQuery(r.URL.Query().Get("q"), acct)
	changes, total, err := s.db.SearchChangesParsed(root, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	out := make([]map[string]any, 0, len(changes))
	for _, c := range changes {
		if !s.canReadChange(acct, c) {
			continue
		}
		info := changeInfo(c)
		if ps, err := s.db.GetPatchSet(c.Number, c.CurrentPS); err == nil {
			info["current_revision"] = ps.CommitSHA
		}
		info["current_ps"] = c.CurrentPS
		info["labels"] = s.labelsFor(c.Number)
		info["reviewers"] = s.reviewersFor(c.Number)
		if acct != nil {
			info["starred"] = s.db.IsStarred(acct.ID, c.Number)
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

// reviewersFor returns the reviewer list for a change as Gerrit ReviewerInfo maps.
func (s *Server) reviewersFor(changeNum int64) []map[string]any {
	reviewers, err := s.db.ListReviewers(changeNum)
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(reviewers))
	for _, rv := range reviewers {
		out = append(out, map[string]any{
			"_account_id": rv.AccountID,
			"name":        rv.Name,
			"username":    rv.Username,
			"email":       rv.Email,
		})
	}
	return out
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
	if !s.ensureChangeRead(w, r, c) {
		return
	}
	info := changeInfo(c)
	info["current_ps"] = c.CurrentPS
	if acct := s.optionalAccount(r); acct != nil {
		info["starred"] = s.db.IsStarred(acct.ID, c.Number)
	}
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
	info["reviewers"] = s.reviewersFor(c.Number)

	if tags, err := s.db.ListHashtags(c.Number); err == nil && len(tags) > 0 {
		info["hashtags"] = tags
	}
	if a, err := s.db.GetAssignee(c.Number); err == nil && a != nil {
		info["assignee"] = accountBrief(a)
	}
	if att, err := s.db.ListAttention(c.Number); err == nil && len(att) > 0 {
		info["attention_set"] = attentionInfo(att)
	}

	// Submit strategy + requirement evaluation.
	strategy := "REBASE_IF_NECESSARY"
	if p, err := s.db.GetProject(c.Project); err == nil && p.SubmitType != "" {
		strategy = p.SubmitType
	}
	info["submit_type"] = strategy

	submittable := false
	submitBlocked := ""
	if c.Status == "NEW" {
		met, reason := s.submitRequirementsMet(c)
		submittable = met
		if !met {
			submitBlocked = reason
		} else if anc := s.openAncestors(c); len(anc) > 0 {
			submittable = false
			submitBlocked = "depends on open changes: " + changeNums(anc)
		}
	}
	info["submittable"] = submittable
	if submitBlocked != "" {
		info["submit_blocked"] = submitBlocked
	}

	// Relation chain: open ancestors (parents) and the changes this one parents.
	if chain := s.relationChain(c); len(chain) > 0 {
		info["relation_chain"] = chain
	}

	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	c, err := s.db.GetChange(num)
	if err != nil || !s.ensureChangeRead(w, r, c) {
		if err != nil {
			writeErr(w, http.StatusNotFound, "change not found")
		}
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
	if !s.ensureChangeRead(w, r, c) {
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
	if !s.ensureChangeRead(w, r, c) {
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
	if !s.ensureChangeRead(w, r, c) {
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
	c, err := s.db.GetChange(num)
	if err != nil {
		writeErr(w, http.StatusNotFound, "change not found")
		return
	}
	if !s.ensureChangeRead(w, r, c) {
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
			"id":          c.ID,
			"patch_set":   c.PatchSet,
			"path":        c.File,
			"line":        c.Line,
			"message":     c.Message,
			"in_reply_to": c.InReplyTo,
			"resolved":    c.Resolved,
			"updated":     c.Created.Format(time.RFC3339),
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
	lang := i18n.LangFrom(r.Context())
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

	ref := branchRef(c.Branch)
	// Posting a cover message or inline comments requires the comment right.
	if strings.TrimSpace(req.Message) != "" || len(req.Comments) > 0 {
		if !s.can(acct, c.Project, ref, PermComment) {
			s.forbid(w, r, PermComment)
			return
		}
	}

	// Anyone who reviews becomes a reviewer (Gerrit behaviour).
	s.db.AddReviewer(c.Number, acct.ID)

	voteTokens := []string{}
	for label, value := range req.Labels {
		acc := s.checkAccess(acct, c.Project, ref, "label-"+label)
		if !acc.allowed {
			s.forbid(w, r, "label-"+label)
			return
		}
		if value < acc.min || value > acc.max {
			writeErr(w, http.StatusForbidden,
				fmt.Sprintf("%s value %d out of permitted range [%d, %d]", label, value, acc.min, acc.max))
			return
		}
		v := &store.Vote{ChangeNumber: c.Number, PatchSet: c.CurrentPS, AccountID: acct.ID, Label: label, Value: value}
		if value == 0 {
			if err := s.db.DeleteVote(v.ChangeNumber, v.PatchSet, v.AccountID, v.Label); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			voteTokens = append(voteTokens, fmt.Sprintf("%s 0", label))
			continue
		}
		if err := s.db.SetVote(v); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		voteTokens = append(voteTokens, fmt.Sprintf("%s%+d", label, value))
	}
	if len(voteTokens) > 0 {
		sort.Strings(voteTokens)
		s.db.AddChangeMessage(&store.ChangeMessage{
			ChangeNum: c.Number, PatchSet: c.CurrentPS, Type: "vote",
			AuthorID: acct.ID, Message: strings.Join(voteTokens, ", "),
		})
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

	// Publishing a review promotes the author's saved drafts to real comments.
	published, _ := s.db.PublishDrafts(c.Number, acct.ID)

	if strings.TrimSpace(req.Message) != "" {
		s.db.AddChangeMessage(&store.ChangeMessage{
			ChangeNum: c.Number, PatchSet: c.CurrentPS, Type: "comment",
			AuthorID: acct.ID, Message: strings.TrimSpace(req.Message),
		})
	}

	s.db.TouchChange(c.Number)

	votes := strings.Join(voteTokens, ", ")
	evType, summary := "review", i18n.T(lang, "msg.sumLeftReview")
	if strings.TrimSpace(req.Message) != "" {
		evType, summary = "comment", strings.TrimSpace(req.Message)
	} else if len(voteTokens) > 0 {
		summary = i18n.T(lang, "msg.sumVoted", votes)
	}
	if published > 0 || len(req.Comments) > 0 {
		evType = "comment"
		if strings.TrimSpace(req.Message) == "" {
			summary = i18n.T(lang, "msg.sumInlineComments")
			if len(voteTokens) > 0 {
				summary = i18n.T(lang, "msg.sumVotedAndComments", votes)
			}
		}
	}
	s.notifyChange(c, acct.ID, notify.Event{
		Type: evType, Lang: lang,
		Message:     i18n.T(lang, "msg.reviewNotify", acct.FullName, summary, c.CurrentPS),
		NotifyOwner: true, IncludeReviewers: true,
	})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.Status != "NEW" {
		writeErr(w, http.StatusConflict, "change is not open")
		return
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermSubmit) {
		s.forbid(w, r, PermSubmit)
		return
	}

	mergedCommit, submittedNums, err := s.submitChangeChain(acct, lang, c)
	if err != nil {
		var se *submitError
		if errors.As(err, &se) {
			if se.status == http.StatusForbidden {
				s.forbid(w, r, PermSubmit)
			} else {
				writeErr(w, se.status, se.msg)
			}
			return
		}
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "MERGED", "commit": mergedCommit, "submitted": submittedNums,
	})
}

// submitError carries an HTTP status for failures surfaced by submitChangeChain
// so the HTTP handler can reproduce its original status codes while the SSH
// command path simply reports the message.
type submitError struct {
	status int
	msg    string
}

func (e *submitError) Error() string { return e.msg }

// submitChangeChain submits c together with its open relation-chain ancestors
// (and topic siblings when the project submits whole topics), parents first.
// Every member is validated up front so a failure never leaves a partially
// submitted chain. It returns the merged commit of c and all submitted numbers.
func (s *Server) submitChangeChain(acct *store.Account, lang string, c *store.Change) (string, []int64, error) {
	strategy, wholeTopic := "REBASE_IF_NECESSARY", false
	if p, err := s.db.GetProject(c.Project); err == nil {
		if p.SubmitType != "" {
			strategy = p.SubmitType
		}
		wholeTopic = p.SubmitWholeTopic
	}

	seen := map[int64]bool{}
	var batch []*store.Change
	var add func(cs ...*store.Change)
	add = func(cs ...*store.Change) {
		for _, x := range cs {
			if x == nil || seen[x.Number] {
				continue
			}
			seen[x.Number] = true
			batch = append(batch, x)
			add(s.openAncestors(x)...)
		}
	}
	add(c)
	if wholeTopic {
		add(s.topicSiblings(c)...)
	}
	for _, b := range batch {
		if met, reason := s.submitRequirementsMet(b); !met {
			return "", nil, &submitError{http.StatusConflict,
				fmt.Sprintf("change %d is not submittable: %s", b.Number, reason)}
		}
		if !s.can(acct, b.Project, branchRef(b.Branch), PermSubmit) {
			return "", nil, &submitError{http.StatusForbidden, "submit not permitted"}
		}
	}

	pending := batch
	mergedCommit := ""
	var submittedNums []int64
	for len(pending) > 0 {
		progress := false
		for i, b := range pending {
			if anc := s.openAncestors(b); len(anc) > 0 {
				continue
			}
			sha, err := s.git.SubmitWithType(b.Number, strategy)
			if err != nil {
				return "", nil, &submitError{http.StatusConflict,
					fmt.Sprintf("submitting change %d failed: %v", b.Number, err)}
			}
			s.db.AddChangeMessage(&store.ChangeMessage{
				ChangeNum: b.Number, PatchSet: b.CurrentPS, Type: "submitted",
				AuthorID: acct.ID, Message: i18n.T(lang, "msg.merged", strategy, shortSHA(sha)),
			})
			s.notifyChange(b, acct.ID, notify.Event{
				Type:             "submitted",
				Lang:             lang,
				Message:          i18n.T(lang, "msg.submittedNotify", acct.FullName, strategy, shortSHA(sha)),
				NotifyOwner:      true,
				IncludeReviewers: true,
			})
			submittedNums = append(submittedNums, b.Number)
			if b.Number == c.Number {
				mergedCommit = sha
			}
			pending = append(pending[:i], pending[i+1:]...)
			progress = true
			break
		}
		if !progress {
			blocked := make([]*store.Change, len(pending))
			copy(blocked, pending)
			return "", nil, &submitError{http.StatusConflict,
				"cannot determine submit order; changes depend on open ancestors: " + changeNums(blocked)}
		}
	}
	return mergedCommit, submittedNums, nil
}

func (s *Server) handleAbandon(w http.ResponseWriter, r *http.Request) {
	s.setChangeStatus(w, r, "ABANDONED", "NEW")
}

// ---------- rebase / cherry-pick / revert ----------

// canUploadPatchSet reports whether acct may create a new patch set on c: the
// change owner, an administrator, or anyone with push rights to refs/for/<branch>.
func (s *Server) canUploadPatchSet(acct *store.Account, c *store.Change) bool {
	if acct == nil {
		return false
	}
	if acct.ID == c.OwnerID {
		return true
	}
	return s.can(acct, c.Project, "refs/for/"+c.Branch, PermPush)
}

// recordPatchSet stores nc as a patch set of changeNumber, marks it current and
// logs an activity message.
func (s *Server) recordPatchSet(changeNumber int64, acct *store.Account, nc gitsvc.NewCommit, msg string) error {
	ps := &store.PatchSet{
		ChangeNumber: changeNumber, Number: nc.NewPatchSet, CommitSHA: nc.SHA,
		AuthorName: nc.AuthorName, AuthorEmail: nc.AuthorEmail, Message: nc.Message,
	}
	if err := s.db.CreatePatchSet(ps); err != nil {
		return err
	}
	if err := s.db.SetCurrentPatchSet(changeNumber, nc.NewPatchSet); err != nil {
		return err
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: changeNumber, PatchSet: nc.NewPatchSet, Type: "patchset-uploaded",
		AuthorID: acct.ID, Message: msg,
	})
	return s.db.TouchChange(changeNumber)
}

func (s *Server) handleRebase(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.Status != "NEW" {
		writeErr(w, http.StatusConflict, "change is not open")
		return
	}
	if !s.canUploadPatchSet(acct, c) {
		s.forbid(w, r, PermPush)
		return
	}
	nc, err := s.git.Rebase(c.Number)
	if err != nil {
		if errors.Is(err, gitsvc.ErrRebaseConflict) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": err.Error(), "conflict": true,
			})
			return
		}
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.recordPatchSet(c.Number, acct, nc,
		i18n.T(lang, "msg.psRebased", nc.NewPatchSet, c.Branch)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, changeInfo(c))
}

// handleRebaseConflicts returns the three-way conflict content produced by a
// trial rebase of the change onto its destination branch tip, so the web UI can
// offer an interactive resolver. An empty list means the rebase is clean.
func (s *Server) handleRebaseConflicts(w http.ResponseWriter, r *http.Request) {
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
	if c.Status != "NEW" {
		writeErr(w, http.StatusConflict, "change is not open")
		return
	}
	if !s.canUploadPatchSet(acct, c) {
		s.forbid(w, r, PermPush)
		return
	}
	files, err := s.git.RebaseConflicts(c.Number)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if files == nil {
		files = []gitsvc.ConflictFile{}
	}
	writeJSON(w, http.StatusOK, files)
}

// handleResolveRebase rebases the change applying the supplied per-path resolved
// content and records the result as a new patch set.
func (s *Server) handleResolveRebase(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.Status != "NEW" {
		writeErr(w, http.StatusConflict, "change is not open")
		return
	}
	if !s.canUploadPatchSet(acct, c) {
		s.forbid(w, r, PermPush)
		return
	}
	var req struct {
		Resolutions map[string]string `json:"resolutions"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Resolutions) == 0 {
		writeErr(w, http.StatusBadRequest, "resolutions are required")
		return
	}
	nc, err := s.git.ResolveRebase(c.Number, req.Resolutions)
	if err != nil {
		if errors.Is(err, gitsvc.ErrRebaseConflict) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": err.Error(), "conflict": true,
			})
			return
		}
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.recordPatchSet(c.Number, acct, nc,
		i18n.T(lang, "msg.psRebased", nc.NewPatchSet, c.Branch)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, changeInfo(c))
}

// startDerivedChange creates a placeholder change row so the git operation has a
// destination number to push into. On any later failure the orphan is abandoned.
func (s *Server) startDerivedChange(acct *store.Account, project, branch, subject string) (*store.Change, error) {
	nc := &store.Change{
		Project: project, Branch: branch, ChangeID: s.git.GenerateChangeID(),
		Subject: subject, OwnerID: acct.ID,
	}
	if err := s.db.CreateChange(nc); err != nil {
		return nil, err
	}
	if err := s.db.AddReviewer(nc.Number, acct.ID); err != nil {
		return nil, err
	}
	return nc, nil
}

func (s *Server) abandonOrphan(number int64) {
	s.db.UpdateChangeStatus(number, "ABANDONED", nil)
}

func (s *Server) handleCherryPick(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	var req struct {
		Destination string `json:"destination"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Destination) == "" {
		writeErr(w, http.StatusBadRequest, "destination branch is required")
		return
	}
	target := strings.TrimSpace(req.Destination)
	if !s.can(acct, c.Project, "refs/for/"+target, PermPush) {
		s.forbid(w, r, PermPush)
		return
	}

	newChange, err := s.startDerivedChange(acct, c.Project, target, c.Subject)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	nc, err := s.git.CherryPick(c.Number, target, newChange.ChangeID, newChange.Number)
	if err != nil {
		s.abandonOrphan(newChange.Number)
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.recordPatchSet(newChange.Number, acct, nc,
		i18n.T(lang, "msg.psCherryPicked", nc.NewPatchSet, c.Number)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: newChange.Number, Type: "comment", AuthorID: acct.ID,
		Message: i18n.T(lang, "msg.cherryPickedFrom", c.Number),
	})
	if created, err := s.db.GetChange(newChange.Number); err == nil {
		writeJSON(w, http.StatusOK, changeInfo(created))
		return
	}
	writeJSON(w, http.StatusOK, changeInfo(newChange))
}

func (s *Server) handleRevert(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.Status != "MERGED" {
		writeErr(w, http.StatusConflict, "only merged changes can be reverted")
		return
	}
	if !s.can(acct, c.Project, "refs/for/"+c.Branch, PermPush) {
		s.forbid(w, r, PermPush)
		return
	}

	newChange, err := s.startDerivedChange(acct, c.Project, c.Branch, "Revert \""+c.Subject+"\"")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	nc, err := s.git.Revert(c.Number, newChange.ChangeID, newChange.Number)
	if err != nil {
		s.abandonOrphan(newChange.Number)
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.recordPatchSet(newChange.Number, acct, nc,
		i18n.T(lang, "msg.psRevert", nc.NewPatchSet, c.Number)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: newChange.Number, Type: "comment", AuthorID: acct.ID,
		Message: i18n.T(lang, "msg.reverts", c.Number),
	})
	if created, err := s.db.GetChange(newChange.Number); err == nil {
		writeJSON(w, http.StatusOK, changeInfo(created))
		return
	}
	writeJSON(w, http.StatusOK, changeInfo(newChange))
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	s.setChangeStatus(w, r, "NEW", "ABANDONED")
}

func (s *Server) setChangeStatus(w http.ResponseWriter, r *http.Request, to, requireFrom string) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.Status != requireFrom {
		writeErr(w, http.StatusConflict, "unexpected change status: "+c.Status)
		return
	}
	if !s.can(acct, c.Project, branchRef(c.Branch), PermAbandon) {
		s.forbid(w, r, PermAbandon)
		return
	}
	if err := s.db.UpdateChangeStatus(c.Number, to, nil); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	msgType, msgKey, notifyKey := "restored", "msg.restored", "msg.restoredNotify"
	if to == "ABANDONED" {
		msgType, msgKey, notifyKey = "abandoned", "msg.abandoned", "msg.abandonedNotify"
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, PatchSet: c.CurrentPS, Type: msgType,
		AuthorID: acct.ID, Message: i18n.T(lang, msgKey),
	})
	s.notifyChange(c, acct.ID, notify.Event{
		Type:             msgType,
		Lang:             lang,
		Message:          i18n.T(lang, notifyKey, acct.FullName),
		NotifyOwner:      true,
		IncludeReviewers: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": to})
}

// ---------- change messages, reviewers, topic, WIP ----------

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
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
	msgs, err := s.db.ListChangeMessages(num)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		entry := map[string]any{
			"id":        m.ID,
			"type":      m.Type,
			"patch_set": m.PatchSet,
			"message":   m.Message,
			"date":      m.Created.Format(time.RFC3339),
		}
		if m.AuthorID > 0 {
			entry["author"] = map[string]any{
				"_account_id": m.AuthorID,
				"name":        orDefault(m.AuthorName, m.AuthorUser),
				"username":    m.AuthorUser,
			}
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveAccount finds an account by numeric id or by username.
func (s *Server) resolveAccount(ident string) (*store.Account, error) {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return nil, errors.New("empty account identifier")
	}
	if id, err := strconv.ParseInt(ident, 10, 64); err == nil {
		return s.db.GetAccount(id)
	}
	return s.db.GetAccountByUsername(ident)
}

func (s *Server) handleAddReviewer(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.OwnerID != acct.ID && !s.can(acct, c.Project, branchRef(c.Branch), PermComment) {
		s.forbid(w, r, PermAddReviewer)
		return
	}
	var req struct {
		Reviewer string `json:"reviewer"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target, err := s.resolveAccount(req.Reviewer)
	if err != nil {
		writeErr(w, http.StatusNotFound, "reviewer not found")
		return
	}
	if err := s.db.AddReviewer(c.Number, target.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "reviewer-added", AuthorID: acct.ID,
		Message: i18n.T(lang, "msg.reviewerAdded", orDefault(target.FullName, target.Username)),
	})
	s.db.TouchChange(c.Number)
	s.notifyChange(c, acct.ID, notify.Event{
		Type:            "reviewer-added",
		Lang:            lang,
		Message:         i18n.T(lang, "msg.reviewerAddedNotify", acct.FullName),
		ExtraRecipients: []int64{target.ID},
	})
	writeJSON(w, http.StatusOK, s.reviewersFor(c.Number))
}

func (s *Server) handleDeleteReviewer(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.OwnerID != acct.ID && !s.can(acct, c.Project, branchRef(c.Branch), PermComment) {
		s.forbid(w, r, PermAddReviewer)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid reviewer id")
		return
	}
	if id == c.OwnerID {
		writeErr(w, http.StatusBadRequest, "cannot remove the change owner from reviewers")
		return
	}
	if err := s.db.RemoveReviewer(c.Number, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := strconv.FormatInt(id, 10)
	if a, err := s.db.GetAccount(id); err == nil {
		name = orDefault(a.FullName, a.Username)
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "reviewer-removed", AuthorID: acct.ID,
		Message: i18n.T(lang, "msg.reviewerRemoved", name),
	})
	s.db.TouchChange(c.Number)
	writeJSON(w, http.StatusOK, s.reviewersFor(c.Number))
}

func (s *Server) handleSetTopic(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if !s.can(acct, c.Project, branchRef(c.Branch), PermEditTopic) {
		s.forbid(w, r, PermEditTopic)
		return
	}
	var req struct {
		Topic string `json:"topic"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	topic := strings.TrimSpace(req.Topic)
	if err := s.db.SetTopic(c.Number, topic); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := i18n.T(lang, "msg.topicCleared")
	if topic != "" {
		msg = i18n.T(lang, "msg.topicSet", topic)
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "topic", AuthorID: acct.ID, Message: msg,
	})
	writeJSON(w, http.StatusOK, map[string]any{"topic": topic})
}

func (s *Server) handleDeleteTopic(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if !s.can(acct, c.Project, branchRef(c.Branch), PermEditTopic) {
		s.forbid(w, r, PermEditTopic)
		return
	}
	if err := s.db.SetTopic(c.Number, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "topic", AuthorID: acct.ID, Message: i18n.T(lang, "msg.topicCleared"),
	})
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) setWIP(w http.ResponseWriter, r *http.Request, wip bool) {
	acct := s.account(r)
	lang := i18n.LangFrom(r.Context())
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
	if c.OwnerID != acct.ID && !s.can(acct, c.Project, branchRef(c.Branch), PermEditTopic) {
		s.forbid(w, r, "wip")
		return
	}
	if err := s.db.SetWorkInProgress(c.Number, wip); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := i18n.T(lang, "msg.readyForReview")
	if wip {
		msg = i18n.T(lang, "msg.wip")
	}
	s.db.AddChangeMessage(&store.ChangeMessage{
		ChangeNum: c.Number, Type: "wip", AuthorID: acct.ID, Message: msg,
	})
	writeJSON(w, http.StatusOK, map[string]any{"work_in_progress": wip})
}

func (s *Server) handleSetWIP(w http.ResponseWriter, r *http.Request)   { s.setWIP(w, r, true) }
func (s *Server) handleClearWIP(w http.ResponseWriter, r *http.Request) { s.setWIP(w, r, false) }

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

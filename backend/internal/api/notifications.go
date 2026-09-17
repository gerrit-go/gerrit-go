package api

import (
	"net/http"
	"strconv"

	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

// notifyChange fills the change-derived fields of ev and dispatches it. A nil
// notifier is a no-op.
func (s *Server) notifyChange(c *store.Change, actorID int64, ev notify.Event) {
	ev.ChangeNumber = c.Number
	ev.Project = c.Project
	ev.Branch = c.Branch
	ev.Subject = c.Subject
	ev.OwnerID = c.OwnerID
	ev.ActorID = actorID
	s.notify.Notify(ev)
	s.emitWebhook(c, ev)
	s.publishNotifyEvent(c, actorID, ev)
}

// emitWebhook fans a change event out to project and global webhooks. The
// event type mirrors the in-app notification type so subscribers can filter on
// a single vocabulary.
func (s *Server) emitWebhook(c *store.Change, ev notify.Event) {
	if s.hook == nil {
		return
	}
	payload := map[string]any{
		"type": ev.Type,
		"actor": map[string]any{
			"_account_id": ev.ActorID,
		},
		"change": map[string]any{
			"number":  c.Number,
			"project": c.Project,
			"branch":  c.Branch,
			"subject": c.Subject,
			"status":  c.Status,
			"owner":   c.OwnerName,
		},
		"message": ev.Message,
	}
	s.hook.Dispatch(ev.Type, c.Project, payload)
}

// ---------- star ----------

func (s *Server) handleStar(w http.ResponseWriter, r *http.Request) {
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
	if err := s.db.StarChange(acct.ID, c.Number); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"starred": true})
}

func (s *Server) handleUnstar(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	num, ok := s.parseChangeNum(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid change number")
		return
	}
	if err := s.db.UnstarChange(acct.ID, num); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"starred": false})
}

// ---------- watch ----------

func (s *Server) handleWatchProject(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	name := r.PathValue("name")
	if _, err := s.db.GetProject(name); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.ensureProjectRead(w, r, name) {
		return
	}
	notify := "ALL"
	var req struct {
		Notify string `json:"notify"`
		Branch string `json:"branch"`
		Author string `json:"author"`
	}
	if err := decodeJSON(r, &req); err == nil {
		if req.Notify != "" {
			notify = req.Notify
		}
	}
	if err := s.db.WatchProject(acct.ID, name, notify, req.Branch, req.Author); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": name, "notify": notify, "branch": req.Branch, "author": req.Author, "watched": true})
}

func (s *Server) handleUnwatchProject(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	name := r.PathValue("name")
	if err := s.db.UnwatchProject(acct.ID, name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": name, "watched": false})
}

func (s *Server) handleListWatched(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	watched, err := s.db.ListWatchedProjects(acct.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if watched == nil {
		watched = []store.WatchedProject{}
	}
	writeJSON(w, http.StatusOK, watched)
}

// ---------- notifications ----------

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 {
		limit = n
	}
	unreadOnly := r.URL.Query().Get("unread") == "1"
	items, err := s.db.ListNotifications(acct.ID, limit, unreadOnly)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []*store.Notification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": items,
		"unread":        s.db.CountUnreadNotifications(acct.ID),
	})
}

func (s *Server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	acct := s.account(r)
	var req struct {
		ID int64 `json:"id"`
	}
	_ = decodeJSON(r, &req)
	var err error
	if req.ID > 0 {
		err = s.db.MarkNotificationRead(acct.ID, req.ID)
	} else {
		err = s.db.MarkAllNotificationsRead(acct.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": s.db.CountUnreadNotifications(acct.ID)})
}

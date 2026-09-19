package api

import (
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/ssh"

	"gerrit-go/internal/events"
	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

// notifyTypeToStream maps internal notification event types to Gerrit's
// stream-events vocabulary. Types not present are not streamed.
var notifyTypeToStream = map[string]string{
	"comment":           "comment-added",
	"review":            "comment-added",
	"submitted":         "change-merged",
	"abandoned":         "change-abandoned",
	"restored":          "change-restored",
	"reviewer-added":    "reviewer-added",
	"patchset-uploaded": "patchset-created",
}

// publishStreamEvent publishes a change event to stream-events subscribers.
// The change is re-fetched so the published snapshot reflects committed state.
func (s *Server) publishStreamEvent(changeNum int64, evType string, actor *store.Account, extra map[string]any) {
	c, err := s.db.GetChange(changeNum)
	if err != nil {
		return
	}
	ev := events.StreamEvent{Type: evType, Change: c, Actor: actor, Extra: extra}
	if ps, err := s.db.GetPatchSet(changeNum, c.CurrentPS); err == nil {
		ev.PatchSet = ps
	}
	s.events.Publish(ev)
}

// publishNotifyEvent maps a notify.Event to its stream-events type and
// publishes it. It is called from notifyChange so every existing notification
// call site streams for free.
func (s *Server) publishNotifyEvent(c *store.Change, actorID int64, ev notify.Event) {
	streamType, ok := notifyTypeToStream[ev.Type]
	if !ok {
		return
	}
	actor, _ := s.db.GetAccount(actorID)
	extra := map[string]any{}
	if ev.Message != "" {
		extra["comment"] = ev.Message
	}
	if ev.Type == "reviewer-added" && len(ev.ExtraRecipients) > 0 {
		if rv, err := s.db.GetAccount(ev.ExtraRecipients[0]); err == nil {
			extra["reviewer"] = accountBrief(rv)
		}
	}
	s.publishStreamEvent(c.Number, streamType, actor, extra)
}

// onGitChangeEvent is the gitsvc callback fired when a push to refs/for creates
// a change or a new patch set.
func (s *Server) onGitChangeEvent(changeNum int64, kind string) {
	s.publishStreamEvent(changeNum, kind, nil, nil)
	if kind == "patchset-created" {
		// CI runs off-path: a slow or broken pipeline must never block a push.
		go s.triggerCI(changeNum)
	}
}

// streamEventJSON renders a StreamEvent as a Gerrit-compatible JSON object.
func streamEventJSON(ev events.StreamEvent) map[string]any {
	out := map[string]any{
		"type":           ev.Type,
		"eventCreatedOn": ev.CreatedOn.Unix(),
	}
	if ev.Change != nil {
		ci := changeInfo(ev.Change)
		ci["number"] = ev.Change.Number
		out["change"] = ci
	}
	if ev.PatchSet != nil {
		out["patchSet"] = map[string]any{
			"number":   ev.PatchSet.Number,
			"revision": ev.PatchSet.CommitSHA,
			"ref":      fmt.Sprintf("refs/changes/%02d/%d/%d", ev.Change.Number%100, ev.Change.Number, ev.PatchSet.Number),
		}
	}
	if ev.Actor != nil {
		out["author"] = accountBrief(ev.Actor)
	}
	for k, v := range ev.Extra {
		out[k] = v
	}
	return out
}

// sshGerritStreamEvents implements `gerrit stream-events`: it subscribes the
// account and streams JSON events (one per line) until the client disconnects,
// filtering each event by the subscriber's read permission.
func (s *Server) sshGerritStreamEvents(ch ssh.Channel, acct *store.Account, args []string) int {
	if acct == nil {
		fmt.Fprintln(ch.Stderr(), "authentication required")
		return 1
	}
	sub := s.events.Subscribe(acct, 64)
	defer s.events.Unsubscribe(sub)
	enc := json.NewEncoder(ch)
	for ev := range sub.Ch {
		if ev.Change != nil && !s.canReadChange(sub.Acct, ev.Change) {
			continue
		}
		if err := enc.Encode(streamEventJSON(ev)); err != nil {
			// Client disconnected; the deferred Unsubscribe cleans up.
			return 0
		}
	}
	return 0
}

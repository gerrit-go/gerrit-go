package api

import (
	"path/filepath"
	"testing"

	"gerrit-go/internal/auth"
	"gerrit-go/internal/gitsvc"
	"gerrit-go/internal/notify"
	"gerrit-go/internal/store"
)

// Go's ServeMux panics at registration time when two patterns can match the same
// path without one being more specific, which crash-loops the whole server.
// Building the route table is therefore worth asserting in CI.
func TestRoutePatternsDoNotConflict(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "routes.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	gitSvc := gitsvc.New(filepath.Join(t.TempDir(), "git"), db)
	s := NewServer(db, auth.New(db), gitSvc, notify.New(db, notify.SMTPConfig{}, "http://localhost"), t.TempDir(), false)
	if s.Handler() == nil {
		t.Fatal("nil handler")
	}
}

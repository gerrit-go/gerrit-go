package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustAccount(t *testing.T, db *DB, username, full string) *Account {
	t.Helper()
	a := &Account{Username: username, FullName: full, Email: username + "@example.com"}
	if err := db.CreateAccount(a); err != nil {
		t.Fatalf("create account %s: %v", username, err)
	}
	return a
}

func mustChange(t *testing.T, db *DB, project, branch, subject string, owner int64, status, updated string) *Change {
	t.Helper()
	c := &Change{Project: project, Branch: branch, ChangeID: "I" + subject, Subject: subject, OwnerID: owner, CurrentPS: 1}
	if err := db.CreateChange(c); err != nil {
		t.Fatalf("create change: %v", err)
	}
	if _, err := db.db.Exec(`UPDATE changes SET status=?, updated=? WHERE number=?`, status, updated, c.Number); err != nil {
		t.Fatalf("adjust change: %v", err)
	}
	c.Status = status
	return c
}

func numbers(cs []*Change) map[int64]bool {
	m := map[int64]bool{}
	for _, c := range cs {
		m[c.Number] = true
	}
	return m
}

func TestSearchChangesParsedIntegration(t *testing.T) {
	db := openTemp(t)
	alice := mustAccount(t, db, "alice", "Alice A")
	bob := mustAccount(t, db, "bob", "Bob B")

	for _, name := range []string{"foo", "bar"} {
		if err := db.CreateProject(&Project{Name: name, Head: "main"}); err != nil {
			t.Fatalf("create project %s: %v", name, err)
		}
	}

	open1 := mustChange(t, db, "foo", "main", "alpha", alice.ID, "NEW", "2026-09-10T00:00:00Z")
	open2 := mustChange(t, db, "bar", "main", "beta", bob.ID, "NEW", "2026-09-14T00:00:00Z")
	merged := mustChange(t, db, "foo", "main", "gamma", alice.ID, "MERGED", "2026-09-15T00:00:00Z")

	// bob reviews open1 (owned by alice)
	if err := db.AddReviewer(open1.Number, bob.ID); err != nil {
		t.Fatalf("add reviewer: %v", err)
	}
	// alice stars open2
	if err := db.StarChange(alice.ID, open2.Number); err != nil {
		t.Fatalf("star: %v", err)
	}

	run := func(q string, self *Account) map[int64]bool {
		t.Helper()
		cs, _, err := db.SearchChangesParsed(ParseQuery(q, self), 50, 0)
		if err != nil {
			t.Fatalf("SearchChangesParsed(%q): %v", q, err)
		}
		return numbers(cs)
	}

	assertSet := func(q string, self *Account, want ...int64) {
		t.Helper()
		got := run(q, self)
		if len(got) != len(want) {
			t.Errorf("q=%q got %d results %v, want %d %v", q, len(got), got, len(want), want)
			return
		}
		for _, n := range want {
			if !got[n] {
				t.Errorf("q=%q missing change %d (got %v)", q, n, got)
			}
		}
	}

	assertSet("status:open", alice, open1.Number, open2.Number)
	assertSet("status:closed", alice, merged.Number)
	assertSet("project:foo status:open", alice, open1.Number)
	// OR
	assertSet("project:foo OR project:bar", alice, open1.Number, open2.Number, merged.Number)
	// implicit AND + OR precedence: open OR (merged foo)
	assertSet("status:open OR status:merged project:foo", alice, open1.Number, open2.Number, merged.Number)
	// NOT / '-'
	assertSet("status:open -project:foo", alice, open2.Number)
	assertSet("status:open NOT project:foo", alice, open2.Number)
	// parens
	assertSet("(project:foo OR project:bar) status:merged", alice, merged.Number)
	// reviewer:self for bob -> open1
	assertSet("reviewer:self", bob, open1.Number)
	// incoming reviews canned query for bob: reviewer:self -owner:self status:open
	assertSet("reviewer:self -owner:self status:open", bob, open1.Number)
	// outgoing for alice
	assertSet("owner:self status:open", alice, open1.Number)
	// is:starred for alice -> open2
	assertSet("is:starred", alice, open2.Number)
	// free text
	assertSet("beta", alice, open2.Number)
	// after: filters by updated
	assertSet("status:open after:2026-09-12", alice, open2.Number)
	assertSet("status:open before:2026-09-12", alice, open1.Number)
	// empty query matches all
	assertSet("", alice, open1.Number, open2.Number, merged.Number)
}

package store

import "testing"

func TestParseQueryCompile(t *testing.T) {
	self := &Account{ID: 7, Username: "alice"}
	cases := []struct {
		q    string
		want string
	}{
		{"", "1=1"},
		{"status:open", "(ch.status=?)"},
		{"status:closed", "(ch.status IN (?,?))"},
		{"bug", "((ch.subject LIKE ? OR ch.project LIKE ? OR ch.change_id LIKE ? OR CAST(ch.number AS TEXT) LIKE ?))"},
		{"status:open owner:self", "((ch.status=?) AND (ch.owner_id=?))"},
		{"status:open OR status:merged", "((ch.status=?) OR (ch.status=?))"},
		{"-status:open", "NOT ((ch.status=?))"},
		{"NOT status:open", "NOT ((ch.status=?))"},
		{"(status:open OR status:merged) project:foo", "(((ch.status=?) OR (ch.status=?)) AND (ch.project=?))"},
		{"is:starred", "(EXISTS (SELECT 1 FROM starred st WHERE st.change_number=ch.number AND st.account_id=?))"},
		// AND binds tighter than OR: a OR b c  =>  a OR (b AND c)
		{"status:open OR status:merged project:foo", "((ch.status=?) OR ((ch.status=?) AND (ch.project=?)))"},
	}
	for _, c := range cases {
		got, _ := ParseQuery(c.q, self).compile()
		if got != c.want {
			t.Errorf("ParseQuery(%q).compile()\n got: %s\nwant: %s", c.q, got, c.want)
		}
	}
}

func TestParseQueryArgs(t *testing.T) {
	self := &Account{ID: 7, Username: "alice"}
	_, args := ParseQuery("status:open owner:self", self).compile()
	if len(args) != 2 {
		t.Fatalf("want 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "NEW" {
		t.Errorf("arg0 = %v, want NEW", args[0])
	}
	if args[1] != int64(7) {
		t.Errorf("arg1 = %v, want 7", args[1])
	}
}

func TestParseQueryQuoted(t *testing.T) {
	// A quoted free-text term keeps its spaces as a single operand.
	got, args := ParseQuery(`"fix the bug"`, nil).compile()
	if got != "((ch.subject LIKE ? OR ch.project LIKE ? OR ch.change_id LIKE ? OR CAST(ch.number AS TEXT) LIKE ?))" {
		t.Errorf("unexpected sql: %s", got)
	}
	if len(args) != 4 || args[0] != "%fix the bug%" {
		t.Errorf("unexpected args: %v", args)
	}
}

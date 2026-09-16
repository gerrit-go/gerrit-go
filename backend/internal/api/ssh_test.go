package api

import "testing"

func TestTokenizeSSHCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"git-upload-pack '/my/proj.git'", []string{"git-upload-pack", "/my/proj.git"}},
		{`git-receive-pack "proj.git"`, []string{"git-receive-pack", "proj.git"}},
		{"gerrit review 123,1 --label Code-Review=+2 --message 'looks good'",
			[]string{"gerrit", "review", "123,1", "--label", "Code-Review=+2", "--message", "looks good"}},
		{"gerrit query status:open project:demo", []string{"gerrit", "query", "status:open", "project:demo"}},
		{"version", []string{"version"}},
		{"", nil},
	}
	for _, c := range cases {
		got := tokenizeSSHCommand(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("tokenize(%q) = %#v, want %#v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("tokenize(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestParseLabelArg(t *testing.T) {
	if l, v, ok := parseLabelArg("Code-Review=+2"); !ok || l != "Code-Review" || v != 2 {
		t.Fatalf("got %q %d %v", l, v, ok)
	}
	if l, v, ok := parseLabelArg("Verified=-1"); !ok || l != "Verified" || v != -1 {
		t.Fatalf("got %q %d %v", l, v, ok)
	}
	if _, _, ok := parseLabelArg("nonsense"); ok {
		t.Fatal("expected failure for missing '='")
	}
}

func TestSSHProjectFromArg(t *testing.T) {
	cases := map[string]string{
		"'/my/proj.git'": "my/proj",
		"/my/proj.git":   "my/proj",
		"proj.git":       "proj",
		"proj":           "proj",
		"":               "",
		"'../evil.git'":  "",
	}
	for in, want := range cases {
		if got := sshProjectFromArg(in); got != want {
			t.Errorf("sshProjectFromArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSHParseChangeNum(t *testing.T) {
	if n, ok := sshParseChangeNum("123,2"); !ok || n != 123 {
		t.Fatalf("got %d %v", n, ok)
	}
	if n, ok := sshParseChangeNum("456"); !ok || n != 456 {
		t.Fatalf("got %d %v", n, ok)
	}
	if _, ok := sshParseChangeNum("abc"); ok {
		t.Fatal("expected failure")
	}
}

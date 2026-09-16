package i18n

import "testing"

func TestCatalogParity(t *testing.T) {
	for key := range enMessages {
		if _, ok := zhMessages[key]; !ok {
			t.Errorf("zh catalog missing key %q", key)
		}
	}
	for key := range zhMessages {
		if _, ok := enMessages[key]; !ok {
			t.Errorf("en catalog missing key %q", key)
		}
	}
}

func TestT(t *testing.T) {
	if got := T(En, "msg.topicCleared"); got != "Topic cleared." {
		t.Errorf("en topicCleared = %q", got)
	}
	if got := T(Zh, "msg.topicCleared"); got != "话题已清除。" {
		t.Errorf("zh topicCleared = %q", got)
	}
	// Positional reordering in zh.
	got := T(Zh, "msg.reviewNotify", "张三", "留下了评审", 3)
	if got != "张三 在补丁集 3 上留下了评审。" {
		t.Errorf("zh reviewNotify = %q", got)
	}
	got = T(En, "msg.merged", "REBASE_IF_NECESSARY", "abc1234")
	if got != "Change merged via REBASE_IF_NECESSARY, commit abc1234." {
		t.Errorf("en merged = %q", got)
	}
	// Unknown key renders as itself; unknown language falls back to English.
	if got := T(En, "nope.missing"); got != "nope.missing" {
		t.Errorf("missing key = %q", got)
	}
	if got := T("fr", "msg.topicCleared"); got != "Topic cleared." {
		t.Errorf("fallback lang = %q", got)
	}
}

func TestParseAcceptLanguage(t *testing.T) {
	cases := map[string]string{
		"":                  En,
		"en-US,en;q=0.9":    En,
		"zh-CN,zh;q=0.9":    Zh,
		"zh-Hans":           Zh,
		"en;q=0.5,zh;q=0.9": Zh,
		"fr;q=1.0,zh;q=0.8": Zh,
		"de,fr;q=0.9":       En,
		"zh;q=0.2,en;q=0.8": En,
	}
	for header, want := range cases {
		if got := ParseAcceptLanguage(header); got != want {
			t.Errorf("ParseAcceptLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

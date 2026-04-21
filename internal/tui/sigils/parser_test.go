package sigils

import (
	"reflect"
	"testing"
)

func TestParse_FindsMatches(t *testing.T) {
	got := Parse("hello [[todo:42]] and [[session:my-branch]] done")
	want := []Match{
		{Prefix: "todo", ID: "42", Raw: "[[todo:42]]", Start: 6, End: 17},
		{Prefix: "session", ID: "my-branch", Raw: "[[session:my-branch]]", Start: 22, End: 43},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParse_IgnoresMalformed(t *testing.T) {
	for _, s := range []string{
		"[[no-colon]]",
		"[[:empty-prefix]]",
		"[[bad prefix:id]]",
		"[no-double-brackets:id]",
		"[[prefix:id",
		"[[Prefix:id]]",
		"[[1badfirst:id]]",
	} {
		got := Parse(s)
		if len(got) != 0 {
			t.Errorf("Parse(%q) = %+v, want none", s, got)
		}
	}
}

func TestParse_AllowsHyphenAndDigitsInPrefix(t *testing.T) {
	got := Parse("[[issue-tracker:n-42]]")
	if len(got) != 1 || got[0].Prefix != "issue-tracker" || got[0].ID != "n-42" {
		t.Fatalf("got %+v", got)
	}
}

func TestValidPrefix(t *testing.T) {
	cases := map[string]bool{
		"todo":       true,
		"a":          true,
		"issue-42":   true,
		"":           false,
		"-bad":       false,
		"bad_prefix": false,
		"Bad":        false,
		"1bad":       false,
	}
	for prefix, want := range cases {
		if got := ValidPrefix(prefix); got != want {
			t.Errorf("ValidPrefix(%q) = %v want %v", prefix, got, want)
		}
	}
}

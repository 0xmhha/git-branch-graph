package extract

import "testing"

func TestParsePR(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"fix: prevent stale work overwrite in syncCheck (#186)", "186"},
		{"fix: merge dev to master (#172)", "172"},
		{"chore: bump version to v0.10.14", ""},
		{"revert (#10) then reland (#42)", "42"}, // last token wins
		{"no parens #99", ""},                    // must be wrapped in (#..)
	}
	for _, c := range cases {
		if got := parsePR(c.subject); got != c.want {
			t.Errorf("parsePR(%q) = %q, want %q", c.subject, got, c.want)
		}
	}
}

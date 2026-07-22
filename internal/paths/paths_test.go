package paths

import "testing"

func TestParseRepoRef(t *testing.T) {
	cases := []struct {
		in        string
		org, repo string
	}{
		{"https://github.com/wemix/go-wemix", "wemix", "go-wemix"},
		{"https://github.com/wemix/go-wemix.git", "wemix", "go-wemix"},
		{"git@github.com:wemix/go-wemix.git", "wemix", "go-wemix"},
		{"/Users/me/Work/github/wemix/go-wemix", "wemix", "go-wemix"},
	}
	for _, c := range cases {
		r := ParseRepoRef(c.in)
		if r.Org != c.org || r.Repo != c.repo {
			t.Errorf("ParseRepoRef(%q) = %s/%s, want %s/%s", c.in, r.Org, r.Repo, c.org, c.repo)
		}
		if r.Slug != c.org+"__"+c.repo {
			t.Errorf("slug = %q, want %q", r.Slug, c.org+"__"+c.repo)
		}
	}
}

func TestRunDirSanitizesBranch(t *testing.T) {
	ref := ParseRepoRef("https://github.com/wemix/go-wemix")
	got := RunDir("data", ref, "release/w0.10.14", "11eb943dfcbb")
	want := "data/wemix__go-wemix__release-w0.10.14__11eb943"
	if got != want {
		t.Errorf("RunDir = %q, want %q", got, want)
	}
}

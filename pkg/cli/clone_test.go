package cli

import "testing"

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:user/myrepo.git":        "myrepo",
		"git@github.com:user/myrepo":            "myrepo",
		"https://github.com/user/myrepo.git":    "myrepo",
		"https://github.com/user/myrepo":        "myrepo",
		"ssh://git@server.com/path/to/repo.git": "repo",
		"/local/path/to/bare.git":               "bare",
		"/local/plain":                          "plain",
	}
	for in, expected := range cases {
		if actual := repoNameFromURL(in); actual != expected {
			t.Errorf("repoNameFromURL(%q) = %q, expected %q", in, actual, expected)
		}
	}
}

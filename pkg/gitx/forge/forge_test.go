package forge

import "testing"

func TestParsePRNumber(t *testing.T) {
	cases := map[string]int{
		"https://github.com/owner/repo/pull/42\n":                       42,
		"Creating PR...\nhttps://github.com/owner/repo/pull/7 done":     7,
		"https://github.com/long-owner-name/some_repo/pull/12345 extra": 12345,
	}
	for in, expected := range cases {
		actual, err := parsePRNumber(in)
		if err != nil {
			t.Errorf("parsePRNumber(%q): %v", in, err)
			continue
		}
		if actual != expected {
			t.Errorf("parsePRNumber(%q) = %d, expected %d", in, actual, expected)
		}
	}
}

func TestParsePRNumberInvalid(t *testing.T) {
	if _, err := parsePRNumber("no URL here"); err == nil {
		t.Error("expected error when no URL is present")
	}
}

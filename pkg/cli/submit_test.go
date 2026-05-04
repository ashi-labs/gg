package cli

import "testing"

func TestStripScaffold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no scaffold leaves content untouched",
			in:   "Body line 1\n\nBody line 2",
			want: "Body line 1\n\nBody line 2",
		},
		{
			name: "scaffold at end removed and surrounding whitespace trimmed",
			in:   "Body\n\n<!-- gg-scaffold\nfoo\ngg-scaffold -->\n",
			want: "Body",
		},
		{
			name: "only scaffold yields empty",
			in:   "<!-- gg-scaffold\nbranch info\ngg-scaffold -->\n",
			want: "",
		},
		{
			name: "empty input yields empty",
			in:   "",
			want: "",
		},
		{
			name: "only whitespace yields empty",
			in:   "\n\n  \n",
			want: "",
		},
		{
			name: "orphan open marker preserved (don't eat user content past it)",
			in:   "Body\n<!-- gg-scaffold\nno close",
			want: "Body\n<!-- gg-scaffold\nno close",
		},
		{
			name: "scaffold between content sections both halves preserved",
			in:   "Above\n<!-- gg-scaffold\nx\ngg-scaffold -->\nBelow",
			want: "Above\n\nBelow",
		},
		{
			name: "user-written HTML comment is preserved (not the scaffold marker)",
			in:   "Body <!-- TODO: revisit -->\n\n<!-- gg-scaffold\nx\ngg-scaffold -->",
			want: "Body <!-- TODO: revisit -->",
		},
		{
			name: "markdown heading at start of body survives",
			in:   "# Summary\n\nText.\n\n<!-- gg-scaffold\nx\ngg-scaffold -->",
			want: "# Summary\n\nText.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripScaffold(tc.in)
			if got != tc.want {
				t.Errorf("stripScaffold mismatch\n in:   %q\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsTitleBodySeparator(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"---", true},
		{"--- ", true},
		{"---\t", true},
		{"---  \t  ", true},
		{"----", false},  // four dashes
		{"--", false},    // two dashes
		{" ---", false},  // leading space — strict
		{"\t---", false}, // leading tab — strict
		{"--- a", false}, // extra content
		{"a---", false},  // prefix
		{"", false},      // empty
		{"---\n", false}, // separator carries the trailing newline AFTER strings.Split, so the line is "---"; this case verifies a literal embedded \n is a no-match
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := isTitleBodySeparator(tc.in)
			if got != tc.want {
				t.Errorf("isTitleBodySeparator(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSplitTitleAndBody(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "title and body split on --- separator",
			in:        "fix: prefetch reload\n---\nRoutes the precmd through ZLE.",
			wantTitle: "fix: prefetch reload",
			wantBody:  "Routes the precmd through ZLE.",
		},
		{
			name:      "no separator: whole file is body, title empty so caller falls back to seed",
			in:        "fix: prefetch reload\n\nMore detail.",
			wantTitle: "",
			wantBody:  "fix: prefetch reload\n\nMore detail.",
		},
		{
			name:      "title only with separator (empty body)",
			in:        "fix: prefetch reload\n---\n",
			wantTitle: "fix: prefetch reload",
			wantBody:  "",
		},
		{
			name:      "empty title section yields empty title (caller falls back to seed)",
			in:        "---\nbody only",
			wantTitle: "",
			wantBody:  "body only",
		},
		{
			name:      "title section trims surrounding whitespace",
			in:        "  fix: prefetch reload  \n---\nbody",
			wantTitle: "fix: prefetch reload",
			wantBody:  "body",
		},
		{
			name:      "multi-line title is preserved (joined) when above the separator",
			in:        "feat: long\nrolling title\n---\nbody",
			wantTitle: "feat: long\nrolling title",
			wantBody:  "body",
		},
		{
			name:      "markdown horizontal rule in body splits on the FIRST --- found, so a title-position --- wins",
			in:        "title\n---\nbody section\n\n---\n\nsecond section",
			wantTitle: "title",
			wantBody:  "body section\n\n---\n\nsecond section",
		},
		{
			name:      "trailing whitespace on separator line still matches",
			in:        "title\n---   \nbody",
			wantTitle: "title",
			wantBody:  "body",
		},
		{
			name:      "leading-whitespace --- is NOT a separator (treated as body content)",
			in:        "title\n  ---\nbody",
			wantTitle: "",
			wantBody:  "title\n  ---\nbody",
		},
		{
			name:      "multi-line body with internal blank lines preserved",
			in:        "title\n---\nfirst paragraph\n\nsecond paragraph",
			wantTitle: "title",
			wantBody:  "first paragraph\n\nsecond paragraph",
		},
		{
			name:      "empty input yields empty title and body",
			in:        "",
			wantTitle: "",
			wantBody:  "",
		},
		{
			name:      "only whitespace yields empty title and body",
			in:        "\n\n  \n\t\n",
			wantTitle: "",
			wantBody:  "",
		},
		{
			name:      "title that begins with a hash (markdown heading) is kept verbatim",
			in:        "# Summary line as title\n---\nBody",
			wantTitle: "# Summary line as title",
			wantBody:  "Body",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTitle, gotBody := splitTitleAndBody(tc.in)
			if gotTitle != tc.wantTitle {
				t.Errorf("title mismatch\n in:   %q\n got:  %q\n want: %q", tc.in, gotTitle, tc.wantTitle)
			}
			if gotBody != tc.wantBody {
				t.Errorf("body mismatch\n in:   %q\n got:  %q\n want: %q", tc.in, gotBody, tc.wantBody)
			}
		})
	}
}

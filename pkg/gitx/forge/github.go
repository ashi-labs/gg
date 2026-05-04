package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var prURLRegex = regexp.MustCompile(`/pull/(\d+)`)

type xGitHub struct{}

// Statically assert it satisfies the Forge interface — catches drift at compile time.
var GitHub Forge = xGitHub{}

func parsePRNumber(out string) (int, error) {
	m := prURLRegex.FindStringSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("could not parse PR number from: %s", strings.TrimSpace(out))
	}
	return strconv.Atoi(m[1])
}

func (xGitHub) CreatePR(opts CreatePROpts) (string, int, error) {
	args := []string{
		"pr",
		"create",
		"--base",
		opts.BaseBranch,
		"--head",
		opts.HeadBranch,
		"--title",
		opts.Title,
		"--body",
		opts.Body,
	}
	if opts.IsDraft {
		args = append(args, "--draft")
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = opts.Worktree
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf(
			"gh pr create %s → %s: %v\n%s",
			opts.HeadBranch,
			opts.BaseBranch,
			err,
			errBuf.String(),
		)
	}
	url := strings.TrimSpace(out.String())
	num, err := parsePRNumber(url)
	return url, num, err
}

func (xGitHub) GetPRBody(worktree string, num int) (string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(num), "--json", "body", "--jq", ".body")
	cmd.Dir = worktree
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view %d: %w", num, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (xGitHub) EditPRBody(worktree string, num int, body string) error {
	cmd := exec.Command("gh", "pr", "edit", strconv.Itoa(num), "--body", body)
	cmd.Dir = worktree
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh pr edit %d: %v\n%s", num, err, errBuf.String())
	}
	return nil
}

func (xGitHub) SetPRBaseBranch(worktree string, num int, base string) error {
	cmd := exec.Command("gh", "pr", "edit", strconv.Itoa(num), "--base", base)
	cmd.Dir = worktree
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh pr edit %d --base %s: %v\n%s", num, base, err, errBuf.String())
	}
	return nil
}

func (xGitHub) GetPRBaseBranch(worktree string, num int) (string, error) {
	cmd := exec.Command(
		"gh",
		"pr",
		"view",
		strconv.Itoa(num),
		"--json",
		"baseRefName",
		"--jq",
		".baseRefName",
	)
	cmd.Dir = worktree
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view %d base: %w", num, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// prTemplateCandidates is the ordered list of single-file locations
// GitHub recognizes as the repo's PR template. Lowercase variants are
// included because some repos commit them that way and case-insensitive
// filesystems still match. First file that exists wins.
var prTemplateCandidates = []string{
	".github/PULL_REQUEST_TEMPLATE.md",
	".github/pull_request_template.md",
	".github/PULL_REQUEST_TEMPLATE",
	"docs/PULL_REQUEST_TEMPLATE.md",
	"docs/pull_request_template.md",
	"PULL_REQUEST_TEMPLATE.md",
	"pull_request_template.md",
}

// PRTemplate looks for a GitHub-style PR template under worktree and
// returns its contents. Returns ("", nil) when no template is found —
// callers can then fall through to "no template" UX without needing to
// distinguish a missing file from any other empty-template repo.
//
// Multi-template directories (`.github/PULL_REQUEST_TEMPLATE/*.md`)
// are flattened to the first entry alphabetically; multi-template
// picker UX is a follow-up since GitHub itself only surfaces it via
// query string, not via a single canonical template.
func (xGitHub) PRTemplate(worktree string) (string, error) {
	for _, rel := range prTemplateCandidates {
		data, err := os.ReadFile(filepath.Join(worktree, rel))
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("reading PR template %s: %w", rel, err)
		}
	}
	dir := filepath.Join(worktree, ".github", "PULL_REQUEST_TEMPLATE")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "", nil
	}
	sort.Strings(names)
	data, err := os.ReadFile(filepath.Join(dir, names[0]))
	if err != nil {
		return "", fmt.Errorf("reading PR template %s: %w", names[0], err)
	}
	return string(data), nil
}

func (xGitHub) GetPRStatus(worktree string, num int) (PRStatus, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(num), "--json", "state,isDraft")
	cmd.Dir = worktree
	out, err := cmd.Output()
	if err != nil {
		return PRStatus{}, fmt.Errorf("gh pr view %d: %w", num, err)
	}
	var parsed struct {
		State   string `json:"state"`
		IsDraft bool   `json:"isDraft"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return PRStatus{}, fmt.Errorf("gh pr view %d: parsing response: %w", num, err)
	}
	return PRStatus{State: strings.TrimSpace(parsed.State), IsDraft: parsed.IsDraft}, nil
}

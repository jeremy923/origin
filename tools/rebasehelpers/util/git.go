package util

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var UpstreamSummaryPattern = regexp.MustCompile(`UPSTREAM: (revert: [a-f0-9]{7,}: )?(([\w\.-]+\/[\w-]+)?: )?(\d+:|<carry>:|<drop>:)`)

// supportedHosts maps source hosts to the number of path segments that
// represent the account/repo for that host. This is necessary because we
// can't tell just by looking at an import path whether the repo is identified
// by the first 2 or 3 path segments.
//
// If dependencies are introduced from new hosts, they'll need to be added
// here.
var SupportedHosts = map[string]int{
	"bitbucket.org":     3,
	"code.google.com":   3,
	"github.com":        3,
	"golang.org":        3,
	"google.golang.org": 2,
	"gopkg.in":          2,
	"k8s.io":            2,
	"speter.net":        2,
}

type Commit struct {
	Sha     string
	Summary string
	Files   []File
}

func (c Commit) DeclaresUpstreamChange() bool {
	return strings.HasPrefix(strings.ToLower(c.Summary), "upstream")
}

func (c Commit) MatchesUpstreamSummaryPattern() bool {
	return UpstreamSummaryPattern.MatchString(c.Summary)
}

func (c Commit) DeclaredUpstreamRepo() (string, error) {
	if !c.DeclaresUpstreamChange() {
		return "", fmt.Errorf("commit declares no upstream changes")
	}
	if !c.MatchesUpstreamSummaryPattern() {
		return "", fmt.Errorf("commit doesn't match the upstream commit summary pattern")
	}
	groups := UpstreamSummaryPattern.FindStringSubmatch(c.Summary)
	repo := groups[3]
	if len(repo) == 0 {
		repo = "k8s.io/kubernetes"
	}
	return repo, nil
}

func (c Commit) HasGodepsChanges() bool {
	for _, file := range c.Files {
		if file.HasGodepsChanges() {
			return true
		}
	}
	return false
}

func (c Commit) HasNonGodepsChanges() bool {
	for _, file := range c.Files {
		if !file.HasGodepsChanges() {
			return true
		}
	}
	return false
}

func (c Commit) GodepsReposChanged() ([]string, error) {
	repos := map[string]struct{}{}
	for _, file := range c.Files {
		if !file.HasGodepsChanges() {
			continue
		}
		repo, err := file.GodepsRepoChanged()
		if err != nil {
			return nil, fmt.Errorf("problem with file %q in commit %s: %s", file, c.Sha, err)
		}
		repos[repo] = struct{}{}
	}
	changed := []string{}
	for repo := range repos {
		changed = append(changed, repo)
	}
	return changed, nil
}

type File string

func (f File) HasGodepsChanges() bool {
	return strings.HasPrefix(string(f), "Godeps")
}

func (f File) GodepsRepoChanged() (string, error) {
	if !f.HasGodepsChanges() {
		return "", fmt.Errorf("file doesn't appear to be a Godeps change")
	}
	// Find the _workspace path segment index.
	workspaceIdx := -1
	parts := strings.Split(string(f), string(os.PathSeparator))
	for i, part := range parts {
		if part == "_workspace" {
			workspaceIdx = i
			break
		}
	}
	// Godeps path struture assumption: Godeps/_workspace/src/...
	if workspaceIdx == -1 || len(parts) < (workspaceIdx+3) {
		return "", fmt.Errorf("file doesn't appear to be a Godeps workspace path")
	}
	// Deal with repos which could be identified by either 2 or 3 path segments.
	host := parts[workspaceIdx+2]
	segments := -1
	for supportedHost, count := range SupportedHosts {
		if host == supportedHost {
			segments = count
			break
		}
	}
	if segments == -1 {
		return "", fmt.Errorf("file modifies an unsupported repo host %q", host)
	}
	switch segments {
	case 2:
		return fmt.Sprintf("%s/%s", host, parts[workspaceIdx+3]), nil
	case 3:
		return fmt.Sprintf("%s/%s/%s", host, parts[workspaceIdx+3], parts[workspaceIdx+4]), nil
	}
	return "", fmt.Errorf("file modifies an unsupported repo host %q", host)
}

func CommitsBetween(a, b string) ([]Commit, error) {
	commits := []Commit{}
	stdout, _, err := run("git", "log", "--oneline", fmt.Sprintf("%s..%s", a, b))
	if err != nil {
		return nil, fmt.Errorf("error executing git log: %s", err)
	}
	for _, log := range strings.Split(stdout, "\n") {
		if len(log) == 0 {
			continue
		}
		commit, err := NewCommitFromOnelineLog(log)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func NewCommitFromOnelineLog(log string) (Commit, error) {
	var commit Commit
	parts := strings.Split(log, " ")
	if len(parts) < 2 {
		return commit, fmt.Errorf("invalid log entry: %s", log)
	}
	commit.Sha = parts[0]
	commit.Summary = strings.Join(parts[1:], " ")
	files, err := filesInCommit(commit.Sha)
	if err != nil {
		return commit, err
	}
	commit.Files = files
	return commit, nil
}

func IsAncestor(commit1, commit2, repoDir string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	defer os.Chdir(cwd)

	if err := os.Chdir(repoDir); err != nil {
		return false, err
	}

	if stdout, stderr, err := run("git", "fetch", "origin"); err != nil {
		return false, fmt.Errorf("out=%s, err=%s, %s", strings.TrimSpace(stdout), strings.TrimSpace(stderr), err)
	}

	if stdout, stderr, err := run("git", "merge-base", "--is-ancestor", commit1, commit2); err != nil {
		return false, fmt.Errorf("out=%s, err=%s, %s", strings.TrimSpace(stdout), strings.TrimSpace(stderr), err)
	}

	return true, nil
}

func filesInCommit(sha string) ([]File, error) {
	files := []File{}
	stdout, _, err := run("git", "diff-tree", "--no-commit-id", "--name-only", "-r", sha)
	if err != nil {
		return nil, err
	}
	for _, filename := range strings.Split(stdout, "\n") {
		if len(filename) == 0 {
			continue
		}
		files = append(files, File(filename))
	}
	return files, nil
}

func run(args ...string) (string, string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

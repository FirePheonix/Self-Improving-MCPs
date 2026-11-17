package publish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	selectiongate "self-improving-mcp/selection-gate"
)

// BranchResult describes the branch CreateBranch made.
type BranchResult struct {
	Branch string // e.g. improve/overlay-1b1acc5d
	Commit string // commit hash on that branch
	Dir    string // directory inside the repo holding the files, e.g. improvements/1b1acc5d
}

// CreateBranch records a winning overlay on a new local branch: overlay.json, evidence.json and PR.md
// under improvements/<id>/, in one commit.
//
// Safety properties, all covered by tests (they live in buildBranch and apply to every kind of branch):
//   - the caller's working tree and index are never touched (the branch is built in a temporary
//     git worktree from HEAD, so uncommitted work cannot be swept into the commit);
//   - only the files the builder names are staged;
//   - nothing is pushed and no pull request is opened;
//   - an existing branch of the same name is never overwritten;
//   - on any failure the temporary worktree and half-made branch are removed.
func CreateBranch(repoDir string, rep selectiongate.Report, overlayJSON, prMarkdown string) (BranchResult, error) {
	id := ShortID(rep.VersionID)
	if id == "" {
		return BranchResult{}, fmt.Errorf("report has no version id")
	}
	branch := "improve/overlay-" + id
	rel := filepath.ToSlash(filepath.Join("improvements", id))
	commit, err := buildBranch(repoDir, branch, func(tmp string) ([]string, string, string, error) {
		if err := writeEvidenceFiles(filepath.Join(tmp, "improvements", id), rep, overlayJSON, prMarkdown); err != nil {
			return nil, "", "", err
		}
		d := rep.Decision
		subject := fmt.Sprintf("Add overlay %s: fixed %d, broke %d on %d sealed tasks", id, d.Fixed, d.Broken, d.SealedN)
		return []string{rel}, subject, d.Reason, nil
	})
	if err != nil {
		return BranchResult{}, err
	}
	return BranchResult{Branch: branch, Commit: commit, Dir: rel}, nil
}

// writeEvidenceFiles writes overlay.json, evidence.json and PR.md into dir.
func writeEvidenceFiles(dir string, rep selectiongate.Report, overlayJSON, prMarkdown string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(overlayJSON), "", "  ") != nil {
		pretty.Reset()
		pretty.WriteString(overlayJSON)
	}
	evidence, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"overlay.json":  append(pretty.Bytes(), '\n'),
		"evidence.json": append(evidence, '\n'),
		"PR.md":         []byte(prMarkdown),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// buildBranch makes `branch` from HEAD in a throwaway worktree. build writes files under the
// worktree and returns the paths to stage plus the commit message; buildBranch commits them.
func buildBranch(repoDir, branch string, build func(tmp string) (stage []string, subject, body string, err error)) (commit string, err error) {
	top, err := runGit(repoDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository: %w", repoDir, err)
	}
	head, err := runGit(top, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("repository has no commits to branch from: %w", err)
	}
	if existing, _ := runGit(top, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch); existing != "" {
		return "", fmt.Errorf("branch %s already exists; refusing to overwrite it", branch)
	}

	tmp, err := os.MkdirTemp("", "simcp-branch-*")
	if err != nil {
		return "", err
	}
	if _, err := runGit(top, "worktree", "add", "-b", branch, tmp, head); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	defer func() {
		_, _ = runGit(top, "worktree", "remove", "--force", tmp)
		_ = os.RemoveAll(tmp)
		if err != nil { // leave no half-made branch behind
			_, _ = runGit(top, "branch", "-D", branch)
		}
	}()

	stage, subject, body, err := build(tmp)
	if err != nil {
		return "", err
	}
	if len(stage) == 0 {
		err = fmt.Errorf("nothing to commit")
		return "", err
	}
	if _, err = runGit(tmp, append([]string{"add", "--"}, stage...)...); err != nil {
		return "", err
	}
	if _, err = runGit(tmp, "commit", "-m", subject, "-m", body); err != nil {
		return "", err
	}
	commit, err = runGit(tmp, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return commit, nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

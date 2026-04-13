package update

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// DiffResult holds the output of comparing a skill against its upstream.
type DiffResult struct {
	Skill    *models.Skill
	Diff     string // unified diff output; empty means no changes
	HasDiff  bool
	Err      error
}

// DiffSkill fetches the upstream HEAD for skill and returns a unified diff
// of the skill directory against it. It runs:
//
//	git fetch origin
//	git diff HEAD..FETCH_HEAD -- <relative-path-of-skill>
//
// in the git root that contains the skill. If the skill's real path IS the
// git root the scope constraint is omitted so the whole repo is diffed.
func DiffSkill(skill *models.Skill) DiffResult {
	result := DiffResult{Skill: skill}

	if skill.Frontmatter.Repository == "" {
		result.Err = fmt.Errorf("skill %q has no repository URL in frontmatter", skill.Name)
		return result
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		result.Err = fmt.Errorf("git not found in PATH")
		return result
	}

	realPath := resolveReal(skill)
	gitRoot := findGitRoot(realPath)
	if gitRoot == "" {
		result.Err = fmt.Errorf("no git repository found for %s", skill.Name)
		return result
	}

	// Fetch latest from origin
	fetchCmd := exec.Command(gitPath, "fetch", "origin")
	fetchCmd.Dir = gitRoot
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		result.Err = fmt.Errorf("git fetch failed: %s", strings.TrimSpace(string(out)))
		return result
	}

	// Build diff args, scoped to the skill's subdirectory if it's nested
	diffArgs := []string{"diff", "HEAD..FETCH_HEAD"}
	rel, err := filepath.Rel(gitRoot, realPath)
	if err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, "..") {
		diffArgs = append(diffArgs, "--", rel)
	}

	diffCmd := exec.Command(gitPath, diffArgs...)
	diffCmd.Dir = gitRoot
	out, err := diffCmd.Output()
	if err != nil {
		result.Err = fmt.Errorf("git diff failed: %w", err)
		return result
	}

	result.Diff = string(out)
	result.HasDiff = strings.TrimSpace(result.Diff) != ""
	return result
}

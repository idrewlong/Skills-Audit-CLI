package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// ApplyResult describes the outcome of applying an update to a skill.
type ApplyResult struct {
	Skill   *models.Skill
	Updated bool   // true if something actually changed (not "Already up to date")
	Output  string // raw git output
	Err     error
}

// ApplyUpdate runs git pull --ff-only in the skill's git repository root.
// For symlinked skills the real path is resolved first. Returns an error if
// the skill directory is not inside a git repo or git is unavailable.
func ApplyUpdate(skill *models.Skill) ApplyResult {
	result := ApplyResult{Skill: skill}

	realPath := resolveReal(skill)
	gitRoot := findGitRoot(realPath)
	if gitRoot == "" {
		result.Err = fmt.Errorf("no git repository found for %s", skill.Name)
		return result
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		result.Err = fmt.Errorf("git not found in PATH")
		return result
	}

	cmd := exec.Command(gitPath, "pull", "--ff-only")
	cmd.Dir = gitRoot
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))

	if err != nil {
		result.Err = fmt.Errorf("git pull failed: %s", result.Output)
		return result
	}

	result.Updated = !strings.Contains(result.Output, "Already up to date")
	return result
}

// ApplyAll applies updates to every skill in the slice that has HasUpdate set.
// Skills that share a git root are pulled once (the result is attributed to
// all skills in that repo) to avoid redundant network round-trips.
func ApplyAll(skills []*models.Skill) []ApplyResult {
	type entry struct {
		root   string
		skills []*models.Skill
	}
	seen := map[string]*entry{}
	var order []string

	for _, skill := range skills {
		if !skill.HasUpdate {
			continue
		}
		root := findGitRoot(resolveReal(skill))
		if root == "" {
			continue
		}
		if _, ok := seen[root]; !ok {
			seen[root] = &entry{root: root}
			order = append(order, root)
		}
		seen[root].skills = append(seen[root].skills, skill)
	}

	var results []ApplyResult
	for _, root := range order {
		e := seen[root]
		gitPath, err := exec.LookPath("git")
		if err != nil {
			for _, s := range e.skills {
				results = append(results, ApplyResult{
					Skill: s,
					Err:   fmt.Errorf("git not found in PATH"),
				})
			}
			continue
		}

		cmd := exec.Command(gitPath, "pull", "--ff-only")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(out))
		updated := err == nil && !strings.Contains(output, "Already up to date")

		for _, s := range e.skills {
			r := ApplyResult{Skill: s, Output: output, Updated: updated}
			if err != nil {
				r.Err = fmt.Errorf("git pull failed: %s", output)
			}
			results = append(results, r)
		}
	}
	return results
}

// findGitRoot walks up from path looking for a .git directory.
func findGitRoot(path string) string {
	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// resolveReal returns the real (non-symlink) path for a skill.
func resolveReal(skill *models.Skill) string {
	if skill.IsSymlink && skill.SymlinkTarget != "" {
		if resolved, err := filepath.EvalSymlinks(skill.Path); err == nil {
			return resolved
		}
	}
	return skill.Path
}

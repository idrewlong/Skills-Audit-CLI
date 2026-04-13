package update

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/idrewlong/skill-mgr/internal/registry"
	"github.com/idrewlong/skill-mgr/pkg/models"
)

// CheckUpdate checks if a skill has an available update.
// It first tries the skills.sh registry, then falls back to git fetch.
func CheckUpdate(skill *models.Skill) (bool, string, error) {
	repo := skill.Frontmatter.Repository
	if repo == "" {
		return false, "", fmt.Errorf("no repository URL in skill frontmatter")
	}

	// Try registry first
	latestSHA, err := registry.FetchLatestSHA(repo)
	if err == nil && latestSHA != "" {
		if skill.GitSHA == "" {
			return false, latestSHA, nil
		}
		// Compare (both may be short or full SHAs)
		current := skill.GitSHA
		if len(current) > len(latestSHA) {
			current = current[:len(latestSHA)]
		}
		if len(latestSHA) > len(current) {
			latestSHA = latestSHA[:len(current)]
		}
		hasUpdate := !strings.EqualFold(current, latestSHA)
		return hasUpdate, latestSHA, nil
	}

	// Fallback: git ls-remote if git is available
	return checkViaGit(skill, repo)
}

// checkViaGit uses git ls-remote to check upstream HEAD
func checkViaGit(skill *models.Skill, repo string) (bool, string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return false, "", fmt.Errorf("git not available and registry unreachable")
	}

	// Normalize to https URL
	if !strings.HasPrefix(repo, "https://") && !strings.HasPrefix(repo, "http://") {
		repo = "https://github.com/" + repo
	}

	cmd := exec.Command(gitPath, "ls-remote", repo, "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false, "", fmt.Errorf("git ls-remote failed: %w", err)
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return false, "", fmt.Errorf("no output from git ls-remote")
	}

	parts := strings.Fields(output)
	if len(parts) == 0 {
		return false, "", fmt.Errorf("unexpected git ls-remote output")
	}

	upstreamSHA := parts[0]
	shortUpstream := upstreamSHA
	if len(shortUpstream) > 7 {
		shortUpstream = shortUpstream[:7]
	}

	current := skill.GitSHA
	if current == "" {
		return false, shortUpstream, nil
	}

	// Normalize comparison length
	minLen := len(current)
	if len(shortUpstream) < minLen {
		minLen = len(shortUpstream)
	}
	hasUpdate := !strings.EqualFold(current[:minLen], shortUpstream[:minLen])
	return hasUpdate, shortUpstream, nil
}

// CheckAll checks all skills for updates, populating HasUpdate and UpstreamSHA fields
func CheckAll(skills []*models.Skill) []UpdateCheckResult {
	var results []UpdateCheckResult
	for _, skill := range skills {
		hasUpdate, upstreamSHA, err := CheckUpdate(skill)
		result := UpdateCheckResult{
			Skill:       skill,
			HasUpdate:   hasUpdate,
			UpstreamSHA: upstreamSHA,
		}
		if err != nil {
			result.Err = err
		} else {
			skill.HasUpdate = hasUpdate
			skill.UpstreamSHA = upstreamSHA
		}
		results = append(results, result)
	}
	return results
}

// UpdateCheckResult holds the result of checking a single skill for updates
type UpdateCheckResult struct {
	Skill       *models.Skill
	HasUpdate   bool
	UpstreamSHA string
	Err         error
}

package remove

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/idrewlong/skill-mgr/internal/discovery"
	"github.com/idrewlong/skill-mgr/pkg/models"
)

// RemoveResult describes what was removed
type RemoveResult struct {
	RemovedPaths []string
	SkippedPaths []string
	Errors       []string
	IsUniversal  bool
}

// Remove uninstalls a skill. For symlinked universal skills, removes all symlinks
// pointing to the same target. For non-symlinked skills, removes the directory.
// If dryRun is true, returns what would be removed without making changes.
func Remove(skill *models.Skill, dryRun bool) (*RemoveResult, error) {
	result := &RemoveResult{}

	if skill.IsSymlink {
		result.IsUniversal = true
		// Find all symlinks pointing to this skill's real target across all agent dirs
		symlinks, err := findAllSymlinks(skill.SymlinkTarget)
		if err != nil {
			return nil, fmt.Errorf("error finding symlinks: %w", err)
		}

		for _, link := range symlinks {
			if dryRun {
				result.RemovedPaths = append(result.RemovedPaths, link)
				continue
			}
			if err := os.Remove(link); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to remove %s: %v", link, err))
				result.SkippedPaths = append(result.SkippedPaths, link)
			} else {
				result.RemovedPaths = append(result.RemovedPaths, link)
			}
		}

		// Also remove the universal entry
		universalPath := filepath.Join(getUniversalDir(), filepath.Base(skill.Path))
		if _, err := os.Lstat(universalPath); err == nil {
			if dryRun {
				result.RemovedPaths = append(result.RemovedPaths, universalPath)
			} else if err := os.Remove(universalPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to remove %s: %v", universalPath, err))
			} else {
				result.RemovedPaths = append(result.RemovedPaths, universalPath)
			}
		}
	} else {
		// Non-symlinked: remove the whole directory
		if dryRun {
			result.RemovedPaths = append(result.RemovedPaths, skill.Path)
			return result, nil
		}
		if err := os.RemoveAll(skill.Path); err != nil {
			return nil, fmt.Errorf("failed to remove skill directory: %w", err)
		}
		result.RemovedPaths = append(result.RemovedPaths, skill.Path)
	}

	return result, nil
}

// RemoveByName finds a skill by name and removes it
func RemoveByName(name string, skills []*models.Skill, dryRun bool, confirm func(skill *models.Skill) bool) (*RemoveResult, error) {
	skill := discovery.FindByName(skills, name)
	if skill == nil {
		// Try partial match
		name = strings.ToLower(name)
		for _, s := range skills {
			if strings.Contains(strings.ToLower(s.Name), name) {
				skill = s
				break
			}
		}
	}
	if skill == nil {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	if confirm != nil && !confirm(skill) {
		return &RemoveResult{SkippedPaths: []string{skill.Path}}, nil
	}

	return Remove(skill, dryRun)
}

// findAllSymlinks finds all symlinks across known agent dirs pointing to target
func findAllSymlinks(target string) ([]string, error) {
	var found []string
	dirs := discovery.KnownAgentDirs()

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir.Path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			entryPath := filepath.Join(dir.Path, entry.Name())
			info, err := os.Lstat(entryPath)
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				resolved, err := filepath.EvalSymlinks(entryPath)
				if err != nil {
					continue
				}
				if resolved == target {
					found = append(found, entryPath)
				}
			}
		}
	}
	return found, nil
}

func getUniversalDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents", "skills")
}

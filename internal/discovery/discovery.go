package discovery

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// AgentDir defines a known agent skill directory
type AgentDir struct {
	Path   string
	Agent  models.AgentTarget
	Source string // "universal" or "agent-specific"
}

// KnownAgentDirs returns all known skill directories for supported agents
func KnownAgentDirs() []AgentDir {
	home, _ := os.UserHomeDir()

	return []AgentDir{
		// Universal (skills.sh standard)
		{Path: filepath.Join(home, ".agents", "skills"), Agent: models.AgentUniversal, Source: "universal"},

		// Claude Code
		{Path: filepath.Join(home, ".claude", "skills"), Agent: models.AgentClaudeCode, Source: "agent-specific"},

		// Cursor
		{Path: filepath.Join(home, ".cursor", "skills"), Agent: models.AgentCursor, Source: "agent-specific"},
		{Path: filepath.Join(home, "Library", "Application Support", "Cursor", "skills"), Agent: models.AgentCursor, Source: "agent-specific"},

		// Codex CLI
		{Path: filepath.Join(home, ".codex", "skills"), Agent: models.AgentCodex, Source: "agent-specific"},

		// GitHub Copilot
		{Path: filepath.Join(home, ".github-copilot", "skills"), Agent: models.AgentCopilot, Source: "agent-specific"},

		// Cline
		{Path: filepath.Join(home, ".cline", "skills"), Agent: models.AgentCline, Source: "agent-specific"},

		// Amp
		{Path: filepath.Join(home, ".amp", "skills"), Agent: models.AgentAmp, Source: "agent-specific"},

		// Windsurf
		{Path: filepath.Join(home, ".windsurf", "skills"), Agent: models.AgentWindsurf, Source: "agent-specific"},
		{Path: filepath.Join(home, "Library", "Application Support", "Windsurf", "skills"), Agent: models.AgentWindsurf, Source: "agent-specific"},
	}
}

// Discover finds all installed skills across all known agent directories.
// Skills installed via symlink (skills.sh universal) are deduplicated.
func Discover() ([]*models.Skill, error) {
	dirs := KnownAgentDirs()
	// key: resolved real path → *Skill (for dedup)
	seen := map[string]*models.Skill{}
	var ordered []*models.Skill

	for _, dir := range dirs {
		info, err := os.Stat(dir.Path)
		if err != nil || !info.IsDir() {
			continue
		}

		entries, err := os.ReadDir(dir.Path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			skillPath := filepath.Join(dir.Path, entry.Name())
			realPath, isSymlink, target := resolveSymlink(skillPath)

			// Must contain a SKILL.md
			skillMD := filepath.Join(realPath, "SKILL.md")
			if _, err := os.Stat(skillMD); err != nil {
				continue
			}

			if existing, ok := seen[realPath]; ok {
				// Deduplicate: add agent to existing entry
				existing.Agents = appendIfMissing(existing.Agents, dir.Agent)
				continue
			}

			fm := parseFrontmatter(skillMD)
			name := fm.Name
			if name == "" {
				name = entry.Name()
			}

			installedAt := getInstallTime(skillPath)
			gitSHA := getGitSHA(realPath)

			skill := &models.Skill{
				Name:          name,
				Path:          skillPath,
				IsSymlink:     isSymlink,
				SymlinkTarget: target,
				Frontmatter:   fm,
				Agents:        []models.AgentTarget{dir.Agent},
				InstalledAt:   installedAt,
				GitSHA:        gitSHA,
				Source:        dir.Source,
			}

			seen[realPath] = skill
			ordered = append(ordered, skill)
		}
	}

	return ordered, nil
}

// resolveSymlink returns (realPath, isSymlink, target)
func resolveSymlink(path string) (string, bool, string) {
	info, err := os.Lstat(path)
	if err != nil {
		return path, false, ""
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return path, true, ""
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return path, true, target
		}
		return real, true, target
	}
	return path, false, ""
}

// parseFrontmatter extracts YAML frontmatter from SKILL.md (between --- delimiters)
func parseFrontmatter(skillMDPath string) models.SkillFrontmatter {
	f, err := os.Open(skillMDPath)
	if err != nil {
		return models.SkillFrontmatter{}
	}
	defer f.Close()

	var fm models.SkillFrontmatter
	scanner := bufio.NewScanner(f)

	inFrontmatter := false
	var lines []string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break // end of frontmatter
		}
		if inFrontmatter {
			lines = append(lines, line)
		}
	}

	// Simple key: value parser (avoids external yaml dep)
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "name":
			fm.Name = val
		case "description":
			fm.Description = val
		case "version":
			fm.Version = val
		case "author":
			fm.Author = val
		case "repository":
			fm.Repository = val
		}
	}

	return fm
}

// getInstallTime returns the mtime of the skill directory
func getInstallTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// getGitSHA returns the HEAD commit SHA if the skill dir is inside a git repo
func getGitSHA(path string) string {
	// Walk up looking for .git
	dir := path
	for {
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			headFile := filepath.Join(gitDir, "HEAD")
			data, err := os.ReadFile(headFile)
			if err != nil {
				return ""
			}
			head := strings.TrimSpace(string(data))
			if strings.HasPrefix(head, "ref: ") {
				ref := strings.TrimPrefix(head, "ref: ")
				refFile := filepath.Join(gitDir, ref)
				sha, err := os.ReadFile(refFile)
				if err != nil {
					return ""
				}
				s := strings.TrimSpace(string(sha))
				if len(s) >= 7 {
					return s[:7]
				}
				return s
			}
			if len(head) >= 7 {
				return head[:7]
			}
			return head
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func appendIfMissing(agents []models.AgentTarget, agent models.AgentTarget) []models.AgentTarget {
	for _, a := range agents {
		if a == agent {
			return agents
		}
	}
	return append(agents, agent)
}

// FindByName returns the first skill matching name (case-insensitive)
func FindByName(skills []*models.Skill, name string) *models.Skill {
	name = strings.ToLower(name)
	for _, s := range skills {
		if strings.ToLower(s.Name) == name {
			return s
		}
	}
	return nil
}

// ScanDirectory scans a single directory for skills (used for project-level skills)
func ScanDirectory(dir string) ([]*models.Skill, error) {
	var skills []*models.Skill
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		skillPath := filepath.Join(dir, entry.Name())
		skillMD := filepath.Join(skillPath, "SKILL.md")
		if _, err := os.Stat(skillMD); err != nil {
			continue
		}
		fm := parseFrontmatter(skillMD)
		name := fm.Name
		if name == "" {
			name = entry.Name()
		}
		skills = append(skills, &models.Skill{
			Name:        name,
			Path:        skillPath,
			Frontmatter: fm,
			InstalledAt: getInstallTime(skillPath),
			Source:      "project",
		})
	}
	return skills, nil
}

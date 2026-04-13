# skill-mgr — Claude Code Build Brief

You are building `skill-mgr`, a Go CLI tool that manages AI agent skills installed via skills.sh or directly. Zero external dependencies — stdlib only.

## What to build

An agent skill manager with these commands:
- `skill-mgr list` — inventory all installed skills across all agent directories
- `skill-mgr audit` — static security analysis of skill files
- `skill-mgr remove <name>` — symlink-aware uninstall
- `skill-mgr check-updates` — SHA-based update detection
- `skill-mgr info <name>` — full detail on one skill

## Steps

1. Create the directory structure below
2. Write each file with the exact content provided
3. Run `go build ./...` and fix any errors
4. Run `make install` to install the binary
5. Test with `skill-mgr list` and `skill-mgr audit --verbose`

## Directory structure

```
skill-mgr/
├── cmd/skill-mgr/main.go
├── internal/
│   ├── audit/audit.go
│   ├── discovery/discovery.go
│   ├── registry/registry.go
│   ├── remove/remove.go
│   └── update/update.go
├── pkg/
│   ├── models/skill.go
│   └── ui/ui.go
├── .claude/skills/skill-mgr/SKILL.md
├── .github/workflows/ci.yml
├── .goreleaser.yml
├── formula/skill-mgr.rb
├── go.mod
├── LICENSE
├── Makefile
└── README.md
```

---

## `go.mod`

```go
module github.com/idrewlong/skill-mgr

go 1.22.2
```

---

## `pkg/models/skill.go`

```go
package models

import "time"

// AgentTarget represents a supported AI agent
type AgentTarget string

const (
	AgentClaudeCode AgentTarget = "claude-code"
	AgentCursor     AgentTarget = "cursor"
	AgentCodex      AgentTarget = "codex"
	AgentCopilot    AgentTarget = "github-copilot"
	AgentCline      AgentTarget = "cline"
	AgentAmp        AgentTarget = "amp"
	AgentWindsurf   AgentTarget = "windsurf"
	AgentUniversal  AgentTarget = "universal"
)

// RiskLevel represents the security risk of a skill
type RiskLevel string

const (
	RiskSafe     RiskLevel = "SAFE"
	RiskLow      RiskLevel = "LOW"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
	RiskUnknown  RiskLevel = "UNKNOWN"
)

// SkillFrontmatter is parsed from SKILL.md YAML frontmatter
type SkillFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Version      string   `yaml:"version"`
	Author       string   `yaml:"author"`
	Repository   string   `yaml:"repository"`
	AllowedTools []string `yaml:"allowed-tools"`
	Tags         []string `yaml:"tags"`
}

// AuditFinding is a single security finding
type AuditFinding struct {
	Severity    RiskLevel
	Rule        string
	Description string
	File        string
	Line        int
	Evidence    string
}

// AuditResult holds the result of scanning a skill
type AuditResult struct {
	RiskScore  int // 0-100
	RiskLevel  RiskLevel
	Findings   []AuditFinding
	ScannedAt  time.Time
	RegistryScore *RegistryScore // nil if not fetched
}

// RegistryScore holds scores from skills.sh registry
type RegistryScore struct {
	GenResult    string // "Safe" / "Unsafe" / "Unknown"
	SocketAlerts int
	SnykRisk     string // "Low Risk" / "Medium Risk" / "High Risk"
	FetchedAt    time.Time
}

// Skill represents a fully resolved installed skill
type Skill struct {
	Name        string
	Path        string        // absolute path to skill directory
	IsSymlink   bool
	SymlinkTarget string     // resolved path if symlink
	Frontmatter SkillFrontmatter
	Agents      []AgentTarget // which agents this is installed for
	InstalledAt time.Time
	GitSHA      string        // current HEAD SHA of skill dir
	UpstreamSHA string        // latest SHA from upstream (populated by update-check)
	HasUpdate   bool
	Audit       *AuditResult  // nil until audited
	Source      string        // "universal" | "agent-specific"
}

// UpdateStatus describes the update state of a skill
type UpdateStatus string

const (
	UpdateCurrent    UpdateStatus = "up to date"
	UpdateAvailable  UpdateStatus = "update available"
	UpdateUnknown    UpdateStatus = "unknown"
)
```

---

## `internal/discovery/discovery.go`

```go
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
				return strings.TrimSpace(string(sha))[:min(7, len(strings.TrimSpace(string(sha))))]
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
```

---

## `internal/audit/audit.go`

```go
package audit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// Rule defines a single detection pattern
type Rule struct {
	ID          string
	Severity    models.RiskLevel
	Description string
	Pattern     *regexp.Regexp
	Score       int // contribution to overall risk score
}

// rules is the threat intelligence database
var rules = []Rule{
	// Prompt injection / override attempts
	{
		ID:          "PI-001",
		Severity:    models.RiskCritical,
		Description: "Prompt injection: instruction override attempt",
		Pattern:     regexp.MustCompile(`(?i)(ignore (previous|all|prior|above) instructions?|disregard (your|all) (previous|prior)|you (must|will) now|forget everything|new persona|act as (if you are|though you|a )?(?:unrestricted|jailbreak|DAN))`),
		Score:       40,
	},
	{
		ID:          "PI-002",
		Severity:    models.RiskHigh,
		Description: "Instruction to never mention or hide behavior",
		Pattern:     regexp.MustCompile(`(?i)(never (mention|tell|reveal|disclose|say)|do not (tell|inform|reveal|mention)|keep (this|these|it) (secret|hidden|confidential))`),
		Score:       20,
	},

	// Credential / secret exfiltration
	{
		ID:          "EX-001",
		Severity:    models.RiskCritical,
		Description: "Potential credential exfiltration: reads env secrets and transmits",
		Pattern:     regexp.MustCompile(`(?i)(\.env|AWS_SECRET|GITHUB_TOKEN|api[_-]?key|private[_-]?key).{0,80}(curl|wget|fetch|http|POST|send)`),
		Score:       50,
	},
	{
		ID:          "EX-002",
		Severity:    models.RiskHigh,
		Description: "Accesses sensitive credential files (.ssh, .aws, .env)",
		Pattern:     regexp.MustCompile(`(?i)(~/\.ssh|~/\.aws|~\/\.env|\/\.env|id_rsa|id_ed25519|credentials|\.netrc)`),
		Score:       25,
	},
	{
		ID:          "EX-003",
		Severity:    models.RiskHigh,
		Description: "Hardcoded IP address (possible C2 infrastructure)",
		Pattern:     regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
		Score:       15,
	},

	// Dangerous shell patterns
	{
		ID:          "SH-001",
		Severity:    models.RiskCritical,
		Description: "Curl-pipe-bash pattern (remote code execution)",
		Pattern:     regexp.MustCompile(`(?i)curl.{0,100}\|.{0,20}(bash|sh|zsh|fish)`),
		Score:       45,
	},
	{
		ID:          "SH-002",
		Severity:    models.RiskHigh,
		Description: "Suspicious prerequisite installer (fake dependency install)",
		Pattern:     regexp.MustCompile(`(?i)(prerequisite|required|dependency).{0,200}(npm install|pip install|brew install|cargo install).{0,100}(--global|-g|--user)`),
		Score:       20,
	},
	{
		ID:          "SH-003",
		Severity:    models.RiskHigh,
		Description: "Background process or cron scheduling",
		Pattern:     regexp.MustCompile(`(?i)(crontab|launchctl|systemctl|nohup|&$| & |disown|screen -dm|tmux new-session -d)`),
		Score:       20,
	},
	{
		ID:          "SH-004",
		Severity:    models.RiskMedium,
		Description: "Unsafe download of external binary",
		Pattern:     regexp.MustCompile(`(?i)(curl|wget).{0,80}\.(sh|bash|py|rb|exe|bin)\b`),
		Score:       15,
	},

	// Permission overreach
	{
		ID:          "PR-001",
		Severity:    models.RiskHigh,
		Description: "Requests broad filesystem access (rm -rf, chmod 777)",
		Pattern:     regexp.MustCompile(`(?i)(rm\s+-rf\s+[~/]|chmod\s+777|chmod\s+-R\s+777|sudo\s+rm)`),
		Score:       25,
	},
	{
		ID:          "PR-002",
		Severity:    models.RiskMedium,
		Description: "Requests sudo / privilege escalation",
		Pattern:     regexp.MustCompile(`(?i)\bsudo\b`),
		Score:       10,
	},

	// Encoding / obfuscation
	{
		ID:          "OB-001",
		Severity:    models.RiskHigh,
		Description: "Base64 encoded payload (possible obfuscation)",
		Pattern:     regexp.MustCompile(`(?i)(base64\s*(-d|--decode)|echo\s+[A-Za-z0-9+/]{40,}={0,2}\s*\|\s*(base64|bash|sh))`),
		Score:       30,
	},

	// Hook / auto-execution abuse
	{
		ID:          "HK-001",
		Severity:    models.RiskMedium,
		Description: "References Claude Code hooks (auto-execution vectors)",
		Pattern:     regexp.MustCompile(`(?i)(PreToolUse|PostToolUse|Stop|SubagentStop|PreCompact).{0,50}(hook|execute|run|bash)`),
		Score:       10,
	},

	// Social engineering
	{
		ID:          "SE-001",
		Severity:    models.RiskMedium,
		Description: "Social engineering language (urgency, trust manipulation)",
		Pattern:     regexp.MustCompile(`(?i)(this skill (has been|is) (verified|approved|certified) by|trust (this|the following)|you (can|should) trust|official (anthropic|openai|cursor|github))`),
		Score:       15,
	},
}

// AuditSkill performs static analysis on a skill and returns an AuditResult
func AuditSkill(skill *models.Skill) (*models.AuditResult, error) {
	result := &models.AuditResult{
		ScannedAt: time.Now(),
		RiskLevel: models.RiskSafe,
	}

	// Scan all text files in the skill directory
	err := filepath.Walk(skill.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		// Only scan text-ish files
		scannable := map[string]bool{
			".md": true, ".sh": true, ".py": true, ".js": true,
			".ts": true, ".rb": true, ".yaml": true, ".yml": true,
			".json": true, ".txt": true, "": true,
		}
		if !scannable[ext] {
			return nil
		}
		findings, err := scanFile(path)
		if err != nil {
			return nil
		}
		result.Findings = append(result.Findings, findings...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking skill directory: %w", err)
	}

	// Calculate risk score
	score := 0
	for _, finding := range result.Findings {
		for _, rule := range rules {
			if rule.ID == finding.Rule {
				score += rule.Score
				break
			}
		}
	}
	if score > 100 {
		score = 100
	}
	result.RiskScore = score
	result.RiskLevel = scoreToLevel(score)

	return result, nil
}

// scanFile runs all rules against a single file, returning findings
func scanFile(path string) ([]models.AuditFinding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []models.AuditFinding
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		for _, rule := range rules {
			if rule.Pattern.MatchString(line) {
				evidence := strings.TrimSpace(line)
				if len(evidence) > 120 {
					evidence = evidence[:117] + "..."
				}
				findings = append(findings, models.AuditFinding{
					Severity:    rule.Severity,
					Rule:        rule.ID,
					Description: rule.Description,
					File:        path,
					Line:        lineNum,
					Evidence:    evidence,
				})
				break // one finding per rule per line
			}
		}
	}

	return findings, scanner.Err()
}

// scoreToLevel converts a numeric score to a RiskLevel
func scoreToLevel(score int) models.RiskLevel {
	switch {
	case score == 0:
		return models.RiskSafe
	case score <= 15:
		return models.RiskLow
	case score <= 35:
		return models.RiskMedium
	case score <= 60:
		return models.RiskHigh
	default:
		return models.RiskCritical
	}
}

// AuditAll audits all skills in a slice, returning results in-place
func AuditAll(skills []*models.Skill) {
	for _, skill := range skills {
		result, err := AuditSkill(skill)
		if err == nil {
			skill.Audit = result
		}
	}
}

// Summary returns a human-readable summary of findings
func Summary(result *models.AuditResult) string {
	if result == nil {
		return "not audited"
	}
	if len(result.Findings) == 0 {
		return fmt.Sprintf("✓ clean (score: %d)", result.RiskScore)
	}
	return fmt.Sprintf("%s (score: %d, %d finding(s))",
		result.RiskLevel, result.RiskScore, len(result.Findings))
}
```

---

## `internal/registry/registry.go`

```go
package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

const (
	skillsShBaseURL = "https://registry.skills.sh/api/v1"
	userAgent       = "skill-mgr/1.0 (https://github.com/idrewlong/skill-mgr)"
	timeout         = 10 * time.Second
)

// SkillsShResponse is the shape of the skills.sh registry API response
type SkillsShResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
	Security struct {
		Gen struct {
			Result string `json:"result"` // "Safe" | "Unsafe" | "Unknown"
		} `json:"gen"`
		Socket struct {
			AlertCount int `json:"alert_count"`
		} `json:"socket"`
		Snyk struct {
			RiskLevel string `json:"risk_level"` // "Low Risk" | "Medium Risk" | "High Risk"
		} `json:"snyk"`
	} `json:"security"`
}

var client = &http.Client{Timeout: timeout}

// FetchScore retrieves the security score for a skill from the skills.sh registry.
// skillSlug should be in the format "owner/repo" or "owner/repo@skill-name".
func FetchScore(skillSlug string) (*models.RegistryScore, error) {
	// Normalize slug: strip leading https://github.com/ etc.
	slug := normalizeSlug(skillSlug)
	if slug == "" {
		return nil, fmt.Errorf("could not determine registry slug for skill")
	}

	url := fmt.Sprintf("%s/skills/%s/security", skillsShBaseURL, slug)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("skill not found in registry: %s", slug)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var data SkillsShResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid registry response: %w", err)
	}

	return &models.RegistryScore{
		GenResult:    data.Security.Gen.Result,
		SocketAlerts: data.Security.Socket.AlertCount,
		SnykRisk:     data.Security.Snyk.RiskLevel,
		FetchedAt:    time.Now(),
	}, nil
}

// FetchLatestSHA returns the latest commit SHA for a skill from the registry.
// Used for update checking.
func FetchLatestSHA(skillSlug string) (string, error) {
	slug := normalizeSlug(skillSlug)
	if slug == "" {
		return "", fmt.Errorf("could not determine registry slug")
	}

	url := fmt.Sprintf("%s/skills/%s", skillsShBaseURL, slug)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var data SkillsShResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("invalid registry response: %w", err)
	}

	return data.GitSHA, nil
}

// normalizeSlug converts various skill identifiers to owner/repo[@skill] format
func normalizeSlug(raw string) string {
	raw = strings.TrimSpace(raw)

	// Already a slug like "greensock/gsap-skills" or "greensock/gsap-skills@gsap-core"
	if !strings.Contains(raw, "://") && strings.Contains(raw, "/") {
		return raw
	}

	// GitHub URL: https://github.com/owner/repo
	raw = strings.TrimPrefix(raw, "https://github.com/")
	raw = strings.TrimPrefix(raw, "http://github.com/")
	raw = strings.TrimSuffix(raw, ".git")

	if strings.Contains(raw, "/") {
		return raw
	}

	return ""
}

// FormatRegistryScore returns a compact display string for a registry score
func FormatRegistryScore(rs *models.RegistryScore) string {
	if rs == nil {
		return "not in registry"
	}

	gen := rs.GenResult
	if gen == "" {
		gen = "unknown"
	}

	socket := "0 alerts"
	if rs.SocketAlerts > 0 {
		socket = fmt.Sprintf("%d alert(s)", rs.SocketAlerts)
	}

	snyk := rs.SnykRisk
	if snyk == "" {
		snyk = "unknown"
	}

	return fmt.Sprintf("Gen: %s | Socket: %s | Snyk: %s", gen, socket, snyk)
}
```

---

## `internal/remove/remove.go`

```go
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
	RemovedPaths  []string
	SkippedPaths  []string
	Errors        []string
	IsUniversal   bool
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
```

---

## `internal/update/update.go`

```go
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
```

---

## `pkg/ui/ui.go`

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// ANSI color codes
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Gray    = "\033[90m"

	BgRed    = "\033[41m"
	BgGreen  = "\033[42m"
	BgYellow = "\033[43m"
)

// RiskColor returns an ANSI color for a risk level
func RiskColor(level models.RiskLevel) string {
	switch level {
	case models.RiskSafe:
		return Green
	case models.RiskLow:
		return Cyan
	case models.RiskMedium:
		return Yellow
	case models.RiskHigh:
		return Red
	case models.RiskCritical:
		return Bold + Red
	default:
		return Gray
	}
}

// RiskBadge returns a colored badge string for a risk level
func RiskBadge(level models.RiskLevel) string {
	color := RiskColor(level)
	icon := riskIcon(level)
	return fmt.Sprintf("%s%s %s%s", color, icon, level, Reset)
}

func riskIcon(level models.RiskLevel) string {
	switch level {
	case models.RiskSafe:
		return "✓"
	case models.RiskLow:
		return "◎"
	case models.RiskMedium:
		return "⚠"
	case models.RiskHigh:
		return "✗"
	case models.RiskCritical:
		return "☠"
	default:
		return "?"
	}
}

// UpdateBadge returns a colored update indicator
func UpdateBadge(skill *models.Skill) string {
	if skill.HasUpdate {
		return fmt.Sprintf("%s↑ update available%s", Yellow, Reset)
	}
	if skill.UpstreamSHA != "" {
		return fmt.Sprintf("%s✓ current%s", Green, Reset)
	}
	return fmt.Sprintf("%s— unknown%s", Gray, Reset)
}

// Header prints the skill-mgr banner
func Header() {
	fmt.Printf("\n%s%s skill-mgr%s %s— agent skill manager%s\n",
		Bold, Cyan, Reset, Gray, Reset)
	fmt.Println(strings.Repeat("─", 60))
}

// PrintSkillTable prints a formatted table of skills
func PrintSkillTable(skills []*models.Skill, showAudit bool) {
	if len(skills) == 0 {
		fmt.Printf("%sNo skills found.%s\n", Gray, Reset)
		return
	}

	// Column widths
	nameW := 20
	agentW := 28
	for _, s := range skills {
		if len(s.Name) > nameW {
			nameW = len(s.Name)
		}
	}

	// Header row
	fmt.Printf("\n%s%-*s  %-*s  %-8s  %s%s\n",
		Bold,
		nameW, "NAME",
		agentW, "AGENTS",
		"SHA",
		ternary(showAudit, "RISK", "INSTALLED"),
		Reset,
	)
	fmt.Println(strings.Repeat("─", nameW+agentW+40))

	for _, s := range skills {
		agents := formatAgents(s.Agents)
		sha := s.GitSHA
		if sha == "" {
			sha = Gray + "——————" + Reset
		}

		var lastCol string
		if showAudit && s.Audit != nil {
			lastCol = RiskBadge(s.Audit.RiskLevel)
		} else {
			lastCol = formatTime(s.InstalledAt)
		}

		symlink := ""
		if s.IsSymlink {
			symlink = fmt.Sprintf(" %s⇒%s", Gray, Reset)
		}

		update := ""
		if s.HasUpdate {
			update = fmt.Sprintf(" %s↑%s", Yellow, Reset)
		}

		fmt.Printf("%-*s%s%s  %-*s  %s%-8s%s  %s\n",
			nameW, s.Name,
			symlink, update,
			agentW, agents,
			Gray, sha, Reset,
			lastCol,
		)
	}
	fmt.Println()
}

// PrintSkillDetail prints full detail for a single skill
func PrintSkillDetail(s *models.Skill) {
	fmt.Printf("\n%s%s%s\n", Bold, s.Name, Reset)
	fmt.Println(strings.Repeat("─", 50))

	if s.Frontmatter.Description != "" {
		fmt.Printf("  %sDesc:%s    %s\n", Gray, Reset, s.Frontmatter.Description)
	}
	if s.Frontmatter.Author != "" {
		fmt.Printf("  %sAuthor:%s  %s\n", Gray, Reset, s.Frontmatter.Author)
	}
	if s.Frontmatter.Repository != "" {
		fmt.Printf("  %sRepo:%s    %s\n", Gray, Reset, s.Frontmatter.Repository)
	}
	if s.Frontmatter.Version != "" {
		fmt.Printf("  %sVersion:%s %s\n", Gray, Reset, s.Frontmatter.Version)
	}

	fmt.Printf("  %sPath:%s    %s\n", Gray, Reset, s.Path)
	if s.IsSymlink {
		fmt.Printf("  %sTarget:%s  %s\n", Gray, Reset, s.SymlinkTarget)
	}
	fmt.Printf("  %sAgents:%s  %s\n", Gray, Reset, formatAgents(s.Agents))
	if s.GitSHA != "" {
		fmt.Printf("  %sSHA:%s     %s\n", Gray, Reset, s.GitSHA)
	}
	if !s.InstalledAt.IsZero() {
		fmt.Printf("  %sAdded:%s   %s\n", Gray, Reset, s.InstalledAt.Format("2006-01-02"))
	}

	if s.Audit != nil {
		fmt.Printf("\n  %sSecurity Audit%s\n", Bold, Reset)
		fmt.Printf("  Risk:    %s (score: %d/100)\n", RiskBadge(s.Audit.RiskLevel), s.Audit.RiskScore)
		fmt.Printf("  Scanned: %s\n", s.Audit.ScannedAt.Format("2006-01-02 15:04"))

		if len(s.Audit.Findings) == 0 {
			fmt.Printf("  %s✓ No issues found%s\n", Green, Reset)
		} else {
			fmt.Printf("\n  %sFindings (%d):%s\n", Bold, len(s.Audit.Findings), Reset)
			for _, f := range s.Audit.Findings {
				color := RiskColor(f.Severity)
				fmt.Printf("  %s[%s] %s%s\n", color, f.Rule, f.Description, Reset)
				fmt.Printf("    %s%s:%d%s\n", Gray, f.File, f.Line, Reset)
				if f.Evidence != "" {
					fmt.Printf("    %s→ %s%s\n", Dim, f.Evidence, Reset)
				}
			}
		}

		if s.Audit.RegistryScore != nil {
			fmt.Printf("\n  %sRegistry Scores%s\n", Bold, Reset)
			rs := s.Audit.RegistryScore
			fmt.Printf("  Gen:    %s\n", rs.GenResult)
			fmt.Printf("  Socket: %d alert(s)\n", rs.SocketAlerts)
			fmt.Printf("  Snyk:   %s\n", rs.SnykRisk)
		}
	}

	if s.HasUpdate {
		fmt.Printf("\n  %s↑ Update available%s (upstream: %s)\n", Yellow, Reset, s.UpstreamSHA)
	}
	fmt.Println()
}

// Confirm prompts the user for y/n confirmation
func Confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	var resp string
	fmt.Scanln(&resp)
	return strings.ToLower(strings.TrimSpace(resp)) == "y"
}

// Spinner prints a simple inline progress indicator
func Spinner(label string) func() {
	done := make(chan struct{})
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
		i := 0
		for {
			select {
			case <-done:
				fmt.Printf("\r%s\r", strings.Repeat(" ", len(label)+4))
				return
			default:
				fmt.Printf("\r%s%s%s %s", Cyan, frames[i%len(frames)], Reset, label)
				time.Sleep(80 * time.Millisecond)
				i++
			}
		}
	}()
	return func() { close(done) }
}

// formatAgents formats a slice of agent targets as a compact string
func formatAgents(agents []models.AgentTarget) string {
	if len(agents) == 0 {
		return Gray + "none" + Reset
	}
	parts := make([]string, len(agents))
	for i, a := range agents {
		parts[i] = string(a)
	}
	return strings.Join(parts, ", ")
}

// formatTime returns a human-readable time string
func formatTime(t time.Time) string {
	if t.IsZero() {
		return Gray + "unknown" + Reset
	}
	return t.Format("2006-01-02")
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// PrintSuccess prints a success message
func PrintSuccess(msg string) {
	fmt.Printf("%s✓ %s%s\n", Green, msg, Reset)
}

// PrintError prints an error message
func PrintError(msg string) {
	fmt.Printf("%s✗ %s%s\n", Red, msg, Reset)
}

// PrintWarn prints a warning message
func PrintWarn(msg string) {
	fmt.Printf("%s⚠ %s%s\n", Yellow, msg, Reset)
}
```

---

## `cmd/skill-mgr/main.go`

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/idrewlong/skill-mgr/internal/audit"
	"github.com/idrewlong/skill-mgr/internal/discovery"
	"github.com/idrewlong/skill-mgr/internal/registry"
	"github.com/idrewlong/skill-mgr/internal/remove"
	"github.com/idrewlong/skill-mgr/internal/update"
	"github.com/idrewlong/skill-mgr/pkg/models"
	"github.com/idrewlong/skill-mgr/pkg/ui"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "list", "ls":
		cmdList(args)
	case "audit":
		cmdAudit(args)
	case "remove", "rm", "uninstall":
		cmdRemove(args)
	case "check-updates", "update-check", "outdated":
		cmdCheckUpdates(args)
	case "info":
		cmdInfo(args)
	case "version", "--version", "-v":
		fmt.Printf("skill-mgr v%s\n", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		ui.PrintError(fmt.Sprintf("unknown command: %s", cmd))
		printHelp()
		os.Exit(1)
	}
}

// ─── list ────────────────────────────────────────────────────────────────────

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	agent := fs.String("agent", "", "filter by agent (e.g. claude-code, cursor, codex)")
	auditFlag := fs.Bool("audit", false, "run security audit on each skill")
	jsonFlag := fs.Bool("json", false, "output as JSON")
	fs.Parse(args)

	ui.Header()

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	if len(skills) == 0 {
		fmt.Println("No skills found. Install skills with: npx skills add <skill>")
		return
	}

	// Filter by agent if requested
	if *agent != "" {
		skills = filterByAgent(skills, *agent)
	}

	if *auditFlag {
		stop2 := ui.Spinner(fmt.Sprintf("Auditing %d skills...", len(skills)))
		audit.AuditAll(skills)
		stop2()
	}

	if *jsonFlag {
		printJSON(skills)
		return
	}

	fmt.Printf("  %s%d skills installed%s\n", ui.Gray, len(skills), ui.Reset)
	ui.PrintSkillTable(skills, *auditFlag)

	if *auditFlag {
		printAuditSummary(skills)
	}
}

// ─── audit ───────────────────────────────────────────────────────────────────

func cmdAudit(args []string) {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	skillName := fs.String("skill", "", "audit a specific skill by name")
	registryFlag := fs.Bool("registry", false, "also fetch scores from skills.sh registry")
	verbose := fs.Bool("verbose", false, "show all findings in detail")
	fs.Parse(args)

	// If a positional arg provided, treat as skill name
	if *skillName == "" && len(fs.Args()) > 0 {
		*skillName = fs.Args()[0]
	}

	ui.Header()

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	// Filter to specific skill if named
	if *skillName != "" {
		skill := discovery.FindByName(skills, *skillName)
		if skill == nil {
			ui.PrintError(fmt.Sprintf("skill %q not found", *skillName))
			os.Exit(1)
		}
		skills = []*models.Skill{skill}
	}

	fmt.Printf("  Auditing %s%d skill(s)%s...\n\n", ui.Bold, len(skills), ui.Reset)

	exitCode := 0
	for _, skill := range skills {
		stop := ui.Spinner(fmt.Sprintf("Scanning %s...", skill.Name))
		result, err := audit.AuditSkill(skill)
		stop()
		skill.Audit = result

		if err != nil {
			ui.PrintError(fmt.Sprintf("%s: scan failed: %v", skill.Name, err))
			continue
		}

		if *registryFlag && skill.Frontmatter.Repository != "" {
			rs, err := registry.FetchScore(skill.Frontmatter.Repository)
			if err == nil {
				result.RegistryScore = rs
			}
		}

		// Print result
		badge := ui.RiskBadge(result.RiskLevel)
		fmt.Printf("  %s%-24s%s %s  (score: %d/100, %d finding(s))\n",
			ui.Bold, skill.Name, ui.Reset,
			badge, result.RiskScore, len(result.Findings))

		if *verbose && len(result.Findings) > 0 {
			for _, f := range result.Findings {
				color := ui.RiskColor(f.Severity)
				fmt.Printf("    %s[%s] %s%s\n", color, f.Rule, f.Description, ui.Reset)
				fmt.Printf("    %s  %s:%d%s\n", ui.Gray, f.File, f.Line, ui.Reset)
				if f.Evidence != "" {
					fmt.Printf("    %s  → %s%s\n", ui.Dim, f.Evidence, ui.Reset)
				}
			}
		} else if len(result.Findings) > 0 && !*verbose {
			fmt.Printf("    %sRun with --verbose to see findings%s\n", ui.Gray, ui.Reset)
		}

		if *registryFlag && result.RegistryScore != nil {
			fmt.Printf("    %sRegistry:%s %s\n", ui.Gray, ui.Reset,
				registry.FormatRegistryScore(result.RegistryScore))
		}

		if result.RiskLevel == models.RiskHigh || result.RiskLevel == models.RiskCritical {
			exitCode = 1
		}
	}

	fmt.Println()
	printAuditSummary(skills)

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// ─── remove ──────────────────────────────────────────────────────────────────

func cmdRemove(args []string) {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "show what would be removed without making changes")
	force := fs.Bool("force", false, "skip confirmation prompt")
	fs.Parse(args)

	if len(fs.Args()) == 0 {
		ui.PrintError("usage: skill-mgr remove <skill-name> [--dry-run] [--force]")
		os.Exit(1)
	}

	skillName := strings.Join(fs.Args(), " ")

	ui.Header()

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	confirmFn := func(skill *models.Skill) bool {
		if *force {
			return true
		}
		if *dryRun {
			return true
		}
		msg := fmt.Sprintf("Remove %s%s%s?", ui.Bold, skill.Name, ui.Reset)
		if skill.IsSymlink {
			msg += fmt.Sprintf(" %s(will remove all agent symlinks)%s", ui.Yellow, ui.Reset)
		}
		return ui.Confirm(msg)
	}

	result, err := remove.RemoveByName(skillName, skills, *dryRun, confirmFn)
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("\n  %sDry run — would remove:%s\n", ui.Yellow, ui.Reset)
		for _, p := range result.RemovedPaths {
			fmt.Printf("  %s× %s%s\n", ui.Red, p, ui.Reset)
		}
		return
	}

	for _, p := range result.RemovedPaths {
		ui.PrintSuccess(fmt.Sprintf("Removed %s", p))
	}
	for _, p := range result.SkippedPaths {
		ui.PrintWarn(fmt.Sprintf("Skipped %s", p))
	}
	for _, e := range result.Errors {
		ui.PrintError(e)
	}

	if len(result.Errors) > 0 {
		os.Exit(1)
	}
}

// ─── check-updates ───────────────────────────────────────────────────────────

func cmdCheckUpdates(args []string) {
	fs := flag.NewFlagSet("check-updates", flag.ExitOnError)
	skillName := fs.String("skill", "", "check a specific skill by name")
	fs.Parse(args)

	if *skillName == "" && len(fs.Args()) > 0 {
		*skillName = fs.Args()[0]
	}

	ui.Header()

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	if *skillName != "" {
		skill := discovery.FindByName(skills, *skillName)
		if skill == nil {
			ui.PrintError(fmt.Sprintf("skill %q not found", *skillName))
			os.Exit(1)
		}
		skills = []*models.Skill{skill}
	}

	fmt.Printf("  Checking %s%d skill(s)%s for updates...\n\n", ui.Bold, len(skills), ui.Reset)

	results := update.CheckAll(skills)

	updatesAvailable := 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("  %-24s %s— %v%s\n", r.Skill.Name, ui.Gray, r.Err, ui.Reset)
			continue
		}
		badge := ui.UpdateBadge(r.Skill)
		sha := ""
		if r.UpstreamSHA != "" {
			sha = fmt.Sprintf(" %s(%s)%s", ui.Gray, r.UpstreamSHA, ui.Reset)
		}
		fmt.Printf("  %-24s %s%s\n", r.Skill.Name, badge, sha)
		if r.HasUpdate {
			updatesAvailable++
		}
	}

	fmt.Println()
	if updatesAvailable > 0 {
		ui.PrintWarn(fmt.Sprintf("%d update(s) available. Run: npx skills update", updatesAvailable))
	} else {
		ui.PrintSuccess("All skills are up to date")
	}
	fmt.Println()
}

// ─── info ────────────────────────────────────────────────────────────────────

func cmdInfo(args []string) {
	if len(args) == 0 {
		ui.PrintError("usage: skill-mgr info <skill-name>")
		os.Exit(1)
	}

	skillName := strings.Join(args, " ")

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	skill := discovery.FindByName(skills, skillName)
	if skill == nil {
		ui.PrintError(fmt.Sprintf("skill %q not found", skillName))
		os.Exit(1)
	}

	// Run audit
	result, _ := audit.AuditSkill(skill)
	skill.Audit = result

	// Check for updates
	hasUpdate, upstreamSHA, _ := update.CheckUpdate(skill)
	skill.HasUpdate = hasUpdate
	skill.UpstreamSHA = upstreamSHA

	ui.PrintSkillDetail(skill)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func printAuditSummary(skills []*models.Skill) {
	counts := map[models.RiskLevel]int{}
	audited := 0
	for _, s := range skills {
		if s.Audit != nil {
			counts[s.Audit.RiskLevel]++
			audited++
		}
	}
	if audited == 0 {
		return
	}

	fmt.Printf("  %sAudit Summary%s (%d scanned)\n", ui.Bold, ui.Reset, audited)
	for _, level := range []models.RiskLevel{
		models.RiskCritical, models.RiskHigh, models.RiskMedium,
		models.RiskLow, models.RiskSafe,
	} {
		if n := counts[level]; n > 0 {
			fmt.Printf("  %s  %d %s%s\n", ui.RiskColor(level), n, level, ui.Reset)
		}
	}
	fmt.Println()
}

func filterByAgent(skills []*models.Skill, agent string) []*models.Skill {
	var filtered []*models.Skill
	for _, s := range skills {
		for _, a := range s.Agents {
			if strings.EqualFold(string(a), agent) {
				filtered = append(filtered, s)
				break
			}
		}
	}
	return filtered
}

func printJSON(skills []*models.Skill) {
	fmt.Println("[")
	for i, s := range skills {
		comma := ","
		if i == len(skills)-1 {
			comma = ""
		}
		agents := make([]string, len(s.Agents))
		for j, a := range s.Agents {
			agents[j] = fmt.Sprintf("%q", string(a))
		}
		fmt.Printf(`  {"name":%q,"path":%q,"symlink":%v,"agents":[%s],"sha":%q}%s`+"\n",
			s.Name, s.Path, s.IsSymlink, strings.Join(agents, ","), s.GitSHA, comma)
	}
	fmt.Println("]")
}

func printHelp() {
	fmt.Printf(`
%sskill-mgr%s v%s — agent skill manager

%sUSAGE%s
  skill-mgr <command> [options]

%sCOMMANDS%s
  list               List all installed skills across all agents
    --agent <name>   Filter by agent (claude-code, cursor, codex, ...)
    --audit          Run security audit on each skill
    --json           Output as JSON

  audit [skill]      Security scan all skills (or one by name)
    --verbose        Show full findings detail
    --registry       Also fetch scores from skills.sh registry

  remove <name>      Uninstall a skill (handles symlinks across agents)
    --dry-run        Preview what would be removed
    --force          Skip confirmation prompt

  check-updates      Check all skills for available updates
  check-updates <n>  Check a single skill by name

  info <name>        Full detail for a skill (audit + update check)

  version            Print version
  help               Show this help

%sEXAMPLES%s
  skill-mgr list
  skill-mgr list --audit
  skill-mgr audit --verbose
  skill-mgr audit gsap-core
  skill-mgr remove gsap-core --dry-run
  skill-mgr remove gsap-core
  skill-mgr check-updates
  skill-mgr info frontend-design

%sINSTALL%s
  go install github.com/idrewlong/skill-mgr/cmd/skill-mgr@latest
  brew install idrewlong/tap/skill-mgr

`, ui.Bold+ui.Cyan, ui.Reset, version,
		ui.Bold, ui.Reset,
		ui.Bold, ui.Reset,
		ui.Bold, ui.Reset,
		ui.Bold, ui.Reset)
}
```

---

## `.claude/skills/skill-mgr/SKILL.md`

```markdown
---
name: skill-mgr
description: >
  Manage, audit, and uninstall AI agent skills installed via skills.sh or directly.
  Use when the user asks to: list installed skills, audit skill security, check for
  suspicious or malicious skills, uninstall/remove a skill, check for skill updates,
  or get info on a specific skill. Triggers on: "what skills do I have", "audit my
  skills", "is this skill safe", "remove skill", "uninstall skill", "skill updates",
  "check skill security", "skill-mgr". Do NOT trigger for general coding help,
  skill usage questions, or requests to install new skills (use npx skills add).
version: 1.0.0
author: idrewlong
repository: idrewlong/skill-mgr
allowed-tools: Bash
---

# skill-mgr — Agent Skill Manager

You are helping the user manage their installed AI agent skills using the `skill-mgr`
CLI. The tool is installed at a path resolvable via `which skill-mgr` or
`~/.local/bin/skill-mgr`.

## Installation check

Before running any command, verify skill-mgr is installed:

```bash
which skill-mgr || echo "NOT_INSTALLED"
```

If not installed, offer to install it:

```bash
go install github.com/idrewlong/skill-mgr/cmd/skill-mgr@latest
# or via Homebrew:
brew install idrewlong/tap/skill-mgr
```

## Commands

### List all installed skills
```bash
skill-mgr list
```
Use `--audit` to include a risk score per skill, `--agent <name>` to filter by agent.

### Audit for security threats
```bash
skill-mgr audit --verbose
```
Use this when the user wants to know if any of their skills are malicious, suspicious,
or have security issues. The auditor checks for:
- Prompt injection / instruction override attempts
- Credential exfiltration patterns (.ssh, .aws, .env access + network calls)
- Curl-pipe-bash and remote code execution patterns
- Hardcoded C2 IP addresses
- Background process / cron scheduling
- Base64-encoded payloads (obfuscation)
- Privilege escalation (sudo misuse, rm -rf)
- Social engineering language

Add `--registry` to also fetch Gen/Socket/Snyk scores from the skills.sh registry.

### Audit a specific skill
```bash
skill-mgr audit <skill-name> --verbose
```

### Remove / uninstall a skill
```bash
# Preview first (always recommended)
skill-mgr remove <skill-name> --dry-run

# Then confirm and remove
skill-mgr remove <skill-name>
```

For skills installed via `npx skills add` (symlinked universally), `remove` will
clean up all agent symlinks automatically.

### Check for updates
```bash
skill-mgr check-updates
```

### Full detail on one skill
```bash
skill-mgr info <skill-name>
```

## Response guidance

- Always run `skill-mgr list` first if the user hasn't specified a skill name.
- For security questions, run `skill-mgr audit --verbose` and interpret the findings
  for the user — explain what each rule ID means in plain language.
- If a skill is flagged CRITICAL or HIGH, proactively suggest removing it and show
  the `--dry-run` output before confirming.
- Risk levels: SAFE (0), LOW (1–15), MEDIUM (16–35), HIGH (36–60), CRITICAL (61–100).
- Rule ID prefixes: PI = prompt injection, EX = exfiltration, SH = shell danger,
  PR = permission overreach, OB = obfuscation, HK = hook abuse, SE = social engineering.
- When removing a skill, always show `--dry-run` output first and get explicit user
  confirmation before running the actual remove.
- Surface registry scores (Gen/Socket/Snyk) when available — these are independent
  third-party assessments, not generated by skill-mgr itself.

## Example user flows

**"What skills do I have installed?"**
→ `skill-mgr list`

**"Are my skills safe?"**
→ `skill-mgr list --audit`, then `skill-mgr audit --verbose` if anything flags

**"Remove the gsap-core skill"**
→ `skill-mgr remove gsap-core --dry-run`, show output, confirm, then remove

**"Is frontend-design up to date?"**
→ `skill-mgr check-updates frontend-design`

**"Tell me everything about my skill-mgr skill"**
→ `skill-mgr info skill-mgr`
```

---

## `Makefile`

```makefile
BINARY     := skill-mgr
MODULE     := github.com/idrewlong/skill-mgr
CMD        := ./cmd/skill-mgr
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -s -w"
INSTALL_DIR := $(HOME)/.local/bin

.PHONY: all build install uninstall test lint clean release snapshot

all: build

## build: compile binary for current platform
build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)
	@echo "Built: ./$(BINARY)"

## install: install to ~/.local/bin (or /usr/local/bin with sudo)
install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"
	@echo "Make sure $(INSTALL_DIR) is in your PATH"

## uninstall: remove from install dir
uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "Removed $(INSTALL_DIR)/$(BINARY)"

## test: run all tests
test:
	go test ./... -v

## lint: run go vet
lint:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf dist/

## release: build for all platforms via goreleaser
release:
	goreleaser release --clean

## snapshot: local goreleaser snapshot (no publish)
snapshot:
	goreleaser release --snapshot --clean

## build-all: cross-compile for common platforms
build-all:
	GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)_darwin_amd64  $(CMD)
	GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o dist/$(BINARY)_darwin_arm64  $(CMD)
	GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)_linux_amd64   $(CMD)
	GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o dist/$(BINARY)_linux_arm64   $(CMD)
	GOOS=windows GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)_windows_amd64.exe $(CMD)
	@echo "Cross-compiled binaries in dist/"

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
```

---

## `.goreleaser.yml`

```yaml
version: 2

project_name: skill-mgr

before:
  hooks:
    - go mod tidy
    - go vet ./...

builds:
  - id: skill-mgr
    main: ./cmd/skill-mgr
    binary: skill-mgr
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}

archives:
  - id: default
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - README.md
      - LICENSE

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"
      - "Merge pull request"
      - "Merge branch"

brews:
  - name: skill-mgr
    repository:
      owner: idrewlong
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    directory: Formula
    homepage: https://github.com/idrewlong/skill-mgr
    description: "Agent skill manager — list, audit, and uninstall AI coding agent skills"
    license: MIT
    install: |
      bin.install "skill-mgr"
    test: |
      system "#{bin}/skill-mgr", "version"

release:
  github:
    owner: idrewlong
    name: skill-mgr
  draft: false
  prerelease: auto
  name_template: "v{{.Version}}"
```

---

## `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
    tags: ["v*"]
  pull_request:
    branches: [main]

jobs:
  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22"

      - name: Vet
        run: go vet ./...

      - name: Test
        run: go test ./... -v -race

  release:
    name: Release
    runs-on: ubuntu-latest
    needs: test
    if: startsWith(github.ref, 'refs/tags/v')
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22"

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v5
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

---

## `formula/skill-mgr.rb`

```ruby
class SkillMgr < Formula
  desc "Agent skill manager — list, audit, and uninstall AI coding agent skills"
  homepage "https://github.com/idrewlong/skill-mgr"
  version "1.0.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_darwin_arm64.tar.gz"
      sha256 "PLACEHOLDER_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_darwin_amd64.tar.gz"
      sha256 "PLACEHOLDER_AMD64_SHA256"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_linux_arm64.tar.gz"
      sha256 "PLACEHOLDER_LINUX_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_linux_amd64.tar.gz"
      sha256 "PLACEHOLDER_LINUX_AMD64_SHA256"
    end
  end

  def install
    bin.install "skill-mgr"
  end

  test do
    system "#{bin}/skill-mgr", "version"
  end
end
```

---

## `LICENSE`

```
MIT License

Copyright (c) 2026 Andrew Long

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## `README.md`

```markdown
# skill-mgr

**Agent skill manager** — list, audit, and uninstall AI coding agent skills installed via [skills.sh](https://skills.sh) or directly.

Works across all major agents: Claude Code, Cursor, Codex, GitHub Copilot, Cline, Amp, Windsurf.

---

## Install

```bash
# Homebrew (recommended)
brew install idrewlong/tap/skill-mgr

# Go install
go install github.com/idrewlong/skill-mgr/cmd/skill-mgr@latest

# From source
git clone https://github.com/idrewlong/skill-mgr
cd skill-mgr
make install
```

---

## Commands

### `list` — inventory across all agents

```bash
skill-mgr list
skill-mgr list --audit          # include risk score per skill
skill-mgr list --agent cursor   # filter by agent
skill-mgr list --json           # machine-readable output
```

### `audit` — static security analysis

```bash
skill-mgr audit                  # scan all skills
skill-mgr audit gsap-core        # scan one skill
skill-mgr audit --verbose        # show full findings
skill-mgr audit --registry       # also fetch Gen/Socket/Snyk scores
```

Detects:

| Rule | What it catches |
|------|----------------|
| `PI-001` | Prompt injection / instruction override |
| `PI-002` | Instructions to hide or conceal behavior |
| `EX-001` | Credential exfiltration (env secrets + network) |
| `EX-002` | Access to `.ssh`, `.aws`, `.env` files |
| `EX-003` | Hardcoded C2 IP addresses |
| `SH-001` | Curl-pipe-bash (remote code execution) |
| `SH-002` | Fake prerequisite installer |
| `SH-003` | Background process / cron scheduling |
| `SH-004` | Unsafe external binary download |
| `PR-001` | Broad filesystem destruction (rm -rf ~) |
| `PR-002` | Unnecessary sudo / privilege escalation |
| `OB-001` | Base64-encoded payloads (obfuscation) |
| `HK-001` | Claude Code hook abuse |
| `SE-001` | Social engineering language |

Risk levels: `SAFE` → `LOW` → `MEDIUM` → `HIGH` → `CRITICAL`

### `remove` — uninstall with symlink awareness

```bash
skill-mgr remove gsap-core --dry-run   # preview what will be removed
skill-mgr remove gsap-core             # remove (prompts for confirmation)
skill-mgr remove gsap-core --force     # skip prompt
```

For skills installed via `npx skills add` (symlinked universally), `remove` cleans up all agent symlinks automatically — no manual hunting across `~/.claude/skills`, `~/.cursor/skills`, etc.

### `check-updates` — SHA-based update detection

```bash
skill-mgr check-updates
skill-mgr check-updates gsap-core
```

Compares the installed Git SHA against the upstream registry or via `git ls-remote`.

### `info` — everything about one skill

```bash
skill-mgr info frontend-design
```

Runs a full audit and update check and displays all metadata in one view.

---

## As a Claude Code skill

Install the bundled SKILL.md to let Claude Code manage your skills inline:

```bash
cp -r .claude/skills/skill-mgr ~/.claude/skills/
```

Then in Claude Code:

> "Audit my installed skills and flag anything suspicious"  
> "Remove the gsap-core skill"  
> "Are any of my skills out of date?"

---

## Why this exists

Skills run with **full agent permissions** — they can read files, execute shell commands, access your environment variables, and make network calls. The [skills.sh registry](https://skills.sh) scans at publish time, but:

- Skills can be updated after the scan
- ~12% of the ClawHub registry was compromised in the January 2026 ClawHavoc incident
- No tool existed to audit what's already installed across all your agents in one place

`skill-mgr` is the `npm audit` for the agent skill ecosystem.

---

## Contributing

```bash
git clone https://github.com/idrewlong/skill-mgr
cd skill-mgr
make build
make test
```

PRs welcome, especially new threat rules in `internal/audit/audit.go`.

---

## License

MIT — Andrew Long ([@idrewlong](https://github.com/idrewlong))
```

---


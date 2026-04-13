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
	RiskScore     int // 0-100
	RiskLevel     RiskLevel
	Findings      []AuditFinding
	ScannedAt     time.Time
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
	Name          string
	Path          string        // absolute path to skill directory
	IsSymlink     bool
	SymlinkTarget string        // resolved path if symlink
	Frontmatter   SkillFrontmatter
	Agents        []AgentTarget // which agents this is installed for
	InstalledAt   time.Time
	GitSHA        string        // current HEAD SHA of skill dir
	UpstreamSHA   string        // latest SHA from upstream (populated by update-check)
	HasUpdate     bool
	Audit         *AuditResult  // nil until audited
	Source        string        // "universal" | "agent-specific"
}

// UpdateStatus describes the update state of a skill
type UpdateStatus string

const (
	UpdateCurrent   UpdateStatus = "up to date"
	UpdateAvailable UpdateStatus = "update available"
	UpdateUnknown   UpdateStatus = "unknown"
)

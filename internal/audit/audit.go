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

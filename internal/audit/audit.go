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
		Pattern:     regexp.MustCompile(`(?i)(~/\.ssh|~/\.aws|~\/\.env|\/\.env|id_rsa|id_ed25519|\.netrc)`),
		Score:       25,
	},
	{
		// Anchored to network context to avoid firing on semver/version strings
		ID:          "EX-003",
		Severity:    models.RiskHigh,
		Description: "Hardcoded IP address in network context (possible C2 infrastructure)",
		Pattern:     regexp.MustCompile(`(?i)(https?://|curl\s+|wget\s+|nc\s+|netcat\s+|connect to\s+|host:\s*|server:\s*|endpoint:\s*)(\d{1,3}\.){3}\d{1,3}`),
		Score:       15,
	},
	{
		ID:          "EX-004",
		Severity:    models.RiskHigh,
		Description: "Accesses password manager or GPG keystore",
		Pattern:     regexp.MustCompile(`(?i)(~/\.gnupg|~/\.config/(gnupg|pass|gopass)|security\s+find-(generic|internet)-password|keychain\s+(access|unlock)|\bpass\s+(show|get|find)\b|gpg\s+(--export|--decrypt|--armor))`),
		Score:       25,
	},
	{
		ID:          "EX-005",
		Severity:    models.RiskHigh,
		Description: "Accesses git credential store or GitHub CLI token",
		Pattern:     regexp.MustCompile(`(?i)(~/\.gitconfig|~/\.git-credentials|git\s+credential\s+(fill|approve|reject)|gh\s+auth\s+token|GIT_ASKPASS|git\s+config\s+--global\s+credential)`),
		Score:       25,
	},
	{
		// Targets agent-mediated exfil: instructing the model to read a file and leak it through output
		ID:          "EX-006",
		Severity:    models.RiskHigh,
		Description: "Agent-mediated file exfiltration (read and return/send file contents)",
		Pattern:     regexp.MustCompile(`(?i)(read (and )?(return|output|print|send|paste) (the )?(contents?|file)|paste (the )?(full |entire )?(contents?|output) of|include (the )?(full |entire |raw )(contents?|file)|send (the )?(following|contents?|file) to)`),
		Score:       25,
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
		Pattern:     regexp.MustCompile(`(?i)(crontab|launchctl|systemctl|nohup|disown|screen -dm|tmux new-session -d|\s+&\s*$)`),
		Score:       20,
	},
	{
		ID:          "SH-004",
		Severity:    models.RiskMedium,
		Description: "Unsafe download of external binary",
		Pattern:     regexp.MustCompile(`(?i)(curl|wget).{0,80}\.(sh|bash|py|rb|exe|bin)\b`),
		Score:       15,
	},
	{
		// Catches eval $(cmd), eval "$(cmd)", eval '$(cmd)' — classic dynamic RCE
		ID:          "SH-005",
		Severity:    models.RiskCritical,
		Description: "eval with dynamic input (remote code execution vector)",
		Pattern:     regexp.MustCompile(`(?i)\beval\s+["']?\$\(`),
		Score:       40,
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
	{
		// 6+ consecutive hex escapes or 4+ unicode escapes suggests encoded payload
		ID:          "OB-002",
		Severity:    models.RiskHigh,
		Description: "Hex or unicode escape obfuscation (encoded payload)",
		Pattern:     regexp.MustCompile(`(\\x[0-9a-fA-F]{2}){6,}|(\\u[0-9a-fA-F]{4}){4,}`),
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
	{
		ID:          "HK-002",
		Severity:    models.RiskMedium,
		Description: "References other agent auto-run hooks (Cursor, Windsurf, Codex, Cline)",
		Pattern:     regexp.MustCompile(`(?i)(\.cursorrules|windsurf[_-]?hook|codex[_-]?hook|cline[_-]?hook|autorun|auto[_-]?execute|on[_-]?(save|open|start|load)).{0,50}(run|exec|bash|sh|python|node)`),
		Score:       10,
	},

	// Social engineering
	{
		ID:          "SE-001",
		Severity:    models.RiskMedium,
		Description: "Social engineering language (urgency, trust manipulation)",
		Pattern:     regexp.MustCompile(`(?i)(this skill (has been|is) (verified|approved|certified) by|trust (this|the following)|you (can|should) trust|(i am|this is) (the )?official (anthropic|openai|cursor|github))`),
		Score:       15,
	},
	{
		ID:          "SE-002",
		Severity:    models.RiskMedium,
		Description: "Urgency or mandatory-action language (pressure tactics)",
		Pattern:     regexp.MustCompile(`(?i)(you must (run|execute|install|do) this (now|immediately|first)|do not (skip|ignore) this (step|instruction|section)|required for (this skill|the skill) to (work|function|operate)|must be (run|executed) before (anything|all|every)|this (step|instruction) is (critical|required|mandatory) and must)`),
		Score:       15,
	},
}

// AuditSkill performs static analysis on a skill and returns an AuditResult.
// Findings suppressed by a .skill-mgr-ignore file or inline skill-mgr:ignore
// comments are excluded from the result.
func AuditSkill(skill *models.Skill) (*models.AuditResult, error) {
	result := &models.AuditResult{
		ScannedAt: time.Now(),
		RiskLevel: models.RiskSafe,
	}

	allowlist := loadAllowlist(skill.Path)

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
		findings, err := scanFile(path, allowlist)
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

// scanFile runs all rules against a single file, returning findings.
// allowlist is a set of rule IDs to suppress for the whole file;
// individual lines can also carry inline skill-mgr:ignore directives.
func scanFile(path string, allowlist map[string]bool) ([]models.AuditFinding, error) {
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

		suppressAll, inlineRules := inlineIgnore(line)

		for _, rule := range rules {
			// Skip if suppressed by file-level allowlist
			if allowlist[rule.ID] {
				continue
			}
			// Skip if suppressed by inline directive
			if suppressAll {
				continue
			}
			if inlineRules[rule.ID] {
				continue
			}

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

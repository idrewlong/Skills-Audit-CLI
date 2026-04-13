package ui

import (
	"fmt"
	"sort"
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

// PromptString prints a prompt with an optional default and returns the user's input.
// If the user presses enter without typing, the default is returned.
func PromptString(prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s%s%s]: ", prompt, Gray, defaultVal, Reset)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	var resp string
	fmt.Scanln(&resp)
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return defaultVal
	}
	return resp
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

// riskOrdinal maps a RiskLevel to a sortable integer (higher = worse).
func riskOrdinal(level models.RiskLevel) int {
	switch level {
	case models.RiskSafe:
		return 0
	case models.RiskLow:
		return 1
	case models.RiskMedium:
		return 2
	case models.RiskHigh:
		return 3
	case models.RiskCritical:
		return 4
	default:
		return -1
	}
}

// ParseRiskLevel converts a string like "medium" to a RiskLevel.
// Returns RiskUnknown if the string is unrecognised.
func ParseRiskLevel(s string) models.RiskLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "safe":
		return models.RiskSafe
	case "low":
		return models.RiskLow
	case "medium":
		return models.RiskMedium
	case "high":
		return models.RiskHigh
	case "critical":
		return models.RiskCritical
	default:
		return models.RiskUnknown
	}
}

// MeetsThreshold reports whether actual is at or above threshold severity.
func MeetsThreshold(actual, threshold models.RiskLevel) bool {
	return riskOrdinal(actual) >= riskOrdinal(threshold)
}

// SortSkills sorts skills in-place by the given field.
// Valid values: "name" (default), "date" (newest first),
// "risk" (highest first, requires Audit populated), "update" (updates first).
func SortSkills(skills []*models.Skill, by string) {
	switch strings.ToLower(by) {
	case "date":
		sort.SliceStable(skills, func(i, j int) bool {
			return skills[i].InstalledAt.After(skills[j].InstalledAt)
		})
	case "risk":
		sort.SliceStable(skills, func(i, j int) bool {
			oi, oj := -1, -1
			if skills[i].Audit != nil {
				oi = riskOrdinal(skills[i].Audit.RiskLevel)
			}
			if skills[j].Audit != nil {
				oj = riskOrdinal(skills[j].Audit.RiskLevel)
			}
			return oi > oj
		})
	case "update":
		sort.SliceStable(skills, func(i, j int) bool {
			if skills[i].HasUpdate != skills[j].HasUpdate {
				return skills[i].HasUpdate
			}
			return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
		})
	default: // "name"
		sort.SliceStable(skills, func(i, j int) bool {
			return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
		})
	}
}

// FilterByRisk returns only skills whose audit risk level meets minLevel.
// Skills that have not been audited are excluded when a level is set.
func FilterByRisk(skills []*models.Skill, minLevel models.RiskLevel) []*models.Skill {
	if minLevel == models.RiskUnknown || minLevel == "" {
		return skills
	}
	out := skills[:0:0]
	for _, s := range skills {
		if s.Audit != nil && MeetsThreshold(s.Audit.RiskLevel, minLevel) {
			out = append(out, s)
		}
	}
	return out
}

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/idrewlong/skill-mgr/internal/audit"
	"github.com/idrewlong/skill-mgr/internal/discovery"
	"github.com/idrewlong/skill-mgr/internal/registry"
	"github.com/idrewlong/skill-mgr/internal/remove"
	"github.com/idrewlong/skill-mgr/internal/update"
	"github.com/idrewlong/skill-mgr/pkg/models"
	"github.com/idrewlong/skill-mgr/pkg/ui"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	colorCyan   = lipgloss.Color("#00D4FF")
	colorGreen  = lipgloss.Color("#00FF88")
	colorYellow = lipgloss.Color("#FFD93D")
	colorRed    = lipgloss.Color("#FF6B6B")
	colorGray   = lipgloss.Color("#888888")
	colorWhite  = lipgloss.Color("#EEEEEE")

	diamondStyle = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	barStyle     = lipgloss.NewStyle().Foreground(colorCyan)
	badgeStyle   = lipgloss.NewStyle().
			Background(colorCyan).
			Foreground(lipgloss.Color("#000000")).
			Padding(0, 1).
			Bold(true)
	labelStyle   = lipgloss.NewStyle().Foreground(colorGray)
	valueStyle   = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(colorRed).Bold(true)

	summaryBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			Padding(0, 2).
			MarginTop(1).
			MarginBottom(1)
)

// ── Logo ──────────────────────────────────────────────────────────────────────

func printLogo() {
	cyan := lipgloss.NewStyle().Foreground(colorCyan)
	gray := lipgloss.NewStyle().Foreground(colorGray)

	logo := []string{
		"███████╗██╗  ██╗██╗██╗     ██╗      ███╗   ███╗ ██████╗ ██████╗ ",
		"██╔════╝██║ ██╔╝██║██║     ██║      ████╗ ████║██╔════╝ ██╔══██╗",
		"███████╗█████╔╝ ██║██║     ██║      ██╔████╔██║██║  ███╗██████╔╝",
		"╚════██║██╔═██╗ ██║██║     ██║      ██║╚██╔╝██║██║   ██║██╔══██╗",
		"███████║██║  ██╗██║███████╗███████╗ ██║ ╚═╝ ██║╚██████╔╝██║  ██║",
		"╚══════╝╚═╝  ╚═╝╚═╝╚══════╝╚══════╝ ╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝",
	}

	fmt.Println()
	for _, line := range logo {
		fmt.Println(cyan.Render("  " + line))
	}
	fmt.Printf("  %s\n\n", gray.Render("agent skill manager  •  v"+version))
}

// ── Step helpers ──────────────────────────────────────────────────────────────

func step(msg string) {
	fmt.Printf("%s %s %s\n",
		barStyle.Render("│"),
		diamondStyle.Render("◆"),
		msg,
	)
}

func stepDone(msg string) {
	fmt.Printf("%s %s %s\n",
		barStyle.Render("│"),
		successStyle.Render("✓"),
		msg,
	)
}

func stepWarn(msg string) {
	fmt.Printf("%s %s %s\n",
		barStyle.Render("│"),
		warnStyle.Render("⚠"),
		msg,
	)
}

func stepError(msg string) {
	fmt.Printf("%s %s %s\n",
		barStyle.Render("│"),
		errorStyle.Render("✗"),
		msg,
	)
}

func bar() {
	fmt.Println(barStyle.Render("│"))
}

func wizardKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	return km
}

// ── Wizard entry ──────────────────────────────────────────────────────────────

func runWizard() {
	printLogo()

	fmt.Printf("  %s\n\n", badgeStyle.Render("skill-mgr"))

	// Discover skills
	bar()
	step(labelStyle.Render("Discovering skills..."))

	skills, err := discovery.Discover()
	if err != nil {
		stepError(fmt.Sprintf("Discovery failed: %v", err))
		os.Exit(1)
	}

	if len(skills) == 0 {
		stepWarn("No skills found. Install skills with: " + valueStyle.Render("npx skills add <skill>"))
		bar()
		return
	}

	stepDone(fmt.Sprintf("Found %s skills", valueStyle.Render(fmt.Sprintf("%d", len(skills)))))
	bar()

	// Action select
	var action string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Description("Use arrow keys to navigate, Enter to select.").
				Options(
					huh.NewOption("Audit skills  (security scan)", "audit"),
					huh.NewOption("List skills", "list"),
					huh.NewOption("Remove a skill", "remove"),
					huh.NewOption("Check for updates", "updates"),
					huh.NewOption("Skill info", "info"),
				).
				Value(&action),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	if err := form.Run(); err != nil {
		fmt.Println(labelStyle.Render("\n  Cancelled."))
		return
	}

	bar()

	switch action {
	case "audit":
		wizardAudit(skills)
	case "list":
		wizardList(skills)
	case "remove":
		wizardRemove(skills)
	case "updates":
		wizardUpdates(skills)
	case "info":
		wizardInfo(skills)
	}
}

// ── Audit wizard ──────────────────────────────────────────────────────────────

func wizardAudit(skills []*models.Skill) {
	// Pick scope
	var scope string
	scopeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Audit scope").
				Options(
					huh.NewOption(fmt.Sprintf("All %d skills", len(skills)), "all"),
					huh.NewOption("Pick a specific skill", "one"),
				).
				Value(&scope),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	if err := scopeForm.Run(); err != nil {
		fmt.Println(labelStyle.Render("\n  Cancelled."))
		return
	}

	bar()

	if scope == "one" {
		opts := skillOptions(skills)
		var pick string
		pickForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select skill to audit").
					Options(opts...).
					Value(&pick),
			),
		).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

		if err := pickForm.Run(); err != nil {
			fmt.Println(labelStyle.Render("\n  Cancelled."))
			return
		}
		bar()

		s := discovery.FindByName(skills, pick)
		if s == nil {
			stepError(fmt.Sprintf("Skill %q not found", pick))
			return
		}
		skills = []*models.Skill{s}
	}

	// Registry flag
	var fetchRegistry bool
	regForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Also fetch scores from skills.sh registry?").
				Description("Gen / Socket / Snyk scores. Requires internet.").
				Affirmative("Yes").
				Negative("No").
				Value(&fetchRegistry),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())
	regForm.Run()

	bar()
	step(fmt.Sprintf("Auditing %s...", valueStyle.Render(fmt.Sprintf("%d skill(s)", len(skills)))))
	fmt.Println()

	exitCode := 0
	for _, skill := range skills {
		result, err := audit.AuditSkill(skill)
		skill.Audit = result

		if err != nil {
			stepError(fmt.Sprintf("%s: scan failed", skill.Name))
			continue
		}

		if fetchRegistry && skill.Frontmatter.Repository != "" {
			rs, err := registry.FetchScore(skill.Frontmatter.Repository)
			if err == nil {
				result.RegistryScore = rs
			}
		}

		riskColor := riskLipgloss(result.RiskLevel)
		icon := riskIconStr(result.RiskLevel)

		fmt.Printf("  %s %s  %s\n",
			riskColor.Render(icon),
			valueStyle.Render(fmt.Sprintf("%-24s", skill.Name)),
			riskColor.Render(fmt.Sprintf("%s  score: %d/100  %d finding(s)",
				string(result.RiskLevel), result.RiskScore, len(result.Findings))),
		)

		for _, f := range result.Findings {
			fc := riskLipgloss(f.Severity)
			fmt.Printf("     %s %s\n", fc.Render(fmt.Sprintf("[%s]", f.Rule)), f.Description)
			fmt.Printf("       %s\n", labelStyle.Render(fmt.Sprintf("%s:%d", f.File, f.Line)))
			if f.Evidence != "" {
				fmt.Printf("       %s %s\n", labelStyle.Render("→"), lipgloss.NewStyle().Faint(true).Render(f.Evidence))
			}
		}

		if result.RiskLevel == models.RiskHigh || result.RiskLevel == models.RiskCritical {
			exitCode = 1
		}
	}

	fmt.Println()
	printAuditSummaryLipgloss(skills)
	bar()

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// ── List wizard ───────────────────────────────────────────────────────────────

func wizardList(skills []*models.Skill) {
	var agentFilter string
	filterForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Filter by agent?").
				Options(
					huh.NewOption("All agents", ""),
					huh.NewOption("claude-code", "claude-code"),
					huh.NewOption("cursor", "cursor"),
					huh.NewOption("codex", "codex"),
					huh.NewOption("github-copilot", "github-copilot"),
					huh.NewOption("cline", "cline"),
					huh.NewOption("amp", "amp"),
					huh.NewOption("windsurf", "windsurf"),
					huh.NewOption("universal", "universal"),
				).
				Value(&agentFilter),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	filterForm.Run()
	bar()

	filtered := skills
	if agentFilter != "" {
		filtered = filterByAgent(skills, agentFilter)
	}

	step(fmt.Sprintf("Showing %s", valueStyle.Render(fmt.Sprintf("%d skills", len(filtered)))))
	fmt.Println()
	ui.PrintSkillTable(filtered, false)
	bar()
}

// ── Remove wizard ─────────────────────────────────────────────────────────────

func wizardRemove(skills []*models.Skill) {
	opts := skillOptions(skills)
	var pick string
	pickForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select skill to remove").
				Options(opts...).
				Value(&pick),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	if err := pickForm.Run(); err != nil {
		fmt.Println(labelStyle.Render("\n  Cancelled."))
		return
	}
	bar()

	skill := discovery.FindByName(skills, pick)
	if skill == nil {
		stepError(fmt.Sprintf("Skill %q not found", pick))
		return
	}

	// Dry run preview
	step("Previewing removal...")
	dryResult, _ := remove.Remove(skill, true)
	fmt.Println()
	for _, p := range dryResult.RemovedPaths {
		fmt.Printf("  %s %s\n", warnStyle.Render("×"), labelStyle.Render(p))
	}
	fmt.Println()

	// Confirm
	var confirmed bool
	msg := fmt.Sprintf("Remove %s?", valueStyle.Render(skill.Name))
	if skill.IsSymlink {
		msg += warnStyle.Render("  (removes all agent symlinks)")
	}
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(msg).
				Affirmative("Remove").
				Negative("Cancel").
				Value(&confirmed),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	if err := confirmForm.Run(); err != nil || !confirmed {
		bar()
		stepWarn("Cancelled — no changes made.")
		bar()
		return
	}

	bar()
	result, err := remove.Remove(skill, false)
	if err != nil {
		stepError(err.Error())
		os.Exit(1)
	}

	for _, p := range result.RemovedPaths {
		stepDone(fmt.Sprintf("Removed %s", labelStyle.Render(p)))
	}
	for _, e := range result.Errors {
		stepError(e)
	}
	bar()
}

// ── Updates wizard ────────────────────────────────────────────────────────────

func wizardUpdates(skills []*models.Skill) {
	var scope string
	scopeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Check updates for").
				Options(
					huh.NewOption(fmt.Sprintf("All %d skills", len(skills)), "all"),
					huh.NewOption("Pick a specific skill", "one"),
				).
				Value(&scope),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	if err := scopeForm.Run(); err != nil {
		fmt.Println(labelStyle.Render("\n  Cancelled."))
		return
	}
	bar()

	if scope == "one" {
		opts := skillOptions(skills)
		var pick string
		pickForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select skill").
					Options(opts...).
					Value(&pick),
			),
		).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

		if err := pickForm.Run(); err != nil {
			fmt.Println(labelStyle.Render("\n  Cancelled."))
			return
		}
		bar()

		s := discovery.FindByName(skills, pick)
		if s != nil {
			skills = []*models.Skill{s}
		}
	}

	step(fmt.Sprintf("Checking %s for updates...", valueStyle.Render(fmt.Sprintf("%d skill(s)", len(skills)))))
	fmt.Println()

	results := update.CheckAll(skills)
	updatesAvailable := 0

	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("  %s %-24s %s\n",
				labelStyle.Render("—"),
				r.Skill.Name,
				labelStyle.Render(r.Err.Error()),
			)
			continue
		}
		if r.HasUpdate {
			fmt.Printf("  %s %-24s %s\n",
				warnStyle.Render("↑"),
				valueStyle.Render(r.Skill.Name),
				warnStyle.Render("update available  "+r.UpstreamSHA),
			)
			updatesAvailable++
		} else {
			fmt.Printf("  %s %-24s %s\n",
				successStyle.Render("✓"),
				r.Skill.Name,
				labelStyle.Render("current"),
			)
		}
	}

	fmt.Println()
	if updatesAvailable > 0 {
		stepWarn(fmt.Sprintf("%d update(s) available — run: %s", updatesAvailable, valueStyle.Render("npx skills update")))
	} else {
		stepDone("All skills are up to date")
	}
	bar()
}

// ── Info wizard ───────────────────────────────────────────────────────────────

func wizardInfo(skills []*models.Skill) {
	opts := skillOptions(skills)
	var pick string
	pickForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select skill").
				Options(opts...).
				Value(&pick),
		),
	).WithTheme(wizardTheme()).WithKeyMap(wizardKeyMap())

	if err := pickForm.Run(); err != nil {
		fmt.Println(labelStyle.Render("\n  Cancelled."))
		return
	}
	bar()

	skill := discovery.FindByName(skills, pick)
	if skill == nil {
		stepError(fmt.Sprintf("Skill %q not found", pick))
		return
	}

	step(fmt.Sprintf("Auditing %s...", valueStyle.Render(skill.Name)))
	result, _ := audit.AuditSkill(skill)
	skill.Audit = result

	step("Checking for updates...")
	hasUpdate, upstreamSHA, _ := update.CheckUpdate(skill)
	skill.HasUpdate = hasUpdate
	skill.UpstreamSHA = upstreamSHA

	fmt.Println()
	ui.PrintSkillDetail(skill)
	bar()
}

// ── Theme ─────────────────────────────────────────────────────────────────────

func wizardTheme() *huh.Theme {
	t := huh.ThemeDracula()
	t.Focused.Base = t.Focused.Base.BorderForeground(colorCyan)
	t.Focused.Title = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colorWhite)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colorCyan).SetString("▸ ")
	t.Focused.Description = lipgloss.NewStyle().Foreground(colorGray)
	return t
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func skillOptions(skills []*models.Skill) []huh.Option[string] {
	opts := make([]huh.Option[string], len(skills))
	for i, s := range skills {
		label := s.Name
		if s.Frontmatter.Description != "" {
			desc := s.Frontmatter.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			label = fmt.Sprintf("%-24s %s", s.Name, labelStyle.Render("— "+desc))
		}
		opts[i] = huh.NewOption(label, s.Name)
	}
	return opts
}

func riskLipgloss(level models.RiskLevel) lipgloss.Style {
	switch level {
	case models.RiskSafe:
		return lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	case models.RiskLow:
		return lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	case models.RiskMedium:
		return lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	case models.RiskHigh:
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	case models.RiskCritical:
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true).Blink(true)
	default:
		return lipgloss.NewStyle().Foreground(colorGray)
	}
}

func riskIconStr(level models.RiskLevel) string {
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

func printAuditSummaryLipgloss(skills []*models.Skill) {
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

	var lines []string
	lines = append(lines, valueStyle.Render(fmt.Sprintf("Audit Summary  (%d scanned)", audited)))
	for _, level := range []models.RiskLevel{
		models.RiskCritical, models.RiskHigh, models.RiskMedium,
		models.RiskLow, models.RiskSafe,
	} {
		if n := counts[level]; n > 0 {
			lines = append(lines, fmt.Sprintf("  %s  %d %s",
				riskLipgloss(level).Render(riskIconStr(level)),
				n,
				riskLipgloss(level).Render(string(level)),
			))
		}
	}
	fmt.Println(summaryBox.Render(strings.Join(lines, "\n")))
}

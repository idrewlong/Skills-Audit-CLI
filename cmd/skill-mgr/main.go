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
	"github.com/idrewlong/skill-mgr/internal/report"
	"github.com/idrewlong/skill-mgr/internal/update"
	"github.com/idrewlong/skill-mgr/pkg/models"
	"github.com/idrewlong/skill-mgr/pkg/ui"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		runWizard()
		return
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
	case "update":
		cmdUpdate(args)
	case "info":
		cmdInfo(args)
	case "report":
		cmdReport(args)
	case "diff":
		cmdDiff(args)
	case "completion":
		cmdCompletion(args)
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
	sortBy := fs.String("sort", "name", "sort by: name, date, risk, update")
	filterRisk := fs.String("filter-risk", "", "show only skills at/above risk level (requires --audit)")
	project := fs.String("project", "", "also scan project-local skills at this path (default: ./.claude/skills)")
	fs.Parse(args)

	ui.Header()

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	// Project-local scan
	if project != nil && fs.Lookup("project").Value.String() != "" {
		projectPath := *project
		if projectPath == "" {
			projectPath = ".claude/skills"
		}
		extra, _ := discovery.ScanDirectory(projectPath)
		skills = append(skills, extra...)
	}

	if len(skills) == 0 {
		fmt.Println("No skills found. Install skills with: npx skills add <skill>")
		return
	}

	if *agent != "" {
		skills = filterByAgent(skills, *agent)
	}

	if *auditFlag {
		stop2 := ui.Spinner(fmt.Sprintf("Auditing %d skills...", len(skills)))
		audit.AuditAll(skills)
		stop2()
	}

	if *filterRisk != "" {
		level := ui.ParseRiskLevel(*filterRisk)
		if level == models.RiskUnknown {
			ui.PrintError(fmt.Sprintf("unknown risk level %q — use: safe, low, medium, high, critical", *filterRisk))
			os.Exit(1)
		}
		skills = ui.FilterByRisk(skills, level)
	}

	ui.SortSkills(skills, *sortBy)

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
	verbose := fs.Bool("verbose", true, "show all findings in detail")
	failOn := fs.String("fail-on", "high", "exit 1 when any skill reaches this risk level (safe/low/medium/high/critical)")
	format := fs.String("format", "text", "output format: text, sarif")
	project := fs.String("project", "", "also scan project-local skills at this path")
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

	if *project != "" {
		extra, _ := discovery.ScanDirectory(*project)
		skills = append(skills, extra...)
	}

	if *skillName != "" {
		skill := discovery.FindByName(skills, *skillName)
		if skill == nil {
			ui.PrintError(fmt.Sprintf("skill %q not found", *skillName))
			os.Exit(1)
		}
		skills = []*models.Skill{skill}
	}

	threshold := ui.ParseRiskLevel(*failOn)
	if threshold == models.RiskUnknown {
		ui.PrintError(fmt.Sprintf("unknown --fail-on level %q — use: safe, low, medium, high, critical", *failOn))
		os.Exit(1)
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

		if *format != "sarif" {
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
		}

		if ui.MeetsThreshold(result.RiskLevel, threshold) && result.RiskLevel != models.RiskSafe {
			exitCode = 1
		}
	}

	if *format == "sarif" {
		if err := audit.WriteSARIF(skills, version, os.Stdout); err != nil {
			ui.PrintError(fmt.Sprintf("SARIF output failed: %v", err))
			os.Exit(1)
		}
	} else {
		fmt.Println()
		printAuditSummary(skills)
		promptAndExport(skills)
	}

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
		if *force || *dryRun {
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
		ui.PrintWarn(fmt.Sprintf("%d update(s) available. Run: skill-mgr update", updatesAvailable))
	} else {
		ui.PrintSuccess("All skills are up to date")
	}
	fmt.Println()
}

// ─── update ──────────────────────────────────────────────────────────────────

func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	skillName := fs.String("skill", "", "update a specific skill by name")
	all := fs.Bool("all", false, "update all skills that have updates available")
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
		// Mark it for update regardless of cached HasUpdate
		skill.HasUpdate = true
		skills = []*models.Skill{skill}
	} else {
		// Check for updates first so we know what to pull
		stop2 := ui.Spinner(fmt.Sprintf("Checking %d skill(s) for updates...", len(skills)))
		update.CheckAll(skills)
		stop2()

		if !*all {
			// Filter to only those with updates
			var pending []*models.Skill
			for _, s := range skills {
				if s.HasUpdate {
					pending = append(pending, s)
				}
			}
			skills = pending
		}
	}

	if len(skills) == 0 {
		ui.PrintSuccess("All skills are already up to date")
		return
	}

	fmt.Printf("  Updating %s%d skill(s)%s...\n\n", ui.Bold, len(skills), ui.Reset)

	results := update.ApplyAll(skills)
	errCount := 0
	for _, r := range results {
		if r.Err != nil {
			ui.PrintError(fmt.Sprintf("%-24s %v", r.Skill.Name, r.Err))
			errCount++
			continue
		}
		if r.Updated {
			ui.PrintSuccess(fmt.Sprintf("%-24s updated", r.Skill.Name))
		} else {
			fmt.Printf("  %s%-24s%s already up to date\n", ui.Gray, r.Skill.Name, ui.Reset)
		}
	}

	fmt.Println()
	if errCount > 0 {
		os.Exit(1)
	}
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

	result, _ := audit.AuditSkill(skill)
	skill.Audit = result

	hasUpdate, upstreamSHA, _ := update.CheckUpdate(skill)
	skill.HasUpdate = hasUpdate
	skill.UpstreamSHA = upstreamSHA

	ui.PrintSkillDetail(skill)
}

// ─── report ──────────────────────────────────────────────────────────────────

func cmdReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	output := fs.String("output", "", "write report to file (default: stdout)")
	runAudit := fs.Bool("audit", false, "run security audit before generating report")
	checkUpd := fs.Bool("check-updates", false, "check for updates before generating report")
	project := fs.String("project", "", "also scan project-local skills at this path")
	fs.Parse(args)

	ui.Header()

	stop := ui.Spinner("Discovering skills...")
	skills, err := discovery.Discover()
	stop()

	if err != nil {
		ui.PrintError(fmt.Sprintf("discovery failed: %v", err))
		os.Exit(1)
	}

	if *project != "" {
		extra, _ := discovery.ScanDirectory(*project)
		skills = append(skills, extra...)
	}

	if *runAudit {
		stop2 := ui.Spinner(fmt.Sprintf("Auditing %d skills...", len(skills)))
		audit.AuditAll(skills)
		stop2()
	}

	if *checkUpd {
		stop3 := ui.Spinner(fmt.Sprintf("Checking %d skills for updates...", len(skills)))
		update.CheckAll(skills)
		stop3()
	}

	ui.SortSkills(skills, "name")

	w := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			ui.PrintError(fmt.Sprintf("could not create output file: %v", err))
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	if err := report.WriteMarkdown(skills, w); err != nil {
		ui.PrintError(fmt.Sprintf("report failed: %v", err))
		os.Exit(1)
	}

	if *output != "" {
		ui.PrintSuccess(fmt.Sprintf("Report written to %s", *output))
	}
}

// ─── diff ────────────────────────────────────────────────────────────────────

func cmdDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	skillName := fs.String("skill", "", "diff a specific skill by name")
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

	var targets []*models.Skill
	if *skillName != "" {
		skill := discovery.FindByName(skills, *skillName)
		if skill == nil {
			ui.PrintError(fmt.Sprintf("skill %q not found", *skillName))
			os.Exit(1)
		}
		targets = []*models.Skill{skill}
	} else {
		targets = skills
	}

	anyDiff := false
	for _, skill := range targets {
		stop2 := ui.Spinner(fmt.Sprintf("Fetching upstream for %s...", skill.Name))
		result := update.DiffSkill(skill)
		stop2()

		if result.Err != nil {
			fmt.Printf("  %s%-24s%s %s— %v%s\n",
				ui.Bold, skill.Name, ui.Reset, ui.Gray, result.Err, ui.Reset)
			continue
		}
		if !result.HasDiff {
			fmt.Printf("  %s✓%s %-24s up to date\n", ui.Green, ui.Reset, skill.Name)
			continue
		}

		anyDiff = true
		fmt.Printf("\n  %s%s%s\n", ui.Bold+ui.Cyan, skill.Name, ui.Reset)
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println(result.Diff)
	}

	if anyDiff {
		fmt.Println()
		ui.PrintWarn("Diffs found — run `skill-mgr update` to apply")
	}
	fmt.Println()
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func promptAndExport(skills []*models.Skill) {
	if !ui.Confirm("  Export results?") {
		return
	}

	format := ui.PromptString("  Format (csv, markdown, json)", "csv")
	switch strings.ToLower(format) {
	case "csv", "markdown", "md", "json":
	default:
		ui.PrintError(fmt.Sprintf("unknown format %q — use: csv, markdown, json", format))
		return
	}

	ext := strings.ToLower(format)
	if ext == "markdown" {
		ext = "md"
	}
	filename := ui.PromptString("  Output file", "audit-results."+ext)

	f, err := os.Create(filename)
	if err != nil {
		ui.PrintError(fmt.Sprintf("could not create file: %v", err))
		return
	}
	defer f.Close()

	switch strings.ToLower(format) {
	case "csv":
		err = report.WriteCSV(skills, f)
	case "markdown", "md":
		err = report.WriteMarkdown(skills, f)
	case "json":
		err = report.WriteJSON(skills, f)
	}

	if err != nil {
		ui.PrintError(fmt.Sprintf("export failed: %v", err))
		return
	}
	ui.PrintSuccess(fmt.Sprintf("Results exported to %s", filename))
}

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
    --sort <field>   Sort by: name (default), date, risk, update
    --filter-risk    Show only skills at/above level (requires --audit)
    --project <path> Also scan project-local skills

  audit [skill]      Security scan all skills (or one by name)
    --verbose        Show full findings detail (default: on)
    --registry       Also fetch scores from skills.sh registry
    --fail-on <lvl>  Exit 1 at this risk level (default: high)
    --format <fmt>   Output format: text (default), sarif
    --project <path> Also scan project-local skills

  remove <name>      Uninstall a skill (handles symlinks across agents)
    --dry-run        Preview what would be removed
    --force          Skip confirmation prompt

  check-updates      Check all skills for available updates
  check-updates <n>  Check a single skill by name

  update             Apply updates to all skills with updates available
  update <name>      Update a specific skill
    --all            Include skills even if no update detected

  info <name>        Full detail for a skill (audit + update check)

  report             Generate a Markdown report of all skills
    --output <file>  Write to file instead of stdout
    --audit          Run security audit before report
    --check-updates  Check updates before report
    --project <path> Also include project-local skills

  diff [skill]       Show unified diff of local vs upstream
    --skill <name>   Diff a specific skill

  completion <shell> Output shell completion script (bash, zsh, fish)

  version            Print version
  help               Show this help

%sEXAMPLES%s
  skill-mgr list
  skill-mgr list --audit --sort risk
  skill-mgr list --audit --filter-risk medium
  skill-mgr audit --verbose --fail-on medium
  skill-mgr audit gsap-core --format sarif > results.sarif
  skill-mgr remove gsap-core --dry-run
  skill-mgr update
  skill-mgr update gsap-core
  skill-mgr diff
  skill-mgr report --audit --check-updates --output report.md
  skill-mgr completion zsh >> ~/.zshrc

%sINSTALL%s
  go install github.com/idrewlong/skill-mgr/cmd/skill-mgr@latest
  brew install idrewlong/tap/skill-mgr

`, ui.Bold+ui.Cyan, ui.Reset, version,
		ui.Bold, ui.Reset,
		ui.Bold, ui.Reset,
		ui.Bold, ui.Reset,
		ui.Bold, ui.Reset)
}

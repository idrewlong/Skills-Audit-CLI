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
	verbose := fs.Bool("verbose", true, "show all findings in detail")
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

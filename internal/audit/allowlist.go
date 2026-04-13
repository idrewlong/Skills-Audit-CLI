package audit

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadAllowlist reads a .skill-mgr-ignore file from skillPath.
// Each non-blank, non-comment line is a rule ID to suppress (e.g. "EX-003").
// Inline comments after a # are stripped.
func loadAllowlist(skillPath string) map[string]bool {
	allowed := map[string]bool{}
	f, err := os.Open(filepath.Join(skillPath, ".skill-mgr-ignore"))
	if err != nil {
		return allowed
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			allowed[strings.ToUpper(line)] = true
		}
	}
	return allowed
}

// inlineIgnore checks a source line for a skill-mgr:ignore directive.
//
//	// skill-mgr:ignore           → suppress all rules on this line
//	// skill-mgr:ignore EX-003    → suppress only EX-003 on this line
//	<!-- skill-mgr:ignore PI-001 PI-002 --> → multiple rules
//
// Returns (suppressAll, specificRuleIDs).
func inlineIgnore(line string) (bool, map[string]bool) {
	const marker = "skill-mgr:ignore"
	idx := strings.Index(strings.ToLower(line), marker)
	if idx < 0 {
		return false, nil
	}
	rest := strings.TrimSpace(line[idx+len(marker):])
	// Strip trailing comment closers (e.g. -->)
	rest = strings.TrimSuffix(strings.TrimSpace(rest), "-->")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return true, nil
	}
	rules := map[string]bool{}
	for _, p := range strings.Fields(rest) {
		rules[strings.ToUpper(p)] = true
	}
	return false, rules
}

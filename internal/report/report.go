package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// WriteMarkdown generates a Markdown report of all skills and their audit /
// update state, writing it to w.
func WriteMarkdown(skills []*models.Skill, w io.Writer) error {
	now := time.Now().Format("2006-01-02 15:04")
	fmt.Fprintf(w, "# Skill Manager Report\n\n")
	fmt.Fprintf(w, "_Generated: %s — %d skill(s)_\n\n", now, len(skills))

	// ── Summary table ────────────────────────────────────────────────────────
	fmt.Fprintf(w, "## Summary\n\n")
	fmt.Fprintf(w, "| Name | Agents | SHA | Risk | Updates |\n")
	fmt.Fprintf(w, "|------|--------|-----|------|---------|\n")
	for _, s := range skills {
		sha := s.GitSHA
		if sha == "" {
			sha = "—"
		}
		risk := "—"
		if s.Audit != nil {
			risk = string(s.Audit.RiskLevel)
		}
		upd := "current"
		if s.HasUpdate {
			upd = "⬆ available"
		} else if s.UpstreamSHA == "" {
			upd = "—"
		}
		fmt.Fprintf(w, "| %s | %s | `%s` | %s | %s |\n",
			s.Name, agentStr(s.Agents), sha, risk, upd)
	}
	fmt.Fprintln(w)

	// ── Per-skill detail ─────────────────────────────────────────────────────
	fmt.Fprintf(w, "## Skills\n\n")
	for _, s := range skills {
		fmt.Fprintf(w, "### %s\n\n", s.Name)
		if s.Frontmatter.Description != "" {
			fmt.Fprintf(w, "%s\n\n", s.Frontmatter.Description)
		}

		// Metadata table
		fmt.Fprintf(w, "| Field | Value |\n|-------|-------|\n")
		if s.Frontmatter.Author != "" {
			fmt.Fprintf(w, "| Author | %s |\n", s.Frontmatter.Author)
		}
		if s.Frontmatter.Repository != "" {
			fmt.Fprintf(w, "| Repository | %s |\n", s.Frontmatter.Repository)
		}
		if s.Frontmatter.Version != "" {
			fmt.Fprintf(w, "| Version | %s |\n", s.Frontmatter.Version)
		}
		fmt.Fprintf(w, "| Path | `%s` |\n", s.Path)
		fmt.Fprintf(w, "| Agents | %s |\n", agentStr(s.Agents))
		if s.GitSHA != "" {
			fmt.Fprintf(w, "| SHA | `%s` |\n", s.GitSHA)
		}
		if !s.InstalledAt.IsZero() {
			fmt.Fprintf(w, "| Installed | %s |\n", s.InstalledAt.Format("2006-01-02"))
		}
		fmt.Fprintln(w)

		// Security section
		if s.Audit != nil {
			fmt.Fprintf(w, "**Security:** %s (score: %d/100, %d finding(s))\n\n",
				s.Audit.RiskLevel, s.Audit.RiskScore, len(s.Audit.Findings))
			if len(s.Audit.Findings) > 0 {
				fmt.Fprintf(w, "| Severity | Rule | Description | File | Line |\n")
				fmt.Fprintf(w, "|----------|------|-------------|------|------|\n")
				for _, f := range s.Audit.Findings {
					fmt.Fprintf(w, "| %s | `%s` | %s | `%s` | %d |\n",
						f.Severity, f.Rule, f.Description, f.File, f.Line)
				}
				fmt.Fprintln(w)
			}
		}

		if s.HasUpdate {
			fmt.Fprintf(w, "> **Update available** — upstream: `%s`\n\n", s.UpstreamSHA)
		}
	}

	return nil
}

// WriteCSV writes a flat CSV of all skills and their audit results.
func WriteCSV(skills []*models.Skill, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"Name", "Path", "Agents", "RiskLevel", "RiskScore", "Findings", "Rules",
	}); err != nil {
		return err
	}
	for _, s := range skills {
		risk, score, findingsCount, rules := "—", "—", "0", ""
		if s.Audit != nil {
			risk = string(s.Audit.RiskLevel)
			score = fmt.Sprintf("%d", s.Audit.RiskScore)
			findingsCount = fmt.Sprintf("%d", len(s.Audit.Findings))
			ruleIDs := make([]string, len(s.Audit.Findings))
			for i, f := range s.Audit.Findings {
				ruleIDs[i] = f.Rule
			}
			rules = strings.Join(ruleIDs, ";")
		}
		if err := cw.Write([]string{
			s.Name, s.Path, agentStr(s.Agents), risk, score, findingsCount, rules,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// skillJSON is the shape written by WriteJSON.
type skillJSON struct {
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	Agents    []string        `json:"agents"`
	RiskLevel string          `json:"risk_level,omitempty"`
	RiskScore int             `json:"risk_score,omitempty"`
	Findings  []findingJSON   `json:"findings,omitempty"`
}

type findingJSON struct {
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Desc     string `json:"description"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Evidence string `json:"evidence,omitempty"`
}

// WriteJSON writes audit results as a JSON array.
func WriteJSON(skills []*models.Skill, w io.Writer) error {
	out := make([]skillJSON, 0, len(skills))
	for _, s := range skills {
		agents := make([]string, len(s.Agents))
		for i, a := range s.Agents {
			agents[i] = string(a)
		}
		sj := skillJSON{Name: s.Name, Path: s.Path, Agents: agents}
		if s.Audit != nil {
			sj.RiskLevel = string(s.Audit.RiskLevel)
			sj.RiskScore = s.Audit.RiskScore
			for _, f := range s.Audit.Findings {
				sj.Findings = append(sj.Findings, findingJSON{
					Severity: string(f.Severity),
					Rule:     f.Rule,
					Desc:     f.Description,
					File:     f.File,
					Line:     f.Line,
					Evidence: f.Evidence,
				})
			}
		}
		out = append(out, sj)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func agentStr(agents []models.AgentTarget) string {
	parts := make([]string, len(agents))
	for i, a := range agents {
		parts[i] = string(a)
	}
	return strings.Join(parts, ", ")
}

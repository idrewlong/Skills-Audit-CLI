package audit

import (
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

// SARIF 2.1.0 — Static Analysis Results Interchange Format

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	ShortDescription sarifMessage    `json:"shortDescription"`
	DefaultConfig    sarifRuleConfig `json:"defaultConfiguration"`
	Properties       map[string]any  `json:"properties,omitempty"`
}

type sarifRuleConfig struct {
	Level string `json:"level"` // "error" | "warning" | "note"
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

func riskToSarifLevel(level models.RiskLevel) string {
	switch level {
	case models.RiskCritical, models.RiskHigh:
		return "error"
	case models.RiskMedium:
		return "warning"
	default:
		return "note"
	}
}

// WriteSARIF encodes audit results for all skills as SARIF 2.1.0 JSON to w.
// toolVersion is embedded in the driver block (pass the CLI version string).
func WriteSARIF(skills []*models.Skill, toolVersion string, w io.Writer) error {
	var sarifRules []sarifRule
	for _, r := range rules {
		sarifRules = append(sarifRules, sarifRule{
			ID:               r.ID,
			Name:             r.ID,
			ShortDescription: sarifMessage{Text: r.Description},
			DefaultConfig:    sarifRuleConfig{Level: riskToSarifLevel(r.Severity)},
			Properties:       map[string]any{"tags": []string{"security", "skill-mgr"}},
		})
	}

	var results []sarifResult
	for _, skill := range skills {
		if skill.Audit == nil {
			continue
		}
		for _, f := range skill.Audit.Findings {
			results = append(results, sarifResult{
				RuleID:  f.Rule,
				Level:   riskToSarifLevel(f.Severity),
				Message: sarifMessage{Text: f.Description},
				Locations: []sarifLocation{
					{
						PhysicalLocation: sarifPhysicalLocation{
							ArtifactLocation: sarifArtifactLocation{
								URI: filepath.ToSlash(f.File),
							},
							Region: sarifRegion{StartLine: f.Line},
						},
					},
				},
			})
		}
	}

	log := sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "skill-mgr",
						Version:        toolVersion,
						InformationURI: "https://github.com/idrewlong/skill-mgr",
						Rules:          sarifRules,
					},
				},
				Results: results,
			},
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

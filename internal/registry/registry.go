package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/idrewlong/skill-mgr/pkg/models"
)

const (
	skillsShBaseURL = "https://registry.skills.sh/api/v1"
	userAgent       = "skill-mgr/1.0 (https://github.com/idrewlong/skill-mgr)"
	timeout         = 10 * time.Second
)

// SkillsShResponse is the shape of the skills.sh registry API response
type SkillsShResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
	Security struct {
		Gen struct {
			Result string `json:"result"` // "Safe" | "Unsafe" | "Unknown"
		} `json:"gen"`
		Socket struct {
			AlertCount int `json:"alert_count"`
		} `json:"socket"`
		Snyk struct {
			RiskLevel string `json:"risk_level"` // "Low Risk" | "Medium Risk" | "High Risk"
		} `json:"snyk"`
	} `json:"security"`
}

var client = &http.Client{Timeout: timeout}

// FetchScore retrieves the security score for a skill from the skills.sh registry.
// skillSlug should be in the format "owner/repo" or "owner/repo@skill-name".
func FetchScore(skillSlug string) (*models.RegistryScore, error) {
	// Normalize slug: strip leading https://github.com/ etc.
	slug := normalizeSlug(skillSlug)
	if slug == "" {
		return nil, fmt.Errorf("could not determine registry slug for skill")
	}

	url := fmt.Sprintf("%s/skills/%s/security", skillsShBaseURL, slug)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("skill not found in registry: %s", slug)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var data SkillsShResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid registry response: %w", err)
	}

	return &models.RegistryScore{
		GenResult:    data.Security.Gen.Result,
		SocketAlerts: data.Security.Socket.AlertCount,
		SnykRisk:     data.Security.Snyk.RiskLevel,
		FetchedAt:    time.Now(),
	}, nil
}

// FetchLatestSHA returns the latest commit SHA for a skill from the registry.
// Used for update checking.
func FetchLatestSHA(skillSlug string) (string, error) {
	slug := normalizeSlug(skillSlug)
	if slug == "" {
		return "", fmt.Errorf("could not determine registry slug")
	}

	url := fmt.Sprintf("%s/skills/%s", skillsShBaseURL, slug)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var data SkillsShResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("invalid registry response: %w", err)
	}

	return data.GitSHA, nil
}

// normalizeSlug converts various skill identifiers to owner/repo[@skill] format
func normalizeSlug(raw string) string {
	raw = strings.TrimSpace(raw)

	// Already a slug like "greensock/gsap-skills" or "greensock/gsap-skills@gsap-core"
	if !strings.Contains(raw, "://") && strings.Contains(raw, "/") {
		return raw
	}

	// GitHub URL: https://github.com/owner/repo
	raw = strings.TrimPrefix(raw, "https://github.com/")
	raw = strings.TrimPrefix(raw, "http://github.com/")
	raw = strings.TrimSuffix(raw, ".git")

	if strings.Contains(raw, "/") {
		return raw
	}

	return ""
}

// FormatRegistryScore returns a compact display string for a registry score
func FormatRegistryScore(rs *models.RegistryScore) string {
	if rs == nil {
		return "not in registry"
	}

	gen := rs.GenResult
	if gen == "" {
		gen = "unknown"
	}

	socket := "0 alerts"
	if rs.SocketAlerts > 0 {
		socket = fmt.Sprintf("%d alert(s)", rs.SocketAlerts)
	}

	snyk := rs.SnykRisk
	if snyk == "" {
		snyk = "unknown"
	}

	return fmt.Sprintf("Gen: %s | Socket: %s | Snyk: %s", gen, socket, snyk)
}

package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds persistent user preferences for skill-mgr.
type Config struct {
	DefaultAgent string // default --agent filter (e.g. "claude-code")
	FailOn       string // default --fail-on level ("safe","low","medium","high","critical")
	DefaultSort  string // default --sort field ("name","date","risk","update")
}

var defaults = Config{
	DefaultAgent: "",
	FailOn:       "high",
	DefaultSort:  "name",
}

// Load reads ~/.config/skill-mgr/config.yaml.
// Returns defaults if the file does not exist.
func Load() (*Config, error) {
	f, err := os.Open(Path())
	if err != nil {
		if os.IsNotExist(err) {
			c := defaults
			return &c, nil
		}
		return &defaults, fmt.Errorf("could not read config: %w", err)
	}
	defer f.Close()

	cfg := defaults
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "default_agent":
			cfg.DefaultAgent = val
		case "fail_on":
			cfg.FailOn = val
		case "default_sort":
			cfg.DefaultSort = val
		}
	}
	return &cfg, scanner.Err()
}

// Save writes cfg to ~/.config/skill-mgr/config.yaml, creating dirs as needed.
func Save(cfg *Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}
	f, err := os.Create(p)
	if err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}
	defer f.Close()

	fmt.Fprintln(f, "# skill-mgr configuration")
	fmt.Fprintln(f, "# https://github.com/idrewlong/skill-mgr")
	fmt.Fprintln(f)
	fmt.Fprintf(f, "default_agent: %s\n", cfg.DefaultAgent)
	fmt.Fprintf(f, "fail_on: %s\n", cfg.FailOn)
	fmt.Fprintf(f, "default_sort: %s\n", cfg.DefaultSort)
	return nil
}

// Path returns the absolute path to the config file.
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "skill-mgr", "config.yaml")
}

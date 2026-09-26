// Package config loads the dashboard sections from YAML.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// Section is one dashboard tab: a title plus a Linear IssueFilter passed to the API as-is.
type Section struct {
	Title  string
	Filter json.RawMessage
}

type Config struct {
	Limit    int
	Sections []Section
}

type fileSection struct {
	Title  string         `yaml:"title"`
	Filter map[string]any `yaml:"filter"`
}

type file struct {
	Limit    int           `yaml:"limit"`
	Sections []fileSection `yaml:"sections"`
}

// DefaultPath is $XDG_CONFIG_HOME/linear-dash/config.yml, falling back to ~/.config like gh-dash does.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "linear-dash", "config.yml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	return filepath.Join(home, ".config", "linear-dash", "config.yml"), nil
}

// Load reads and validates a config file.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("config not found at %s: copy config.example.yml from the repo there", path)
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if f.Limit < 1 || f.Limit > 250 {
		return Config{}, fmt.Errorf("config %s: limit must be between 1 and 250, got %d", path, f.Limit)
	}
	if len(f.Sections) == 0 {
		return Config{}, fmt.Errorf("config %s: at least one section is required", path)
	}

	sections := make([]Section, 0, len(f.Sections))
	for i, s := range f.Sections {
		if s.Title == "" {
			return Config{}, fmt.Errorf("config %s: section %d has no title", path, i+1)
		}
		if len(s.Filter) == 0 {
			return Config{}, fmt.Errorf("config %s: section %q has no filter", path, s.Title)
		}
		filter, err := json.Marshal(s.Filter)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: section %q filter: %w", path, s.Title, err)
		}
		sections = append(sections, Section{Title: s.Title, Filter: filter})
	}
	return Config{Limit: f.Limit, Sections: sections}, nil
}

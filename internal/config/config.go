// Package config loads the dashboard sections from YAML.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Kind is what a section lists.
type Kind string

const (
	// KindIssues lists issues matching a Linear IssueFilter.
	KindIssues Kind = "issues"
	// KindNotifications lists your Linear inbox.
	KindNotifications Kind = "notifications"
	// KindReleases lists active and recently completed releases.
	KindReleases Kind = "releases"
)

var kinds = []Kind{KindIssues, KindNotifications, KindReleases}

// Section is one dashboard tab.
type Section struct {
	Title string
	Kind  Kind
	// Filter is a Linear IssueFilter passed to the API as-is; only set for KindIssues
	Filter json.RawMessage
	// Grouped opens the tab grouped (by state, or by pipeline for releases)
	Grouped bool
	// RecentDays is how long completed releases stay listed; only set for KindReleases
	RecentDays int
}

type Config struct {
	Limit int
	// Refresh is how often every section reloads; zero turns auto-refresh off
	Refresh  time.Duration
	Sections []Section
	// RepoPaths maps a Linear team key to the local clone used for its branches
	RepoPaths map[string]string
}

type fileSection struct {
	Title      string         `yaml:"title"`
	Kind       string         `yaml:"kind"`
	Filter     map[string]any `yaml:"filter"`
	Grouped    bool           `yaml:"grouped"`
	RecentDays *int           `yaml:"recentDays"`
}

type file struct {
	Limit          int               `yaml:"limit"`
	RefreshMinutes int               `yaml:"refreshMinutes"`
	RepoPaths      map[string]string `yaml:"repoPaths"`
	Sections       []fileSection     `yaml:"sections"`
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
	if f.RefreshMinutes < 0 {
		return Config{}, fmt.Errorf("config %s: refreshMinutes must be 0 (off) or more, got %d", path, f.RefreshMinutes)
	}
	if len(f.Sections) == 0 {
		return Config{}, fmt.Errorf("config %s: at least one section is required", path)
	}

	sections := make([]Section, 0, len(f.Sections))
	for i, s := range f.Sections {
		section, err := parseSection(s)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: section %d: %w", path, i+1, err)
		}
		sections = append(sections, section)
	}

	repoPaths, err := expandPaths(f.RepoPaths)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return Config{
		Limit:     f.Limit,
		Refresh:   time.Duration(f.RefreshMinutes) * time.Minute,
		Sections:  sections,
		RepoPaths: repoPaths,
	}, nil
}

func parseSection(s fileSection) (Section, error) {
	if s.Title == "" {
		return Section{}, errors.New("title is required")
	}
	// kind is optional in YAML so v0 configs keep working; omitted means an issue list
	kind := KindIssues
	if s.Kind != "" {
		kind = Kind(s.Kind)
	}
	if !slices.Contains(kinds, kind) {
		return Section{}, fmt.Errorf("%q: kind must be one of %v, got %q", s.Title, kinds, s.Kind)
	}
	if s.RecentDays != nil && kind != KindReleases {
		return Section{}, fmt.Errorf("%q: recentDays only applies to releases sections", s.Title)
	}
	if kind == KindReleases {
		if s.RecentDays == nil {
			return Section{}, fmt.Errorf("%q: releases sections need recentDays (how long completed releases stay listed)", s.Title)
		}
		days := *s.RecentDays
		if days < 0 {
			return Section{}, fmt.Errorf("%q: recentDays must be 0 or more, got %d", s.Title, days)
		}
		if len(s.Filter) > 0 {
			return Section{}, fmt.Errorf("%q: releases sections don't take a filter", s.Title)
		}
		return Section{Title: s.Title, Kind: kind, Grouped: s.Grouped, RecentDays: days}, nil
	}
	if kind != KindIssues {
		if len(s.Filter) > 0 {
			return Section{}, fmt.Errorf("%q: %s sections don't take a filter", s.Title, kind)
		}
		return Section{Title: s.Title, Kind: kind, Grouped: s.Grouped}, nil
	}
	if len(s.Filter) == 0 {
		return Section{}, fmt.Errorf("%q: filter is required", s.Title)
	}
	filter, err := json.Marshal(s.Filter)
	if err != nil {
		return Section{}, fmt.Errorf("%q: filter: %w", s.Title, err)
	}
	return Section{Title: s.Title, Kind: kind, Filter: filter, Grouped: s.Grouped}, nil
}

// expandPaths resolves a leading ~ in each repo path.
func expandPaths(paths map[string]string) (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home for repoPaths: %w", err)
	}
	out := make(map[string]string, len(paths))
	for team, p := range paths {
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			p = filepath.Join(home, rest)
		}
		out[team] = p
	}
	return out, nil
}

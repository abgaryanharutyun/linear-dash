// Command linear-dash is a gh-dash style terminal dashboard for Linear issues.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/abgaryanharutyun/linear-dash/internal/config"
	"github.com/abgaryanharutyun/linear-dash/internal/linear"
	"github.com/abgaryanharutyun/linear-dash/internal/ui"
)

const (
	endpoint       = "https://api.linear.app/graphql"
	requestTimeout = 20 * time.Second
	attempts       = 3
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "linear-dash:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flag.String("config", defaultPath, "path to the config file")
	flag.Parse()

	if !term.IsTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("stdout is not a terminal, so the dashboard can't draw; if you run it through `op run`, add --no-masking")
	}

	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LINEAR_API_KEY is not set: create a personal API key at https://linear.app/settings/account/security")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	logFile, err := openLog()
	if err != nil {
		return err
	}
	defer logFile.Close()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logFile, nil)))

	client := linear.NewClient(apiKey, endpoint, requestTimeout, attempts)
	if _, err := tea.NewProgram(ui.New(client, cfg)).Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

// openLog sends logs to a file because stderr would corrupt the TUI.
func openLog() (*os.File, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve log dir: %w", err)
	}
	path := filepath.Join(dir, "linear-dash", "linear-dash.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	return f, nil
}

// Package gitrepo switches branches in local clones.
package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// SwitchResult says what Switch did.
type SwitchResult string

const (
	SwitchedLocal  SwitchResult = "switched to existing branch"
	SwitchedRemote SwitchResult = "checked out branch from origin"
	Created        SwitchResult = "created branch"
)

// Switch checks out branch in the repo at path, creating it from the current HEAD when it exists nowhere.
func Switch(path string, branch string) (SwitchResult, error) {
	if err := validBranch(branch); err != nil {
		return "", err
	}
	local, err := refExists(path, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if local {
		return SwitchedLocal, run(path, "switch", branch)
	}
	remote, err := refExists(path, "refs/remotes/origin/"+branch)
	if err != nil {
		return "", err
	}
	if remote {
		return SwitchedRemote, run(path, "switch", "--track", "origin/"+branch)
	}
	return Created, run(path, "switch", "-c", branch)
}

// validBranch rejects names git wouldn't accept as a branch, and ones that would parse as a flag.
func validBranch(branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	cmd := exec.Command("git", "check-ref-format", "--branch", branch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invalid branch name %q: %w: %s", branch, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// refExists uses show-ref's exit code: 0 found, 1 missing, anything else is a real failure.
func refExists(path string, ref string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "show-ref", "--verify", "--quiet", ref)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git -C %s show-ref %s: %w: %s", path, ref, err, strings.TrimSpace(stderr.String()))
}

func run(path string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git -C %s %s: %w: %s", path, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

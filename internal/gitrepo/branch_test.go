package gitrepo

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestSwitch covers the three paths against real repos: create, reuse local, and track a branch only on origin.
func TestSwitch(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	clone := filepath.Join(root, "clone")

	git(t, root, "init", "-q", "-b", "main", origin)
	git(t, origin, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	git(t, origin, "branch", "eng-2-remote-only")
	git(t, root, "clone", "-q", origin, clone)

	result, err := Switch(clone, "eng-1-new")
	if err != nil || result != Created {
		t.Fatalf("new branch: result=%q err=%v", result, err)
	}
	git(t, clone, "switch", "-q", "main")

	result, err = Switch(clone, "eng-1-new")
	if err != nil || result != SwitchedLocal {
		t.Fatalf("existing local branch: result=%q err=%v", result, err)
	}

	result, err = Switch(clone, "eng-2-remote-only")
	if err != nil || result != SwitchedRemote {
		t.Fatalf("remote-only branch: result=%q err=%v", result, err)
	}
	if upstream := git(t, clone, "rev-parse", "--abbrev-ref", "@{upstream}"); upstream != "origin/eng-2-remote-only" {
		t.Fatalf("remote-only branch should track origin, got upstream %q", upstream)
	}

	for _, bad := range []string{"-f", "bad..name", ""} {
		if _, err := Switch(clone, bad); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}

	if _, err := Switch(filepath.Join(root, "missing"), "x"); err == nil {
		t.Fatal("expected an error for a path that isn't a repo")
	}
}

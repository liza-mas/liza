package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doctorChecks(env map[string]any) []map[string]any {
	result, _ := env["result"].(map[string]any)
	raw, _ := result["checks"].([]any)
	checks := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if check, ok := item.(map[string]any); ok {
			checks = append(checks, check)
		}
	}
	return checks
}

func doctorCheckByID(env map[string]any, id string) map[string]any {
	for _, check := range doctorChecks(env) {
		if check["id"] == id {
			return check
		}
	}
	return nil
}

// TestDoctorPersonalGlobalContractIsDegraded covers doctor's done-when case:
// a personal file at the named provider's global contract location is a warn
// check naming the provider and the path, and the run exits degraded.
func TestDoctorPersonalGlobalContractIsDegraded(t *testing.T) {
	f := newDegradedFixture(t)
	f.setup(t)
	global := f.occupyGlobalClaudeContract(t)
	stdout, _, code := f.run(t, "doctor", "--json", "--provider", "claude")
	if code != exitDegraded {
		t.Fatalf("doctor exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
	}
	assertDegradedCheck(t, decodeSingleEnvelope(t, stdout), "doctor.global-contract", "claude", global)
}

func TestDoctorCleanSetupAndInitExitsZero(t *testing.T) {
	f := newDegradedFixture(t)
	f.setup(t)
	if _, stderr, code := f.run(t, "init", "--claude", "--yes"); code != 0 {
		t.Fatalf("init precondition: exit %d\n%s", code, stderr)
	}
	stdout, _, code := f.run(t, "doctor", "--json", "--provider", "claude", "--repo")
	if code != 0 {
		t.Fatalf("doctor exit = %d, want 0\nstdout:\n%s", code, stdout)
	}
	env := decodeSingleEnvelope(t, stdout)
	checks := doctorChecks(env)
	if env["ok"] != true || len(checks) == 0 {
		t.Fatalf("envelope = %v, want ok with checks", env)
	}
	for _, check := range checks {
		if check["status"] != "ok" {
			t.Fatalf("clean setup + init has a non-ok check: %v", check)
		}
	}
	if doctorCheckByID(env, "doctor.repo-contract") == nil {
		t.Fatal("--repo did not add repository checks")
	}
}

func TestDoctorRepoOutsideGitRepositoryFails(t *testing.T) {
	f := newDegradedFixture(t)
	f.setup(t)
	f.repo = t.TempDir()
	stdout, _, code := f.run(t, "doctor", "--json", "--repo")
	if code != 1 {
		t.Fatalf("doctor --repo outside a repository exit = %d, want 1\nstdout:\n%s", code, stdout)
	}
	if env := decodeSingleEnvelope(t, stdout); env["ok"] != false {
		t.Fatalf("envelope ok = %v, want false", env["ok"])
	}
}

func TestDoctorHumanOutput(t *testing.T) {
	f := newDegradedFixture(t)
	f.setup(t)
	global := f.occupyGlobalClaudeContract(t)
	stdout, stderr, code := f.run(t, "doctor", "--provider", "claude")
	if code != exitDegraded {
		t.Fatalf("doctor exit = %d, want %d", code, exitDegraded)
	}
	if !strings.Contains(stdout, "1 not ok") {
		t.Fatalf("stdout lacks the summary:\n%s", stdout)
	}
	if !strings.Contains(stderr, "doctor.global-contract claude "+global) {
		t.Fatalf("stderr does not list the non-ok check:\n%s", stderr)
	}
}

// TestDoctorCommandWritesNothing runs the built command, root hooks included,
// on a degraded machine and compares HOME and the repository before and after.
func TestDoctorCommandWritesNothing(t *testing.T) {
	f := newDegradedFixture(t)
	f.setup(t)
	if _, stderr, code := f.run(t, "init", "--claude", "--yes"); code != 0 {
		t.Fatalf("init precondition: exit %d\n%s", code, stderr)
	}
	// Init linked the global contract: replace the link, never write through it.
	if err := os.Remove(filepath.Join(f.home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	f.occupyGlobalClaudeContract(t)
	before := snapshotPaths(t, f.home, f.repo)
	if _, _, code := f.run(t, "doctor", "--json", "--provider", "claude", "--repo"); code != exitDegraded {
		t.Fatalf("doctor exit = %d, want %d", code, exitDegraded)
	}
	after := snapshotPaths(t, f.home, f.repo)
	for path, state := range before {
		if after[path] != state {
			t.Fatalf("doctor changed %s", path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Fatalf("doctor created %s", path)
		}
	}
}

func snapshotPaths(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			state := fmt.Sprintf("%s %d", info.Mode(), info.ModTime().UnixNano())
			if info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(path)
				state += " -> " + target
			} else if info.Mode().IsRegular() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(data)
				state += " " + hex.EncodeToString(sum[:])
			}
			out[path] = state
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

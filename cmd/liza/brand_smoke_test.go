package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonDefaultBrandBuildSmoke(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "acme-agent")
	ldflags := strings.Join([]string{
		"-X github.com/liza-mas/liza/internal/brand.NameLower=acme-agent",
		"-X github.com/liza-mas/liza/internal/brand.NameUpper=ACME_AGENT",
		"-X 'github.com/liza-mas/liza/internal/brand.NameTitle=Acme Agent'",
		"-X github.com/liza-mas/liza/internal/brand.Repo=acme/agent",
		"-X github.com/liza-mas/liza/internal/brand.BinaryName=acme-agent",
		"-X github.com/liza-mas/liza/internal/brand.GlobalDirName=.acme-agent",
		"-X github.com/liza-mas/liza/internal/brand.ProjectDirName=.acme-agent",
		"-X github.com/liza-mas/liza/internal/brand.EnvPrefix=ACME_AGENT",
		"-X github.com/liza-mas/liza/internal/brand.ArchivePrefix=acme-release",
		"-X github.com/liza-mas/liza/internal/brand.ReleaseRepo=acme/agent",
		"-X github.com/liza-mas/liza/internal/brand.ReleaseBaseURL=https://github.com/acme/agent/releases/download",
		"-X github.com/liza-mas/liza/internal/brand.ChecksumBaseURL=https://github.com/acme/agent/releases/download",
		"-X main.Version=v9.9.9-smoke",
	}, " ")

	build := exec.Command("go", "build", "-o", bin, "-ldflags", ldflags, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	version := runBrandSmokeCommand(t, bin, "version")
	assertContains(t, version, "acme-agent version v9.9.9-smoke")

	rootHelp := runBrandSmokeCommand(t, bin, "--help")
	assertContains(t, rootHelp, "Acme Agent is a multi-agent task execution system")
	assertContains(t, rootHelp, "acme-agent [command]")

	initHelp := runBrandSmokeCommand(t, bin, "init", "--help")
	assertContains(t, initHelp, "~/.acme-agent/CORE.md")
	assertContains(t, initHelp, "Acme Agent contract")

	agentHelp := runBrandSmokeCommand(t, bin, "agent", "--help")
	assertContains(t, agentHelp, "ACME_AGENT_AGENT_ID")
}

func runBrandSmokeCommand(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "ACME_AGENT_SKIP_AUTO_UPDATE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", bin, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

// Package a8 freezes the supervisor's default-brand POSIX prompt surfaces.
package a8

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) { oracle.RunMain(m) }

func TestOraclePrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prompt goldens embed POSIX native paths")
	}
	for _, fixture := range loadFixtures(t) {
		t.Run(fixture.ID, func(t *testing.T) {
			prompt := renderFixture(t, fixture)
			for _, marker := range fixture.Markers {
				if !strings.Contains(prompt, marker) {
					t.Fatalf("%s %s: missing branch marker %q", fixture.ID, goldenPath(fixture.ID), marker)
				}
			}
			if fixture.Wake != "" {
				var triggers []string
				for _, line := range strings.Split(prompt, "\n") {
					if strings.HasPrefix(line, "WAKE TRIGGER:") {
						triggers = append(triggers, line)
					}
				}
				if len(triggers) != 1 || triggers[0] != "WAKE TRIGGER: "+fixture.Wake {
					t.Fatalf("%s %s: wrong WAKE TRIGGER line", fixture.ID, goldenPath(fixture.ID))
				}
			}
			oracle.Check(t, oracle.Prompt, fixture.ID+".txt", []byte(prompt))
		})
	}
}

func TestOraclePromptDistinctRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prompt goldens embed POSIX native paths")
	}
	for _, fixture := range loadFixtures(t) {
		t.Run(fixture.ID, func(t *testing.T) {
			first := renderFixture(t, fixture)
			second := renderFixture(t, fixture)
			if !bytes.Equal([]byte(first), []byte(second)) {
				t.Fatalf("%s: renders from distinct roots differ after case-root substitution", fixture.ID)
			}
		})
	}
}

func goldenPath(id string) string { return fmt.Sprintf("testdata/golden/prompt/%s.txt", id) }

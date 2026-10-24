package a9

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// orchGuardCase is one sprint pair of the I-13 guard fixture.
type orchGuardCase struct {
	Name    string        `yaml:"name"`
	Current models.Sprint `yaml:"current"`
}

type orchGuardFixture struct {
	Expected models.Sprint   `yaml:"expected"`
	Cases    []orchGuardCase `yaml:"cases"`
}

// orchGuardOutcome is what the exported predicate answered for one case.
type orchGuardOutcome struct {
	Name string
	Same bool
}

// orchSameCheckpoint is the I-13 refusal adapter (AC2). The callback guard of
// PreWork compares the resumed checkpoint a second time inside its write; that
// comparison differs from the gate only under an interleaved checkpoint, which
// the binary cannot produce (D44), so the exported pure predicate is called on
// fixture sprint pairs. Only exported entry points are used.
func orchSameCheckpoint(path string) ([]orchGuardOutcome, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read guard fixture: %w", err)
	}
	var fixture orchGuardFixture
	if err := yaml.Unmarshal(raw, &fixture); err != nil {
		return nil, fmt.Errorf("parse guard fixture: %w", err)
	}
	outcomes := make([]orchGuardOutcome, 0, len(fixture.Cases))
	for _, c := range fixture.Cases {
		outcomes = append(outcomes, orchGuardOutcome{c.Name, ops.SameResumedTransitionCheckpoint(c.Current, fixture.Expected)})
	}
	return outcomes, nil
}

package present

import (
	"io"
	"path/filepath"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
)

func watchAlertLines(projectRoot string) ([]string, error) {
	state, err := db.For(filepath.Join(projectRoot, ".liza", "state.yaml")).ReadSnapshot()
	if err != nil {
		return nil, err
	}
	snapshot := commands.RunChecksWithStateSnapshot(state, commands.WatchConfig{ProjectRoot: projectRoot, WarnWriter: io.Discard})
	lines := make([]string, 0, len(snapshot.Alerts))
	for _, alert := range snapshot.Alerts {
		lines = append(lines, alert.String())
	}
	return lines, nil
}

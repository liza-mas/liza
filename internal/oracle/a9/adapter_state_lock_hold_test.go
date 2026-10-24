package a9

import (
	"context"
	"path/filepath"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// recoverStateLockHolds is the state-lock-hold recovery adapter (AC2). The
// supervisor's poll calls the exported ops.RecoverStateLockHolds between wake
// selections; the binary has no command for it, and a supervisor run cannot
// pin the moment a hold becomes recoverable. The exported entry point is
// called on the scenario's disposable state and real Git repository as the
// registered orchestrator generation. No other module symbol is used.
func recoverStateLockHolds(ctx context.Context, p *scenario) (int, error) {
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "gen-1"}
	return ops.RecoverStateLockHolds(ctx, db.For(filepath.Join(p.dir, ".liza", "state.yaml")), p.dir, authority)
}

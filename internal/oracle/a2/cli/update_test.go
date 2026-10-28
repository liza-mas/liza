//go:build linux

package cli

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2UpdateSettings(t *testing.T) {
	t.Run("settings", func(t *testing.T) {
		f := newFixture(t)
		f.check("update-settings/saved", oracle.Human, f.run(nil, "--check-update=false", "--update-channel", "main"), 0)
		f.fileEffects("update-settings/saved", map[string]string{"home": f.home, "project": f.project})
	})
	t.Run("invalid", func(t *testing.T) {
		f := newFixture(t)
		f.check("update-settings/invalid", oracle.Human, f.run(nil, "--check-update=maybe"), 1)
		f.fileEffects("update-settings/invalid", map[string]string{"home": f.home, "project": f.project})
	})
}

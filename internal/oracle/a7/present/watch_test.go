package present

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleWatchAlerts(t *testing.T) {
	skipUnlessLinux(t)
	e := newEnvironment(t)
	lines, err := watchAlertLines(materialiseFixture(t, e))
	if err != nil {
		t.Fatal("load watch fixture state")
	}
	// Alert order is asserted exactly as emitted; never sort this output.
	e.norm.Check(t, oracle.Human, "watch/alerts.txt", []byte(maskHuman(strings.Join(lines, "\n"))))
}

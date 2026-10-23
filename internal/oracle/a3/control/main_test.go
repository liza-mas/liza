// Package control freezes the default-brand A3 operator control CLI surfaces.
// Drivers import only the standard library and the oracle harness.
package control

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) {
	setUmask()
	oracle.RunMain(m)
}

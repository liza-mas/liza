package agent

import (
	"testing"

	"c6fixture/internal/db"
)

func TestExempt(t *testing.T) {
	_ = db.For("p").Write(nil)
}

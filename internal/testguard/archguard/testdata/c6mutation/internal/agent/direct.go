// Package agent holds one violation per rule shape.
package agent

import (
	"time"

	"c6fixture/internal/db"
	"c6fixture/internal/models"
)

func direct(bb *db.Blackboard) {
	_ = bb.Modify(nil)
	_ = bb.Write(nil)
	_ = bb.UpdateTask("t", nil)
	_ = bb.UpdateAgent("a", nil)
	_, _ = bb.ArchiveTerminalTask(&models.Task{}, time.Time{})
}

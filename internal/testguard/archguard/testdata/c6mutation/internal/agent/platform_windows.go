package agent

import "c6fixture/internal/db"

func windowsOnly(bb *db.Blackboard) {
	_ = bb.Write(nil)
}

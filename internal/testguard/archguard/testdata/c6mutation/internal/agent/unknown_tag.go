//go:build c6fixtureunknowntag

package agent

import "c6fixture/internal/db"

func neverBuilt(bb *db.Blackboard) {
	_ = bb.Write(nil)
}

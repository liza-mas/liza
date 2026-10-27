package agent

import "c6fixture/internal/db"

type wrapper struct{ *db.Blackboard }

func indirect(bb *db.Blackboard) {
	value := bb.Write
	_ = value(nil)
	expression := (*db.Blackboard).Write
	_ = expression(bb, nil)
	_ = db.For("p").Patient().Write(nil)
	_ = wrapper{bb}.Write(nil)
}

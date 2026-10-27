package agent

import "c6fixture/internal/models"

type store interface {
	Modify(fn func(*models.State) error) error
}

func viaInterface(s store) error {
	return s.Modify(nil)
}

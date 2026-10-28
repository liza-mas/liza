package ops

import (
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// ToggleAutoResume inverts the auto_resume config flag in one locked
// transaction and returns the committed value. The caller keeps the handle,
// so its acquisition policy is unchanged; no history entry is recorded.
func ToggleAutoResume(bb *db.Blackboard) (bool, error) {
	var committed bool
	err := bb.Modify(func(s *models.State) error {
		s.Config.AutoResume = !s.Config.AutoResume
		committed = s.Config.AutoResume
		return nil
	})
	if err != nil {
		return false, err
	}
	return committed, nil
}

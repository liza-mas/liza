package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// renewLease sets a fresh lease expiry on task based on the configured duration.
//
// A lease is one half of the ownership tuple: `assigned_to` and `lease_expires`
// are valid only together (statevalidate.validate_task.go). An unassigned task
// therefore gets no lease, and a lease left behind by a doer that is already
// gone is cleared rather than refreshed — a verdict landing on a task whose
// agent exited between submission and review would otherwise renew a lease
// nobody holds and fail global validation from then on.
func renewLease(s *models.State, t *models.Task) {
	if t.AssignedTo == nil {
		t.LeaseExpires = nil
		return
	}
	dur := s.Config.LeaseDuration
	if dur <= 0 {
		dur = models.DefaultLeaseDurationSeconds
	}
	exp := time.Now().UTC().Add(time.Duration(dur) * time.Second)
	t.LeaseExpires = &exp
}

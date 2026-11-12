package models

import "time"

// PoolReservation holds one run-wide slot for a role that has actionable work
// and no occupied registration (ADR-0246). Pool auto-repair writes it; an
// entry whose Until has passed is ignored by every reader, so a watcher that
// stopped refreshing cannot keep a slot held.
type PoolReservation struct {
	Role       string    `yaml:"role" json:"role"`
	SelectedAt time.Time `yaml:"selected_at" json:"selected_at"`
	Until      time.Time `yaml:"until" json:"until"`
}

// UnexpiredPoolReservations returns the reservations still in force at now,
// in reservation order.
func UnexpiredPoolReservations(state *State, now time.Time) []PoolReservation {
	if state == nil {
		return nil
	}
	var live []PoolReservation
	for _, reservation := range state.PoolReservations {
		if reservation.Until.After(now) {
			live = append(live, reservation)
		}
	}
	return live
}

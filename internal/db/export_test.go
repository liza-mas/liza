package db

import "time"

// SetPatientReadLockTimeoutForTest shortens the ReadContextPatient wait for
// external tests. Callers must not run in parallel with other patient reads.
func SetPatientReadLockTimeoutForTest(timeout time.Duration) func() {
	previous := patientReadLockTimeout
	patientReadLockTimeout = timeout
	return func() {
		patientReadLockTimeout = previous
	}
}

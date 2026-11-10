package providersignal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/liza-mas/liza/internal/atomicfile"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
)

// Quota recovery (#114). An expired quota signal does not mean the provider
// recovered: it means one attempt may find out. A per-provider lease file next
// to the signal elects that attempt across processes:
//
//   - a spawn takes a reservation for the agent it starts, so one recovery
//     costs one spawn, not one per missing role;
//   - the launch gate turns the reservation (or no lease) into the probe, the
//     one provider start admitted while the signal is expired;
//   - the probe's outcome settles the episode: a provider answer clears the
//     signal it was admitted on, renewed exhaustion re-raises it, anything
//     else frees the lease for the next attempt.
//
// Every mutation of the signal and its lease runs under one per-provider file
// lock, a leaf lock no holder nests another lock inside. Readers outside it
// see a whole lease or none, since leases are published by rename.
const (
	quotaProbePrefix = "provider-quota-probe-"
	// quotaReservationHold bounds a spawn reservation whose agent has not
	// reached its first provider start. A probe has no time bound: it lasts
	// while its holder process lives, so no replacement overlaps it.
	quotaReservationHold         = 10 * time.Minute
	quotaReservationHolderPrefix = "spawn:"
	quotaLifecycleLockOperation  = "quota-recovery"
	quotaLifecycleLockPrefix     = "quota-recovery-"

	recordKeyKind       = "kind"
	recordKeyHolder     = "holder"
	recordKeyPID        = "pid"
	recordKeyAcquired   = "acquired"
	recordKeyGeneration = "signal_generation"

	quotaLeaseReservation = "reservation"
	quotaLeaseProbe       = "probe"
)

// QuotaAdmission is how the quota lifecycle answers a start request.
type QuotaAdmission int

const (
	// QuotaClear: no quota signal; the start is not a recovery attempt.
	QuotaClear QuotaAdmission = iota
	// QuotaProbe: the caller holds the recovery probe for this start.
	QuotaProbe
	// QuotaReserved: the spawn holds the reservation for the agent it starts.
	QuotaReserved
	// QuotaRefused: the provider is blocked or another attempt is running.
	QuotaRefused
)

// QuotaAdmissionResult is an admission and, for a refusal, its reason.
type QuotaAdmissionResult struct {
	Admission QuotaAdmission
	// Until is the block's expiry when the signal itself refuses, or the
	// reservation's hold; zero when unknown or when a probe refuses.
	Until time.Time
	// AttemptHolder names the reservation or probe that refuses the start,
	// empty when the unexpired signal does.
	AttemptHolder string
}

// QuotaProbeOutcome is what a probe's provider start showed.
type QuotaProbeOutcome int

const (
	// QuotaProbeRecovered: the provider answered the probe's turn.
	QuotaProbeRecovered QuotaProbeOutcome = iota
	// QuotaProbeInconclusive: the turn ended without showing either way.
	QuotaProbeInconclusive
)

// QuotaProbePath returns the recovery lease path for a provider.
func QuotaProbePath(projectRoot, provider string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), quotaProbePrefix+canonicalQuotaProvider(provider))
}

// QuotaProbeFile names the recovery lease relative to the project root, as
// operator-facing remedies print it.
func QuotaProbeFile(provider string) string {
	return filepath.Join(paths.ProjectDirName(), quotaProbePrefix+canonicalQuotaProvider(provider))
}

type quotaLease struct {
	Kind       string
	Holder     string
	PID        int
	Acquired   time.Time
	Expires    time.Time // reservation hold; zero for a probe
	Generation string    // probe: the signal it was admitted on
}

func (l quotaLease) record() []byte {
	content := recordLine(recordKeyKind, l.Kind) +
		recordLine(recordKeyHolder, l.Holder) +
		recordLine(recordKeyPID, strconv.Itoa(l.PID)) +
		recordLine(recordKeyAcquired, l.Acquired.UTC().Format(time.RFC3339))
	if !l.Expires.IsZero() {
		content += recordLine(recordKeyExpires, l.Expires.UTC().Format(time.RFC3339))
	}
	if l.Generation != "" {
		content += recordLine(recordKeyGeneration, l.Generation)
	}
	return []byte(content)
}

// holds reports whether the lease is the probe holderID holds from this process.
func (l quotaLease) holds(holderID string) bool {
	return l.Kind == quotaLeaseProbe && l.Holder == holderID && l.PID == os.Getpid()
}

// until is the refusal expiry a live lease reports: its hold when it has one.
func (l quotaLease) until() time.Time {
	return l.Expires
}

// processAlive reports whether pid runs. A failed probe counts as alive, so a
// lease is never taken over on an unanswered question.
var processAlive = func(pid int) bool {
	if pid <= 0 {
		return false
	}
	alive, _, err := procscan.ProcessAlive(pid)
	return alive || err != nil
}

// readLiveQuotaLease returns the provider's lease when one exists and still
// coordinates an attempt. A lease too damaged to read stays live for the
// reservation hold after its last write (fail closed), then is stale.
func readLiveQuotaLease(projectRoot, provider string, now time.Time) (quotaLease, bool) {
	path := QuotaProbePath(projectRoot, provider)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return quotaLease{}, false
	}
	lease, parsed := parseQuotaLease(data)
	if err != nil || !parsed {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return quotaLease{}, !os.IsNotExist(statErr)
		}
		return quotaLease{}, now.Sub(info.ModTime()) < quotaReservationHold
	}
	if lease.Kind == quotaLeaseReservation && !now.Before(lease.Expires) {
		return lease, false
	}
	return lease, processAlive(lease.PID)
}

func parseQuotaLease(data []byte) (quotaLease, bool) {
	fields := parseRecord(string(data))
	pid, pidErr := strconv.Atoi(fields[recordKeyPID])
	acquired, acquiredErr := time.Parse(time.RFC3339, fields[recordKeyAcquired])
	lease := quotaLease{Kind: fields[recordKeyKind], Holder: fields[recordKeyHolder], PID: pid, Acquired: acquired, Generation: fields[recordKeyGeneration]}
	if pidErr != nil || acquiredErr != nil || lease.Holder == "" {
		return quotaLease{}, false
	}
	switch lease.Kind {
	case quotaLeaseProbe:
		return lease, true
	case quotaLeaseReservation:
		expires, err := time.Parse(time.RFC3339, fields[recordKeyExpires])
		if err != nil {
			return quotaLease{}, false
		}
		lease.Expires = expires
		return lease, true
	}
	return quotaLease{}, false
}

func writeQuotaLease(projectRoot, provider string, lease quotaLease) error {
	return atomicfile.Write(QuotaProbePath(projectRoot, provider), lease.record())
}

func removeQuotaLease(projectRoot, provider string) error {
	err := os.Remove(QuotaProbePath(projectRoot, provider))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Lifecycle lock timing. Each acquisition attempt waits
// quotaLifecycleLockTimeout; a raise retries attempts for quotaRaiseLockBudget,
// since a detected exhaustion must not be dropped on brief contention.
var (
	quotaLifecycleLockTimeout = filelock.DefaultLockTimeout
	quotaRaiseLockBudget      = 2 * time.Minute
)

// quotaLifecycleLock is the provider's lifecycle lock. Its file is named
// apart from the provider-* signal files, which it outlives.
func quotaLifecycleLock(projectRoot, provider string) *filelock.FileLock {
	protected := filepath.Join(paths.New(projectRoot).LizaDir(), quotaLifecycleLockPrefix+canonicalQuotaProvider(provider))
	return filelock.New(protected).WithTimeout(quotaLifecycleLockTimeout)
}

// quotaLifecycleLockFile names the lifecycle lock file relative to the
// project root, as operator-facing messages print it.
func quotaLifecycleLockFile(provider string) string {
	return filepath.Join(paths.ProjectDirName(), quotaLifecycleLockPrefix+canonicalQuotaProvider(provider)+paths.LockSuffix)
}

// withQuotaLifecycleLock runs fn holding the provider's lifecycle lock.
func withQuotaLifecycleLock(projectRoot, provider string, fn func() error) error {
	return quotaLifecycleLock(projectRoot, provider).WithLockOperation(quotaLifecycleLockOperation, fn)
}

// quotaAdmissionWithoutLock answers the cases that need no coordination: no
// signal, or one still blocking. It reports false when the signal has
// expired, which only the locked lifecycle may decide.
func quotaAdmissionWithoutLock(projectRoot, provider string) (QuotaAdmissionResult, bool) {
	signal := readQuotaSignalSnapshot(projectRoot, provider, time.Now())
	switch {
	case !signal.exists:
		return QuotaAdmissionResult{Admission: QuotaClear}, true
	case signal.blocking:
		return QuotaAdmissionResult{Admission: QuotaRefused, Until: signal.expires}, true
	}
	return QuotaAdmissionResult{}, false
}

// quotaSignalSnapshot is the signal as one lifecycle decision reads it.
type quotaSignalSnapshot struct {
	exists     bool
	blocking   bool // unexpired, or unreadable (fail closed)
	expires    time.Time
	detected   string
	generation string
}

func readQuotaSignalSnapshot(projectRoot, provider string, now time.Time) quotaSignalSnapshot {
	path := QuotaSignalPath(projectRoot, provider)
	expires, exists, readable := readQuotaSignalState(path, now)
	if !exists {
		return quotaSignalSnapshot{}
	}
	snapshot := quotaSignalSnapshot{exists: true, expires: expires, blocking: !readable || now.Before(expires)}
	if data, err := os.ReadFile(path); err == nil {
		sum := sha256.Sum256(data)
		snapshot.generation = hex.EncodeToString(sum[:])
		snapshot.detected = parseRecord(string(data))[recordKeyDetected]
	} else {
		snapshot.blocking = true
	}
	return snapshot
}

func (s quotaSignalSnapshot) detectedText() string {
	if s.detected == "" {
		return "an unknown time"
	}
	return s.detected
}

// AdmitQuotaStart decides whether agentID may start a provider session on
// provider now. The launch gate calls it last, right before the start, so an
// admission is a start. An expired signal admits exactly one probe; the
// caller settles it with ResolveQuotaProbe or, on renewed exhaustion,
// RaiseQuotaExhaustion.
func AdmitQuotaStart(projectRoot, provider, agentID string) (QuotaAdmissionResult, error) {
	if result, decided := quotaAdmissionWithoutLock(projectRoot, provider); decided {
		return result, nil
	}
	var result QuotaAdmissionResult
	err := withQuotaLifecycleLock(projectRoot, provider, func() error {
		now := time.Now().UTC()
		signal := readQuotaSignalSnapshot(projectRoot, provider, now)
		switch {
		case !signal.exists:
			result = QuotaAdmissionResult{Admission: QuotaClear}
			return nil
		case signal.blocking:
			result = QuotaAdmissionResult{Admission: QuotaRefused, Until: signal.expires}
			return nil
		}
		if lease, live := readLiveQuotaLease(projectRoot, provider, now); live {
			if lease.holds(agentID) {
				result = QuotaAdmissionResult{Admission: QuotaProbe}
				return nil
			}
			if lease.Kind != quotaLeaseReservation {
				result = QuotaAdmissionResult{Admission: QuotaRefused, AttemptHolder: attemptHolderText(lease)}
				return nil
			}
		}
		probe := quotaLease{Kind: quotaLeaseProbe, Holder: agentID, PID: os.Getpid(), Acquired: now, Generation: signal.generation}
		if err := writeQuotaLease(projectRoot, provider, probe); err != nil {
			return fmt.Errorf("take quota recovery probe: %w", err)
		}
		// Not alerted: alerts are critical, and a probe starting is routine.
		// Its outcome is alerted; while it runs, resume and the spawn gate
		// name it.
		result = QuotaAdmissionResult{Admission: QuotaProbe}
		return nil
	})
	return result, err
}

// ReserveQuotaProbe holds the next recovery attempt for an agent a spawn is
// about to start on provider. QuotaClear needs no reservation; QuotaRefused
// means the provider is blocked or another attempt is coordinated.
func ReserveQuotaProbe(projectRoot, provider, role string) (QuotaAdmissionResult, error) {
	if result, decided := quotaAdmissionWithoutLock(projectRoot, provider); decided {
		return result, nil
	}
	var result QuotaAdmissionResult
	err := withQuotaLifecycleLock(projectRoot, provider, func() error {
		now := time.Now().UTC()
		signal := readQuotaSignalSnapshot(projectRoot, provider, now)
		switch {
		case !signal.exists:
			result = QuotaAdmissionResult{Admission: QuotaClear}
			return nil
		case signal.blocking:
			result = QuotaAdmissionResult{Admission: QuotaRefused, Until: signal.expires}
			return nil
		}
		if lease, live := readLiveQuotaLease(projectRoot, provider, now); live {
			result = QuotaAdmissionResult{Admission: QuotaRefused, Until: lease.until(), AttemptHolder: attemptHolderText(lease)}
			return nil
		}
		reservation := quotaLease{Kind: quotaLeaseReservation, Holder: quotaReservationHolderPrefix + role, PID: os.Getpid(), Acquired: now, Expires: now.Add(quotaReservationHold)}
		if err := writeQuotaLease(projectRoot, provider, reservation); err != nil {
			return fmt.Errorf("reserve quota recovery attempt: %w", err)
		}
		result = QuotaAdmissionResult{Admission: QuotaReserved}
		return nil
	})
	return result, err
}

// HandOverQuotaReservation records the started agent's process as the holder
// of the reservation this process took for role, so the reservation outlives
// a short-lived spawner and ends with the agent.
func HandOverQuotaReservation(projectRoot, provider, role string, agentPID int) error {
	return withQuotaLifecycleLock(projectRoot, provider, func() error {
		lease, live := readLiveQuotaLease(projectRoot, provider, time.Now())
		if !live || lease.Kind != quotaLeaseReservation || lease.Holder != quotaReservationHolderPrefix+role || lease.PID != os.Getpid() {
			return nil // converted, released or taken over meanwhile
		}
		lease.PID = agentPID
		return writeQuotaLease(projectRoot, provider, lease)
	})
}

// ReleaseQuotaReservation frees the reservation this process took for role
// when its spawn failed. pids are the processes the reservation may name:
// this one, and the agent it was handed over to.
func ReleaseQuotaReservation(projectRoot, provider, role string, pids ...int) error {
	return withQuotaLifecycleLock(projectRoot, provider, func() error {
		data, err := os.ReadFile(QuotaProbePath(projectRoot, provider))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		lease, parsed := parseQuotaLease(data)
		if !parsed || lease.Kind != quotaLeaseReservation || lease.Holder != quotaReservationHolderPrefix+role {
			return nil
		}
		for _, pid := range append(pids, os.Getpid()) {
			if lease.PID == pid {
				return removeQuotaLease(projectRoot, provider)
			}
		}
		return nil
	})
}

// ResolveQuotaProbe settles the probe agentID holds on provider. It is a no-op
// when agentID holds none, so a launch path may call it after every start.
// A provider answer clears the signal only if it is still the one the probe
// was admitted on: a newer exhaustion raised meanwhile keeps blocking.
func ResolveQuotaProbe(projectRoot, provider, agentID string, outcome QuotaProbeOutcome) error {
	if _, err := os.Stat(QuotaProbePath(projectRoot, provider)); os.IsNotExist(err) {
		return nil
	}
	return withQuotaLifecycleLock(projectRoot, provider, func() error {
		data, err := os.ReadFile(QuotaProbePath(projectRoot, provider))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		lease, parsed := parseQuotaLease(data)
		if !parsed || !lease.holds(agentID) {
			return nil
		}
		name := canonicalQuotaProvider(provider)
		signal := readQuotaSignalSnapshot(projectRoot, provider, time.Now())
		if outcome == QuotaProbeInconclusive {
			return errors.Join(removeQuotaLease(projectRoot, provider),
				LogAlert(projectRoot, alertLevel, alertCategoryQuotaProbeInconclusive,
					fmt.Sprintf("%s: %s's recovery probe ended without a provider answer; the next provider start probes again", name, agentID)))
		}
		if signal.exists && signal.generation != lease.Generation {
			return errors.Join(removeQuotaLease(projectRoot, provider),
				LogAlert(projectRoot, alertLevel, alertCategoryQuotaProbeSuperseded,
					fmt.Sprintf("%s: %s's recovery probe got a provider answer, but a newer quota block detected at %s still holds", name, agentID, signal.detectedText())))
		}
		if signal.exists {
			if err := os.Remove(QuotaSignalPath(projectRoot, provider)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("clear recovered quota signal: %w", err)
			}
		}
		return errors.Join(removeQuotaLease(projectRoot, provider),
			LogAlert(projectRoot, alertLevel, alertCategoryQuotaRecovered,
				fmt.Sprintf("%s: provider answered %s's recovery probe; the quota block detected at %s is lifted and spawns are allowed", name, agentID, signal.detectedText())))
	})
}

func attemptHolderText(lease quotaLease) string {
	if lease.Holder == "" {
		return "an unreadable recovery lease"
	}
	return fmt.Sprintf("%s by %s (pid %d)", lease.Kind, lease.Holder, lease.PID)
}

// liveQuotaProbe reports whether an expired signal's recovery probe runs.
func liveQuotaProbe(projectRoot, provider string, now time.Time) (quotaLease, bool) {
	lease, live := readLiveQuotaLease(projectRoot, provider, now)
	return lease, live && lease.Kind != quotaLeaseReservation
}

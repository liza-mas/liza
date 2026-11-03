package db

import (
	"crypto/sha256"
	"sync"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

// decodedStates memoizes decodeState by the SHA-256 of the published bytes.
// Every Read, Modify and snapshot reads the bytes as before, under the same
// lock; only decoding identical bytes again is skipped. A new publication is a
// new key, so a stale state is never served. Entries are private: callers get
// a CloneState copy, which they may mutate (terminal restore, liveness overlay
// and callbacks all do). Copying is about 20 times cheaper than parsing
// (BenchmarkReadCachedHit against BenchmarkStateParse). Failures are not
// cached, so each keeps its own operation name. Ceiling: past maxDecodedStates
// entries the memo is cleared; a project publishes one state at a time, and
// the bound keeps a long-lived process from retaining many large states.
var decodedStates = struct {
	sync.Mutex
	byHash map[[sha256.Size]byte]*models.State
}{byHash: map[[sha256.Size]byte]*models.State{}}

const maxDecodedStates = 8

func decodeState(data []byte, operation string) (*models.State, error) {
	key := sha256.Sum256(data)
	decodedStates.Lock()
	cached := decodedStates.byHash[key]
	decodedStates.Unlock()
	if cached != nil {
		return CloneState(cached), nil
	}

	var state models.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, &errors.StateSchemaError{Operation: operation, Err: err}
	}
	normalizeAgentRoles(&state)
	normalizeTaskAttempts(&state)

	decodedStates.Lock()
	if len(decodedStates.byHash) >= maxDecodedStates {
		clear(decodedStates.byHash)
	}
	decodedStates.byHash[key] = CloneState(&state)
	decodedStates.Unlock()
	return &state, nil
}

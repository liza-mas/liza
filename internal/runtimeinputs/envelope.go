// Package runtimeinputs holds the state-free mechanics of runtime-input
// provisioning (ADR-0169): the operator's KEY=VALUE envelope, the keyed
// identity of a materialization, and the recipe registry format. Nothing here
// reads the blackboard; errors never carry values, and the ones an agent can
// see never carry paths.
package runtimeinputs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Error classes. Unavailable is a read failure that says nothing about the
// materialization, so it never invalidates a ledger instance; Invalid is
// content the envelope rules refuse.
var (
	ErrUnavailable    = errors.New("runtime input artifact unavailable")
	ErrInvalid        = errors.New("runtime input envelope invalid")
	ErrKeyUnavailable = errors.New("runtime input key unavailable")
)

// MaxEnvelopeBytes bounds an envelope file.
const MaxEnvelopeBytes = 64 << 10

// MinSecretValueBytes is the shortest secret value accepted: declared-set
// masking replaces every occurrence of a value, so a very short one would
// shred unrelated output.
const MinSecretValueBytes = 8

var envelopeName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnvelope reads the KEY=VALUE envelope format used by provider env files,
// refusing every line that format would silently alter: surrounding
// whitespace, an inline " #" comment, a carriage return, an empty value or a
// repeated name. Blank lines and whole-line comments are allowed. What is
// accepted therefore parses to exactly the text the operator wrote.
func ParseEnvelope(data []byte) (map[string]string, error) {
	if len(data) > MaxEnvelopeBytes {
		return nil, fmt.Errorf("%w: envelope exceeds %d bytes", ErrInvalid, MaxEnvelopeBytes)
	}
	values := make(map[string]string)
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		number := i + 1
		if strings.ContainsAny(line, "\r\x00") || trimmed != line {
			return nil, fmt.Errorf("%w: line %d has surrounding whitespace, a carriage return or NUL", ErrInvalid, number)
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || !envelopeName.MatchString(name) {
			return nil, fmt.Errorf("%w: line %d is not NAME=VALUE", ErrInvalid, number)
		}
		if value == "" || strings.Contains(value, " #") {
			return nil, fmt.Errorf("%w: line %d (%s) has an empty value or an inline \" #\"", ErrInvalid, number, name)
		}
		if _, dup := values[name]; dup {
			return nil, fmt.Errorf("%w: line %d repeats %s", ErrInvalid, number, name)
		}
		values[name] = value
	}
	return values, nil
}

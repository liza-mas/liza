// Package oracle is the stdlib-only harness of the oracle suites under
// internal/oracle: normalisers, per-kind golden form and comparison, write
// mode, the child-environment policy and the CLI build/run helper.
//
// It imports nothing from this module outside its own tree, so it compiles
// against the run's starting commit.
package oracle

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Replacement tokens written by the normaliser.
const (
	TokenTime    = "<TIME>"
	TokenTmp     = "<TMP>"
	TokenHome    = "<HOME>"
	TokenProject = "<PROJECT>"
	TokenID      = "<ID>"
)

var (
	// Date-times with T or space separator, optional fraction, optional Z or
	// numeric offset (with or without colon, optionally after a space).
	timestampRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z| ?[+-]\d{2}:?\d{2})?`)
	uuidRE      = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	hexIDRE     = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{64}|[0-9a-f]{40})\b`)
	ansiRE      = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
)

type dirRepl struct{ dir, token string }

type literalRepl struct{ value, token string }

// Normaliser replaces the volatile parts of an output with fixed tokens:
// timestamps, registered directories, hex and UUID identifiers and
// driver-registered literal values. Normalising a normalised value is a
// no-op.
type Normaliser struct {
	dirs     []dirRepl
	literals []literalRepl
	keptSHAs map[string]bool
}

// NewNormaliser returns a normaliser that already maps the system temporary
// directory to <TMP>.
func NewNormaliser() *Normaliser {
	n := &Normaliser{}
	n.Dir(os.TempDir(), TokenTmp)
	return n
}

// Dir registers a directory to be replaced by token. The slash form, the
// native form and the symlink-resolved form are all matched, longest first.
func (n *Normaliser) Dir(dir, token string) {
	if dir == "" {
		return
	}
	forms := map[string]bool{}
	add := func(d string) {
		d = filepath.Clean(d)
		forms[d] = true
		forms[filepath.ToSlash(d)] = true
	}
	add(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		add(resolved)
	}
	for f := range forms {
		n.dirs = append(n.dirs, dirRepl{dir: f, token: token})
	}
	sort.SliceStable(n.dirs, func(i, j int) bool {
		if len(n.dirs[i].dir) != len(n.dirs[j].dir) {
			return len(n.dirs[i].dir) > len(n.dirs[j].dir)
		}
		return n.dirs[i].dir < n.dirs[j].dir
	})
}

// Literal registers a driver-chosen volatile value to be replaced by token.
func (n *Normaliser) Literal(value, token string) {
	if value == "" {
		return
	}
	n.literals = append(n.literals, literalRepl{value: value, token: token})
	sort.SliceStable(n.literals, func(i, j int) bool {
		return len(n.literals[i].value) > len(n.literals[j].value)
	})
}

// KeepSHA preserves this exact spelling during hex-ID normalisation. Only
// complete 40- or 64-character ASCII hex strings are accepted; an invalid
// registration leaves the allowlist unchanged. Repeated registration is a no-op.
// Register before use: concurrent reads are safe, concurrent mutation is not.
// Earlier Dir and Literal substitutions still apply.
func (n *Normaliser) KeepSHA(sha string) error {
	if (len(sha) != 40 && len(sha) != 64) || hexIDRE.FindString(sha) != sha {
		return errors.New("oracle: SHA must be exactly 40 or 64 ASCII hex characters")
	}
	if n.keptSHAs == nil {
		n.keptSHAs = make(map[string]bool)
	}
	n.keptSHAs[sha] = true
	return nil
}

// Text normalises s, retaining explicitly registered SHAs verbatim.
func (n *Normaliser) Text(s string) string {
	for _, d := range n.dirs {
		s = replaceDir(s, d.dir, d.token)
	}
	for _, l := range n.literals {
		s = strings.ReplaceAll(s, l.value, l.token)
	}
	s = timestampRE.ReplaceAllString(s, TokenTime)
	s = uuidRE.ReplaceAllString(s, TokenID)
	return hexIDRE.ReplaceAllStringFunc(s, func(sha string) string {
		if n.keptSHAs[sha] {
			return sha
		}
		return TokenID
	})
}

// SortLines sorts the lines of s, for outputs whose line order is not
// asserted. A trailing newline is kept.
func SortLines(s string) string {
	trailing := strings.HasSuffix(s, "\n")
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	sort.Strings(lines)
	out := strings.Join(lines, "\n")
	if trailing {
		out += "\n"
	}
	return out
}

func isNameByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isPathByte(b byte) bool { return isNameByte(b) || b == '/' || b == '\\' }

// replaceDir replaces whole-path occurrences of dir by token and rewrites the
// separators of the path tail to slashes, so goldens agree across platforms.
// An occurrence is whole when it is not glued to a name character on either
// side (a trailing dot ends a sentence, not a name).
func replaceDir(s, dir, token string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, dir)
		if i < 0 {
			break
		}
		end := i + len(dir)
		glued := (i > 0 && isNameByte(s[i-1])) || (end < len(s) && s[end] != '.' && isNameByte(s[end]))
		if glued {
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(token)
		tail := end
		for tail < len(s) && isPathByte(s[tail]) {
			tail++
		}
		b.WriteString(strings.ReplaceAll(s[end:tail], `\`, "/"))
		s = s[tail:]
	}
	b.WriteString(s)
	return b.String()
}

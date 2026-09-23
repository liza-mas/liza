package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
)

// catFileObject is one answer from a `git cat-file --batch[-check]` session.
// Missing reports that the name did not resolve to an object (git prints
// "<name> missing", or "ambiguous" for an ambiguous short name); the other
// fields are then empty.
type catFileObject struct {
	Name    string
	OID     string
	Type    string
	Size    int64
	Content []byte
	Missing bool
	Status  string
}

// catFileBatchable reports whether name can be sent as one line on a
// cat-file batch stdin. Git reads the whole line as the object name, so a
// name containing a line terminator cannot be expressed; callers fall back to
// argument-based lookups for those.
func catFileBatchable(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\n\r\x00")
}

// catFileBatch resolves every name in one short-lived `git cat-file` process
// instead of one rev-parse/cat-file process per question. Names are read from
// stdin, so they never reach git's option parser. withContent selects --batch
// (header plus raw object bytes) over --batch-check (header only).
//
// The result has exactly one entry per name, in order. Output that is
// truncated or does not match the documented batch format is an error rather
// than a partial answer.
func (g *Git) catFileBatch(names []string, withContent bool) ([]catFileObject, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var input strings.Builder
	for _, name := range names {
		if !catFileBatchable(name) {
			return nil, fmt.Errorf("cat-file batch cannot express object name %q", name)
		}
		input.WriteString(name)
		input.WriteByte('\n')
	}
	mode := "--batch-check"
	if withContent {
		mode = "--batch"
	}
	output, err := gitenv.OutputWithStdin(g.projectRoot, input.String(), "cat-file", mode)
	if err != nil {
		// Output() keeps git's stderr only on the exit error; surface it so a
		// failure reads like the rev-parse/cat-file errors it replaces.
		var stderr []byte
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = bytes.TrimSpace(exitErr.Stderr)
		}
		return nil, fmt.Errorf("git cat-file %s failed: %w\nStderr: %s\nOutput: %s", mode, err, stderr, output)
	}
	return parseCatFileBatch(names, output, withContent)
}

func parseCatFileBatch(names []string, output []byte, withContent bool) ([]catFileObject, error) {
	results := make([]catFileObject, 0, len(names))
	rest := output
	for _, name := range names {
		newline := bytes.IndexByte(rest, '\n')
		if newline < 0 {
			return nil, fmt.Errorf("cat-file batch output truncated before the answer for %q", name)
		}
		header := string(rest[:newline])
		rest = rest[newline+1:]

		// "<name> missing" / "<name> ambiguous" carry the requested name, which
		// may itself contain spaces, so match on the suffix before splitting.
		if status, ok := unresolvedBatchStatus(header, name); ok {
			results = append(results, catFileObject{Name: name, Missing: true, Status: status})
			continue
		}

		fields := strings.Fields(header)
		if len(fields) == 2 && fields[1] == "submodule" {
			// A gitlink whose commit is not in this repository.
			results = append(results, catFileObject{Name: name, OID: fields[0], Missing: true, Status: "submodule"})
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed cat-file batch header for %q: %q", name, header)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("malformed cat-file batch size for %q: %q", name, header)
		}
		object := catFileObject{Name: name, OID: fields[0], Type: fields[1], Size: size}
		if withContent {
			if int64(len(rest)) < size+1 {
				return nil, fmt.Errorf("cat-file batch output truncated inside the content of %q", name)
			}
			object.Content = rest[:size]
			if rest[size] != '\n' {
				return nil, fmt.Errorf("cat-file batch content for %q is not newline-terminated", name)
			}
			rest = rest[size+1:]
		}
		results = append(results, object)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("cat-file batch produced %d unexpected trailing bytes", len(rest))
	}
	return results, nil
}

func unresolvedBatchStatus(header, name string) (string, bool) {
	for _, status := range []string{"missing", "ambiguous", "excluded"} {
		if header == name+" "+status {
			return status, true
		}
	}
	return "", false
}

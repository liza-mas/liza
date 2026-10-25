package supplement

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Git's rebase transcript is Git's own output: progress, hint wording,
// abbreviations and the final line differ between Git versions. A case that
// records one in a persisted diagnostic opts in with a gitTranscript spec; the
// state text is compared with every transcript replaced by one token, after
// the facts production and the fixture imply are asserted on the raw text.

const (
	// transcriptOpen ends the production-owned text before Git's output: the
	// prefix the git package gives a rebase conflict.
	transcriptOpen = "rebase conflict: "
	// transcriptClose starts the production-owned text after it: the heal's
	// closing of the reason and its repair instruction.
	transcriptClose    = "); repair: "
	gitTranscriptToken = "<git-rebase-transcript>"
)

// gitTranscript is a case's opt-in: the path the fixture conflicts on and how
// many persisted copies of the transcript the state holds.
type gitTranscript struct {
	Path   string `json:"path"`
	Copies int    `json:"copies"`
}

var (
	quotedScalarRE = regexp.MustCompile(`^(\s*(?:-\s+)?[\w-]+:\s+)"((?:[^"\\]|\\.)*)"\s*$`)
	blockScalarRE  = regexp.MustCompile(`^(\s*(?:-\s+)?[\w-]+:\s+)\|[-+]?\s*$`)
	quoteEscaper   = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
)

// maskGitTranscript replaces, in every double-quoted or literal-block scalar
// holding a rebase conflict, the text between transcriptOpen and
// transcriptClose with gitTranscriptToken, in the double-quoted form. Before
// masking, each transcript must carry the classification marker production
// reads (CONFLICT or could not apply) and name the conflicted path, and the
// state must hold exactly spec.Copies of them; failures name the missing fact,
// never Git output. Text outside those scalars is returned unchanged.
func maskGitTranscript(text string, spec gitTranscript) (string, error) {
	lines := strings.Split(text, "\n")
	var out []string
	masked := 0
	for i := 0; i < len(lines); i++ {
		if m := quotedScalarRE.FindStringSubmatch(lines[i]); m != nil {
			if value, ok, err := maskTranscriptValue(m[2], spec, func(s string) string { return s }); err != nil {
				return "", err
			} else if ok {
				out = append(out, m[1]+`"`+value+`"`)
				masked++
				continue
			}
		}
		if m := blockScalarRE.FindStringSubmatch(lines[i]); m != nil {
			indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
			end := i + 1
			for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || len(lines[end])-len(strings.TrimLeft(lines[end], " ")) > indent) {
				end++
			}
			var body []string
			for _, l := range lines[i+1 : end] {
				body = append(body, strings.TrimLeft(l, " "))
			}
			if value, ok, err := maskTranscriptValue(strings.Join(body, "\n"), spec, quoteEscaper.Replace); err != nil {
				return "", err
			} else if ok {
				out = append(out, m[1]+`"`+value+`"`)
				masked++
				i = end - 1
				continue
			}
		}
		out = append(out, lines[i])
	}
	if masked != spec.Copies {
		return "", fmt.Errorf("found %d rebase conflict transcripts in the state text, want %d", masked, spec.Copies)
	}
	return strings.Join(out, "\n"), nil
}

// maskTranscriptValue masks one scalar value; ok is false for a scalar that
// holds no transcript. escape renders the production-owned parts into the
// double-quoted form (a quoted scalar is already escaped).
func maskTranscriptValue(value string, spec gitTranscript, escape func(string) string) (string, bool, error) {
	open := strings.Index(value, transcriptOpen)
	if open < 0 {
		return "", false, nil
	}
	start := open + len(transcriptOpen)
	length := strings.Index(value[start:], transcriptClose)
	if length < 0 {
		return "", false, nil
	}
	end := start + length
	if err := checkTranscript(value[start:end], spec.Path); err != nil {
		return "", false, err
	}
	return escape(value[:start]) + gitTranscriptToken + escape(value[end:]), true, nil
}

// checkTranscript asserts the facts of a rebase conflict that production and
// the fixture imply, and nothing else about Git's wording.
func checkTranscript(remainder, path string) error {
	if !strings.Contains(remainder, "CONFLICT") && !strings.Contains(remainder, "could not apply") {
		return errors.New("rebase conflict transcript lacks the CONFLICT or could not apply marker production classifies on")
	}
	if !strings.Contains(remainder, path) {
		return fmt.Errorf("rebase conflict transcript does not name the conflicted path %s", path)
	}
	return nil
}

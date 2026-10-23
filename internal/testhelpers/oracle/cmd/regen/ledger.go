package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const ledgerPath = "internal/oracle/PROVENANCE"

type change struct{ golden, reason string }
type ledger struct {
	start   string
	fixes   []string
	changes []change
}

var commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func parseLedger(data []byte) (ledger, error) {
	var result ledger
	text := strings.TrimSuffix(string(data), "\n")
	for i, line := range strings.Split(text, "\n") {
		fields := strings.Split(line, " ")
		bad := func() (ledger, error) { return ledger{}, fmt.Errorf("ledger line %d: invalid record", i+1) }
		if len(fields) < 2 || (i == 0 && fields[0] != "start") {
			return bad()
		}
		switch fields[0] {
		case "start":
			if i != 0 || len(fields) != 2 || !commitSHA.MatchString(fields[1]) {
				return bad()
			}
			result.start = fields[1]
		case "fix":
			if len(fields) != 2 || !commitSHA.MatchString(fields[1]) {
				return bad()
			}
			result.fixes = append(result.fixes, fields[1])
		case "changed":
			if len(fields) != 3 || !goldenPath(fields[1]) || !validReason(fields[2]) {
				return bad()
			}
			result.changes = append(result.changes, change{fields[1], fields[2]})
		default:
			return bad()
		}
	}
	return result, nil
}

func validReason(reason string) bool {
	return isC(reason) || reason == "determinism" || reason == "fix"
}

func isC(reason string) bool {
	return len(reason) == 2 && reason[0] == 'C' && reason[1] >= '1' && reason[1] <= '6'
}

func goldenPath(name string) bool {
	if path.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n\t ") || !strings.HasPrefix(name, "internal/oracle/") {
		return false
	}
	prefix, suffix, ok := strings.Cut(strings.TrimPrefix(name, "internal/oracle/"), "/testdata/golden/")
	if !ok || prefix == "" {
		return false
	}
	kind, file, ok := strings.Cut(suffix, "/")
	if !ok || file == "" {
		return false
	}
	switch kind {
	case "prompt", "artifact", "json", "human", "state":
		return true
	}
	return false
}

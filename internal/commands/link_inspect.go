package commands

import (
	"fmt"
	"os"
)

// linkStatus is what a path holds relative to the link a command manages there.
type linkStatus int

const (
	// linkAbsent: nothing exists at the path.
	linkAbsent linkStatus = iota
	// linkLinked: a symlink whose target is exactly the managed target.
	linkLinked
	// linkElsewhere: a symlink to another target.
	linkElsewhere
	// linkOccupied: a regular file, a directory or another non-symlink entry.
	linkOccupied
)

// linkState is the read-only observation of a managed link location. Kind is
// "symlink" for linkElsewhere and "file" or "directory" for linkOccupied;
// Target is the existing symlink target for linkElsewhere.
type linkState struct {
	Status linkStatus
	Kind   string
	Target string
}

// inspectLink reports what path holds relative to a symlink to target,
// without changing anything. Placement, setup and doctor share it, so the
// commands that create links and the command that checks them read a location
// the same way. The comparison is the raw link text, as the creating code
// writes it.
func inspectLink(path, target string) (linkState, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return linkState{Status: linkAbsent}, nil
	}
	if err != nil {
		return linkState{}, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		return linkState{Status: linkOccupied, Kind: kind}, nil
	}
	existing, err := os.Readlink(path)
	if err != nil {
		return linkState{}, fmt.Errorf("failed to read symlink %s: %w", path, err)
	}
	if existing == target {
		return linkState{Status: linkLinked}, nil
	}
	return linkState{Status: linkElsewhere, Kind: "symlink", Target: existing}, nil
}

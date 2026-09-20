package git

import (
	"bytes"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
)

// gitPatchID feeds a diff to `git patch-id --verbatim`, which reads only stdin
// and so cannot be expressed through the shared exec helpers.
//
// --verbatim (git >= 2.39) hashes whitespace as written. --stable would treat
// a commit differing from the reviewed one only in whitespace as identical,
// and a repair that rewrites approval evidence should not be the layer that
// decides whitespace is immaterial. Blob equality at the acceptance check is a
// second, independent boundary; this keeps the first one exact.
func gitPatchID(projectRoot, diff string) (string, error) {
	cmd := gitenv.Command("patch-id", "--verbatim")
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(diff)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

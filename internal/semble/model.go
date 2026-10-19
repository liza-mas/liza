package semble

import (
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// IsModelUnavailableOffline reports whether a Semble command's stderr shows
// huggingface_hub refusing to fetch an uncached model in offline mode, as
// after a Semble upgrade changes its default model. It reads stderr only and
// matches only the library's own exception and environment names: corpus
// queries print the user's source on stdout, where words such as "offline"
// are ordinary. A reworded library message only loses this classification.
func IsModelUnavailableOffline(stderr string) bool {
	text := strings.ToLower(stderr)
	return strings.Contains(text, "hf_hub_offline") || strings.Contains(text, "localentrynotfounderror")
}

// ModelRecoveryCommand is the operator command that prewarms Semble's model.
func ModelRecoveryCommand() string {
	return brand.Command("toolchain", "install", "--tool", sembleExecutableName, "--yes")
}

// ValidationFixtureCommand writes the package-owned validation fixture outside
// any project and returns the Semble arguments that query it, so callers with
// their own command runner prewarm or check the model the same way.
func ValidationFixtureCommand() (dir string, args []string, cleanup func(), err error) {
	fixture := DefaultValidationFixtureIdentity()
	dir, cleanup, err = createValidationFixture("", fixture)
	if err != nil {
		return "", nil, cleanup, err
	}
	args = []string{"search", fixture.Query, dir, "--top-k", strconv.Itoa(fixture.TopK), "--content", fixture.ContentMode}
	return dir, args, cleanup, nil
}

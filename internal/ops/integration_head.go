package ops

import (
	"fmt"

	gitpkg "github.com/liza-mas/liza/internal/git"
)

// ResolveIntegrationHEAD resolves integrationBranch only as a local branch ref.
// An empty branch is invalid; callers must not substitute an implicit default
// because config.integration_branch is a required state field.
func ResolveIntegrationHEAD(projectRoot, integrationBranch string) (string, error) {
	if integrationBranch == "" {
		return "", fmt.Errorf("integration branch is empty")
	}
	return gitpkg.New(projectRoot).GetCommitSHA("refs/heads/" + integrationBranch)
}

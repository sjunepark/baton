package workflow

import (
	"strings"

	gitadapter "github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/repository"
)

type repositoryIdentityInput struct {
	source string
	value  string
}

// resolveRepositoryIdentities validates non-checkout repository identities.
// Event-driven workflows cannot infer a local remote reliably, but they must
// still reject conflicting flags, environment, and event payloads.
func resolveRepositoryIdentities(inputs ...repositoryIdentityInput) (string, error) {
	identities := make([]repositoryIdentityInput, 0, len(inputs))
	for _, identity := range inputs {
		identity.value = strings.TrimSpace(identity.value)
		if identity.value == "" {
			continue
		}
		normalized, err := gitadapter.NormalizeRepositoryName(identity.value)
		if err != nil {
			return "", classifyRepositoryError(&repository.ResolveError{
				Code: repository.ErrorInvalidRepository, LeftSource: identity.source, Left: identity.value, Cause: err,
			})
		}
		identity.value = normalized
		identities = append(identities, identity)
	}
	if len(identities) == 0 {
		return "", classifyRepositoryError(&repository.ResolveError{Code: repository.ErrorMissingRepository})
	}
	for _, identity := range identities[1:] {
		if !strings.EqualFold(identities[0].value, identity.value) {
			return "", classifyRepositoryError(&repository.ResolveError{
				Code:       repository.ErrorRepositoryMismatch,
				LeftSource: identities[0].source, Left: identities[0].value,
				RightSource: identity.source, Right: identity.value,
			})
		}
	}
	return identities[0].value, nil
}

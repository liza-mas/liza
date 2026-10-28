package agent

import (
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

var (
	buildSemblePromptMetadata          = semble.BuildPromptMetadata
	scipAvailableIndexes               = scipsearch.AvailableIndexes
	scipAvailableProjectRootIndexes    = scipsearch.AvailableProjectRootIndexes
	stacklitAvailableIndexes           = stacklit.AvailableIndexes
	functionalClustersAvailableIndexes = functionalclusters.AvailableIndexes
)

func toPromptStacklitIndexRefs(indexes []stacklit.IndexRef) []prompts.StacklitIndexRef {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.StacklitIndexRef, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.StacklitIndexRef{Path: index.Path})
	}
	return refs
}

func toPromptFunctionalClusterIndexRefs(indexes []functionalclusters.IndexRef) []prompts.FunctionalClusterIndexRef {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.FunctionalClusterIndexRef, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.FunctionalClusterIndexRef{Path: index.Path})
	}
	return refs
}

func toPromptScipIndexRefs(indexes []scipsearch.IndexRef) []prompts.ScipIndexRef {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.ScipIndexRef, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.ScipIndexRef{
			Language: string(index.Language),
			Path:     index.Path,
		})
	}
	return refs
}

func toBasePromptScipSearchIndexes(indexes []prompts.ScipIndexRef) []prompts.ScipSearchIndex {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.ScipSearchIndex, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.ScipSearchIndex{
			Language:  index.Language,
			IndexPath: index.Path,
		})
	}
	return refs
}

func toBasePromptStacklitIndexes(indexes []prompts.StacklitIndexRef) []prompts.StacklitIndex {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.StacklitIndex, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.StacklitIndex{
			IndexPath: index.Path,
		})
	}
	return refs
}

func toBasePromptFunctionalClusterIndexes(indexes []prompts.FunctionalClusterIndexRef) []prompts.FunctionalClusterIndex {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.FunctionalClusterIndex, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.FunctionalClusterIndex{
			IndexPath: index.Path,
		})
	}
	return refs
}

func availablePromptScipIndexRefs(state *models.State, targetRoot string, available func(scipsearch.RuntimePlanOptions) ([]scipsearch.IndexRef, error)) []prompts.ScipIndexRef {
	if targetRoot == "" || !scipsearch.RuntimeEnabled(state.Config.ScipSearch) {
		return nil
	}
	availableIndexes, err := available(scipsearch.RuntimePlanOptions{
		TargetRoot:          targetRoot,
		ConfiguredLanguages: state.Config.ScipSearch,
	})
	if err != nil {
		return nil
	}
	return toPromptScipIndexRefs(availableIndexes)
}

func availablePromptStacklitIndexRefs(targetRoot string) []prompts.StacklitIndexRef {
	if targetRoot == "" || !stacklit.RuntimeEnabled() {
		return nil
	}
	availableIndexes, err := stacklitAvailableIndexes(stacklit.RuntimePlanOptions{
		TargetRoot: targetRoot,
	})
	if err != nil {
		return nil
	}
	return toPromptStacklitIndexRefs(availableIndexes)
}

func availablePromptFunctionalClusterIndexRefs(targetRoot string) []prompts.FunctionalClusterIndexRef {
	if targetRoot == "" || !functionalclusters.RuntimeEnabled() {
		return nil
	}
	availableIndexes, err := functionalClustersAvailableIndexes(functionalclusters.RuntimePlanOptions{
		TargetRoot: targetRoot,
	})
	if err != nil {
		return nil
	}
	return toPromptFunctionalClusterIndexRefs(availableIndexes)
}

func availablePromptSembleSearchMetadata(targetRoot string, kind semble.TargetKind) prompts.SembleSearchMetadata {
	if targetRoot == "" {
		return prompts.SembleSearchMetadata{}
	}
	opts := semble.PromptMetadataOptions{
		Kind:       kind,
		TargetRoot: targetRoot,
	}
	if kind == semble.TargetKindTaskWorktree {
		opts.ExpectedWorktreeRoot = targetRoot
	}
	metadata, ok := buildSemblePromptMetadata(opts)
	if !ok {
		return prompts.SembleSearchMetadata{}
	}
	return prompts.SembleSearchMetadata{
		TargetRoot:      metadata.TargetRoot,
		ShellTargetRoot: metadata.ShellTargetRoot,
	}
}

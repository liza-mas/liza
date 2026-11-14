package embedded

import "testing"

func TestCodeReviewSkillConceptDefinitionCheck(t *testing.T) {
	_, items := codeReviewAnswerOrFinding(t)
	record := codeReviewItemIndex(t, "record substance", items, codeReviewRecordLink, false)
	concept := codeReviewItemIndex(t, "concept definition", items, "shared concept", true)
	if concept != record+1 || concept != len(items)-1 {
		t.Errorf("order: want concept definition directly after record substance as the last item, got concept=%d record=%d of %d", concept, record, len(items))
	}
	requireReviewFragments(t, "concept definition", foldReviewText(items[concept]), []string{
		"writer", "kind of content", "directory", "file", "flag", "state", "record",
		"written definition", "no later than this change",
		"holds and does not", "who writes it", "how long its content lives",
		"name", "property", "writers must respect", "storage", "lifespan", "not only the owner",
		"no longer matches", "finding",
	})
}

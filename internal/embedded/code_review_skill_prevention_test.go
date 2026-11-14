package embedded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	codeReviewChecklistHeading = "# Review Checklist"
	codeReviewFeedbackHeading  = "# Feedback Format"
	codeReviewLastExistingItem = "Mock-heavy implementation-testing flagged"
	codeReviewRecordLink       = "../shared/references/prevention-record.md"
)

// foldReviewText lowercases, turns hyphens into spaces and collapses whitespace,
// so fragments survive line wrapping and hyphenation changes.
func foldReviewText(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.ToLower(s), "-", " ")), " ")
}

// codeReviewAnswerOrFinding returns the lead (folded) and the items (whitespace-
// normalized, unfolded) appended to the Review Checklist after its existing last item.
func codeReviewAnswerOrFinding(t *testing.T) (lead string, items []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("read code-review skill: %v", err)
	}
	text := string(raw)
	start := strings.Index(text, "\n"+codeReviewChecklistHeading+"\n")
	end := strings.Index(text, "\n"+codeReviewFeedbackHeading+"\n")
	if start < 0 || end < 0 || start >= end {
		t.Fatalf("code-review skill: %q must precede %q as heading lines", codeReviewChecklistHeading, codeReviewFeedbackHeading)
	}
	segments := strings.Split(text[start:end], "- [ ] ")
	for i, segment := range segments {
		normalized := strings.Join(strings.Fields(segment), " ")
		_, rest, found := strings.Cut(normalized, codeReviewLastExistingItem)
		if !found {
			continue
		}
		for _, item := range segments[i+1:] {
			items = append(items, strings.Join(strings.Fields(item), " "))
		}
		return foldReviewText(rest), items
	}
	t.Fatalf("code-review skill: Review Checklist lacks its existing last item %q", codeReviewLastExistingItem)
	return "", nil
}

// codeReviewItemIndex returns the index of the single item holding anchor.
func codeReviewItemIndex(t *testing.T, check string, items []string, anchor string, fold bool) int {
	t.Helper()
	if fold {
		anchor = foldReviewText(anchor)
	}
	found := -1
	for i, item := range items {
		haystack := item
		if fold {
			haystack = foldReviewText(item)
		}
		if !strings.Contains(haystack, anchor) {
			continue
		}
		if found >= 0 {
			t.Fatalf("%s: anchor %q is held by more than one new item", check, anchor)
		}
		found = i
	}
	if found < 0 {
		t.Fatalf("%s: no new Review Checklist item holds anchor %q", check, anchor)
	}
	return found
}

func requireReviewFragments(t *testing.T, check, folded string, fragments []string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(folded, foldReviewText(fragment)) {
			t.Errorf("%s: missing fragment %q", check, fragment)
		}
	}
}

func TestCodeReviewSkillAnswerOrFindingLead(t *testing.T) {
	lead, items := codeReviewAnswerOrFinding(t)
	if len(items) == 0 {
		t.Fatal("lead: no new Review Checklist item follows it")
	}
	// The lead is the text before the first new bullet.
	if !strings.Contains(lead, "unanswered") {
		t.Fatalf("lead: missing anchor %q", "unanswered")
	}
	requireReviewFragments(t, "lead", lead, []string{"finding", "commit message", "reply", "feedback format"})
}

func TestCodeReviewSkillOpensCheck(t *testing.T) {
	_, items := codeReviewAnswerOrFinding(t)
	i := codeReviewItemIndex(t, "opens", items, "representation", true)
	requireReviewFragments(t, "opens", foldReviewText(items[i]),
		[]string{"fix", "state", "path", "file", "flag", "reader", "writer"})
}

func TestCodeReviewSkillLosesCheck(t *testing.T) {
	_, items := codeReviewAnswerOrFinding(t)
	i := codeReviewItemIndex(t, "loses", items, "cherry pick", true)
	requireReviewFragments(t, "loses", foldReviewText(items[i]), []string{
		"merge", "rebase", "conflict resolution", "exactly once", "lost", "duplicated",
		"range diff", "patch identity", "git protocol",
	})
}

func TestCodeReviewSkillRecordSubstanceCheck(t *testing.T) {
	_, items := codeReviewAnswerOrFinding(t)
	opens := codeReviewItemIndex(t, "opens", items, "representation", true)
	loses := codeReviewItemIndex(t, "loses", items, "cherry pick", true)
	record := codeReviewItemIndex(t, "record substance", items, codeReviewRecordLink, false)
	if !(opens < loses && loses < record) {
		t.Errorf("order: want opens < loses < record substance, got %d, %d, %d", opens, loses, record)
	}
	requireReviewFragments(t, "record substance", foldReviewText(items[record]), []string{
		"fix", "gate", "system level", "removed", "boundary", "negative test", "sibling",
		"disposition", "scheduled", "type",
	})
}

func TestCodeReviewSkillCitesPreventionRecord(t *testing.T) {
	_, items := codeReviewAnswerOrFinding(t)
	codeReviewItemIndex(t, "record citation", items, codeReviewRecordLink, false)

	root := findRepoRoot(t)
	target := filepath.Join(root, "skills", "code-review", codeReviewRecordLink)
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("record link %q does not resolve to a regular file from the skill (err=%v)", codeReviewRecordLink, err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("read code-review skill: %v", err)
	}
	if strings.Contains(string(raw), "Prevention-") {
		t.Error("code-review skill must cite the record, not restate its trailer keys (found \"Prevention-\")")
	}
}

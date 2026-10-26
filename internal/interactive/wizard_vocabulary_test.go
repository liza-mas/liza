package interactive

import "testing"

func TestWizardModeValuesKeepFormerSpelling(t *testing.T) {
	if WizardModePairing != "pairing" || WizardModeFull != "full" {
		t.Fatalf("wizard mode spelling changed")
	}
}

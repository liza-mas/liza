package agentlaunch

import "testing"

// The template delimiters were literals before the K7 sweep; they must stay the
// pair templateExprRE matches, and keep their spelling.
func TestA9TemplateDelimitersKeepSpelling(t *testing.T) {
	if templateExprOpen != "{{" || templateExprClose != "}}" {
		t.Fatalf("delimiters = %q %q, want {{ }}", templateExprOpen, templateExprClose)
	}
	if !templateExprRE.MatchString(templateExprOpen + "name" + templateExprClose) {
		t.Fatal("templateExprRE does not match the named delimiter pair")
	}
}

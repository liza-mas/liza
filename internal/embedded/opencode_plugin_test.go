package embedded

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestOpenCodeResultPluginInstallAndRefresh(t *testing.T) {
	root := t.TempDir()
	if err := WriteOpenCodeResultPlugin(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".opencode", "plugins", brand.BinaryName+"-tool-result-budget.ts")
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, OpenCodeResultPluginContent()) {
		t.Fatal("plugin content differs")
	}
	if bytes.Contains(actual, []byte("__BRAND_")) {
		t.Fatal("unrendered brand")
	}
	if err := WriteOpenCodeResultPlugin(root); err != nil {
		t.Fatal(err)
	}
}
func TestOpenCodeResultPluginPreservesUserFileAndSymlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".opencode", "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, brand.BinaryName+"-tool-result-budget.ts")
	user := []byte("export const UserPlugin = async () => ({})\n")
	if err := os.WriteFile(path, user, 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteOpenCodeResultPlugin(root); err == nil {
		t.Fatal("unowned collision silently accepted")
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, user) {
		t.Fatal("user plugin overwritten")
	}
	other := t.TempDir()
	root2 := t.TempDir()
	if err := os.Mkdir(filepath.Join(root2, ".opencode"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root2, ".opencode", "plugins")); err != nil {
		t.Skip(err)
	}
	if err := WriteOpenCodeResultPlugin(root2); err == nil {
		t.Fatal("symlink directory accepted")
	}
	entries, _ := os.ReadDir(other)
	if len(entries) != 0 {
		t.Fatal("outside directory modified")
	}
}

func TestOpenCodeExecActivationAlsoInstallsPluginForUserExec(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".opencode", "tools")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	user := []byte("export default userOwnedExec\n")
	path := filepath.Join(dir, "exec.ts")
	if err := os.WriteFile(path, user, 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteOpenCodeExecTool(root); err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, user) {
		t.Fatal("user exec overwritten")
	}
	plugin, err := os.ReadFile(filepath.Join(root, ".opencode", "plugins", brand.BinaryName+"-tool-result-budget.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plugin, OpenCodeResultPluginContent()) {
		t.Fatal("native plugin missing")
	}
}

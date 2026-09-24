package embedded

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/brand"
)

//go:embed opencode-plugins/tool-result-budget.ts
var openCodeResultPlugin []byte

func OpenCodeResultPluginContent() []byte { return renderEmbeddedAsset(openCodeResultPlugin) }

// WriteOpenCodeResultPlugin installs the native return hook additively. A name
// collision is a visible activation error, never an overwritten user plugin.
func WriteOpenCodeResultPlugin(projectRoot string) error {
	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, dir := range []string{".opencode", filepath.Join(".opencode", "plugins")} {
		info, err := root.Lstat(dir)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("OpenCode plugin directory must not be a symlink: %s", dir)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := root.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	name := filepath.Join(".opencode", "plugins", brand.BinaryName+"-tool-result-budget.ts")
	content := OpenCodeResultPluginContent()
	header := bytes.SplitN(content, []byte("\n"), 2)[0]
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("OpenCode budget plugin must be a regular managed file")
		}
		existing, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(existing, header) {
			return errors.New("OpenCode budget plugin conflicts with a user-owned file; existing file preserved")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Rename a new inode rather than following a destination symlink on update.
	temp := name + "." + rand.Text() + ".tmp"
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, name)
}

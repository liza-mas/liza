//go:build linux

package artifact

import (
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
	"github.com/liza-mas/liza/internal/embedded"
)

func renderedFiles(root string, values map[string]string) ([]fileEntry, error) {
	files, err := brandrender.ExpectedEmbeddedFiles(root, brand.ValuesFromEnv(func(key string) string { return values[key] }))
	if err != nil {
		return nil, err
	}
	entries := make([]fileEntry, 0, len(files))
	for _, file := range files {
		// Git preserves the executable class, not the checkout's umask.
		mode := file.Mode.Perm() & 0o111
		if mode != 0 {
			mode = 0o755
		} else {
			mode = 0o644
		}
		entries = append(entries, fileEntry{file.RelPath, mode, file.Content})
	}
	return entries, nil
}

func writeGlobal(target string) error {
	_, err := embedded.WriteGlobalFiles(target, nil)
	return err
}

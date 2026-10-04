// Package atomicfile publishes file copies so readers never observe a partial
// destination file.
package atomicfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Copy copies src to dst through a temporary file in dst's directory, then
// renames it over dst. A concurrent reader of dst sees either the previous
// file or the complete copy. src may be replaced by rename while it is read;
// the copy is then the file that was open.
func Copy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary copy of %s: %w", dst, err)
	}
	tmpPath := tmp.Name()
	_, err = io.Copy(tmp, in)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpPath, dst)
	}
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}

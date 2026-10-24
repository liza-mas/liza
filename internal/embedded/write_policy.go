package embedded

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/termutil"
)

// ConfirmOptions controls yes/no approval prompts in embedded file writers.
type ConfirmOptions struct {
	AutoConfirm bool
}

// BackupFile copies src to src.bak using streaming I/O.
func BackupFile(src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(src + ".bak")
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func confirmMergeWithOptions(prompt string, reader *bufio.Reader, opts ConfirmOptions) (bool, error) {
	fmt.Print(prompt)
	if opts.AutoConfirm {
		fmt.Println("yes")
		return true, nil
	}
	response, err := termutil.ReadSingleKey(reader)
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	return response == "y", nil
}

func ensureCodexDir(codexDir string) error {
	return ensureProviderDir(codexDir)
}

func ensureProviderDir(dir string) error {
	info, err := os.Lstat(dir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			targetInfo, targetErr := os.Stat(dir)
			if targetErr == nil && targetInfo.IsDir() {
				return nil
			}
			if targetErr != nil {
				return fmt.Errorf("%s exists as symlink: %w", dir, targetErr)
			}
			return fmt.Errorf("%s exists as symlink and is not a directory", dir)
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode().IsRegular() && info.Size() == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
			return os.MkdirAll(dir, 0755)
		}
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

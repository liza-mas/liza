package updater

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

func Install(ctx context.Context, next candidate, target string, stdout, stderr io.Writer) error {
	return InstallWithBaseURL(ctx, next, target, stdout, stderr, "")
}

// InstallWithBaseURL is the actual implementation that accepts a releaseBaseURL parameter.
// This allows test injection while maintaining a safe default in production.
func InstallWithBaseURL(ctx context.Context, next candidate, target string, stdout, stderr io.Writer, releaseBaseURL string) error {
	switch next.Channel {
	case channelStable:
		return installReleaseBinary(ctx, next.Ref, target, stderr, releaseBaseURL)
	case channelMain:
		return installFromSource(ctx, next.Ref, target, stderr)
	default:
		return fmt.Errorf("invalid update channel %q", next.Channel)
	}
}

func installReleaseBinary(ctx context.Context, version, target string, stderr io.Writer, releaseBaseURL string) error {
	return installReleaseBinaryWithChecksumBase(ctx, version, target, stderr, releaseBaseURL, "")
}

func installReleaseBinaryWithChecksumBase(ctx context.Context, version, target string, stderr io.Writer, releaseBaseURL, checksumBaseURL string) error {
	url := releaseArchiveURL(version, runtime.GOOS, runtime.GOARCH, releaseBaseURL)
	fmt.Fprintf(stderr, "Downloading %s\n", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create release request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download release archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download release archive: HTTP %s", resp.Status)
	}

	// Read the entire archive into memory for checksum verification
	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read release archive: %w", err)
	}

	// Download and verify checksums
	checksumsURL := checksumURLWithBase(version, checksumBaseURL)
	checksums, err := downloadChecksums(ctx, checksumsURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}

	versionBare := strings.TrimPrefix(version, "v")
	archiveName := releaseArchiveName(versionBare, runtime.GOOS, runtime.GOARCH)
	expectedChecksum, ok := checksums[archiveName]
	if !ok {
		return fmt.Errorf("checksum not found for %s", archiveName)
	}

	if err := verifyChecksum(archiveData, expectedChecksum); err != nil {
		return fmt.Errorf("verify checksum: %w", err)
	}

	if runtime.GOOS == "windows" {
		return installBinaryFromZip(archiveData, target)
	}
	return installBinaryFromTarGz(bytes.NewReader(archiveData), target)
}

func releaseArchiveURL(version, goos, goarch string, releaseBaseURL string) string {
	versionBare := strings.TrimPrefix(version, "v")
	archive := releaseArchiveName(versionBare, goos, goarch)
	baseURL := strings.TrimRight(releaseBaseURL, "/")
	if baseURL == "" {
		baseURL = brand.ReleaseBaseURL
		if baseURL == "" {
			baseURL = fmt.Sprintf("https://github.com/%s/releases/download", releaseRepo())
		}
	}
	return fmt.Sprintf("%s/%s/%s", baseURL, version, archive)
}

func releaseArchiveName(versionBare, goos, goarch string) string {
	prefix := brand.ArchivePrefix
	if prefix == "" {
		prefix = brand.BinaryName
	}
	if prefix == "" {
		prefix = brand.RuntimeValues().ArchivePrefix
	}
	// Windows releases ship as zip, per the format override in .goreleaser.yaml.
	extension := "tar.gz"
	if goos == "windows" {
		extension = "zip"
	}
	return fmt.Sprintf("%s-%s-%s-%s.%s", prefix, versionBare, goos, goarch, extension)
}

func checksumURL(version string) string {
	return checksumURLWithBase(version, "")
}

func checksumURLWithBase(version, checksumBaseURL string) string {
	// Fetch checksums from the configured checksum base, independent from any
	// archive mirror override.
	baseURL := strings.TrimRight(checksumBaseURL, "/")
	if baseURL == "" {
		baseURL = brand.ChecksumBaseURL
		if baseURL == "" {
			baseURL = fmt.Sprintf("https://github.com/%s/releases/download", releaseRepo())
		}
	}
	return fmt.Sprintf("%s/%s/checksums.txt", baseURL, version)
}

func downloadChecksums(ctx context.Context, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create checksum request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download checksums: HTTP %s", resp.Status)
	}

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// GoReleaser format: "<sha256>  <filename>" or "<sha256> *<filename>"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		checksum := parts[0]
		filename := strings.TrimPrefix(parts[1], "*")
		checksums[filename] = checksum
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	return checksums, nil
}

func verifyChecksum(data []byte, expectedChecksum string) error {
	hash := sha256.Sum256(data)
	actual := hex.EncodeToString(hash[:])
	if actual != expectedChecksum {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expectedChecksum)
	}
	return nil
}

func installBinaryFromTarGz(r io.Reader, target string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read release gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("release archive does not contain %s binary", expectedArchiveBinaryName())
		}
		if err != nil {
			return fmt.Errorf("read release tar: %w", err)
		}

		// Only accept regular files named exactly like the branded binary.
		if filepath.Base(header.Name) != expectedArchiveBinaryName() {
			continue
		}

		// Reject dangerous entry types
		switch header.Typeflag {
		case tar.TypeReg:
			// Regular file is allowed
		case '\x00':
			// Legacy regular file marker is allowed
		default:
			// Reject symlinks, hardlinks, directories, devices, etc.
			return fmt.Errorf("reject dangerous tar entry type %d for %s", header.Typeflag, header.Name)
		}

		// Reject path traversal attempts
		if strings.Contains(header.Name, "..") {
			return fmt.Errorf("reject path traversal in tar entry: %s", header.Name)
		}

		// Strip dangerous permission bits (setuid, setgid, sticky)
		mode := header.FileInfo().Mode()
		mode &^= 0o7000 // Clear setuid (0o4000), setgid (0o2000), sticky (0o1000)

		return replaceBinary(target, tr, mode)
	}
}

// installBinaryFromZip extracts the branded binary from a Windows release zip.
//
// The whole archive is held in memory rather than streamed: the zip central
// directory lives at the end of the file, so archive/zip needs an io.ReaderAt,
// and the caller has already buffered the download to verify its checksum.
func installBinaryFromZip(data []byte, target string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("read release zip: %w", err)
	}

	for _, entry := range zr.File {
		if filepath.Base(entry.Name) != expectedArchiveBinaryName() {
			continue
		}
		if strings.Contains(entry.Name, "..") {
			return fmt.Errorf("reject path traversal in zip entry: %s", entry.Name)
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("reject non-regular zip entry %s", entry.Name)
		}

		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", entry.Name, err)
		}
		defer rc.Close()

		// Windows carries no permission bits worth preserving; replaceBinary
		// defaults the mode.
		return replaceBinary(target, rc, 0)
	}

	return fmt.Errorf("release archive does not contain %s binary", expectedArchiveBinaryName())
}

func expectedArchiveBinaryName() string {
	name := brand.RuntimeValues().BinaryName
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func replaceBinary(target string, r io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+binaryName()+"-update-*")
	if err != nil {
		return fmt.Errorf("create temporary binary: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary binary: %w", err)
	}
	if mode == 0 {
		mode = 0o755
	}
	if err := os.Chmod(tmpPath, mode|0o111); err != nil {
		return fmt.Errorf("chmod temporary binary: %w", err)
	}
	if err := renameOverBinary(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// renameOverBinary moves the staged binary onto target.
//
// On Windows an executable that is currently running cannot be overwritten or
// deleted, and `liza update` is itself running from target. It can, however, be
// renamed: the directory entry moves while the open image keeps pointing at the
// same file. So displace the running binary first, then move the new one into
// place, and try to sweep the leftover — which fails while the old process
// lives, hence the best-effort removal at the next update.
func renameOverBinary(stagedPath, target string) error {
	if err := os.Rename(stagedPath, target); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return fmt.Errorf("replace %s: %w", target, err)
	}

	displaced := target + ".old"
	_ = os.Remove(displaced)
	if err := os.Rename(target, displaced); err != nil {
		return fmt.Errorf("displace running binary %s: %w", target, err)
	}
	if err := os.Rename(stagedPath, target); err != nil {
		// Put the previous binary back rather than leaving nothing installed.
		if restoreErr := os.Rename(displaced, target); restoreErr != nil {
			return fmt.Errorf("replace %s: %w (and restoring %s failed: %v)", target, err, displaced, restoreErr)
		}
		return fmt.Errorf("replace %s: %w", target, err)
	}
	_ = os.Remove(displaced)
	return nil
}

type installRollback struct {
	backupPath string
}

func prepareInstallRollback(target string) (installRollback, error) {
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return installRollback{}, nil
		}
		return installRollback{}, fmt.Errorf("stat current binary: %w", err)
	}
	if info.IsDir() {
		return installRollback{}, fmt.Errorf("current binary target is a directory: %s", target)
	}

	src, err := os.Open(target)
	if err != nil {
		return installRollback{}, fmt.Errorf("open current binary: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+binaryName()+"-update-backup-*")
	if err != nil {
		return installRollback{}, fmt.Errorf("create backup binary: %w", err)
	}
	backupPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(backupPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return installRollback{}, fmt.Errorf("copy current binary to backup: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return installRollback{}, fmt.Errorf("close backup binary: %w", err)
	}
	if err := os.Chmod(backupPath, info.Mode().Perm()); err != nil {
		return installRollback{}, fmt.Errorf("chmod backup binary: %w", err)
	}

	cleanup = false
	return installRollback{backupPath: backupPath}, nil
}

func (r *installRollback) restore(target string) error {
	if r.backupPath == "" {
		return nil
	}
	if err := os.Rename(r.backupPath, target); err != nil {
		return fmt.Errorf("restore %s from backup: %w", target, err)
	}
	r.backupPath = ""
	return nil
}

func (r *installRollback) cleanup() {
	if r.backupPath == "" {
		return
	}
	_ = os.Remove(r.backupPath)
	r.backupPath = ""
}

// VerifyInstall runs a post-install verification by executing the installed binary
// with a version command to ensure it's functional.
func VerifyInstall(ctx context.Context, target string, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, target, "version")
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("version check failed: %w", err)
	}
	return nil
}

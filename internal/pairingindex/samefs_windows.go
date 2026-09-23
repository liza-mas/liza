//go:build windows

package pairingindex

// sameFilesystem is not checked on Windows, where the index script already
// runs through Git Bash and indexes are a best-effort aid.
func sameFilesystem(string, string) (bool, error) {
	return true, nil
}

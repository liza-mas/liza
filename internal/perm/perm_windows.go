package perm

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// fileDeleteChild is the right to delete an entry from a directory. x/sys does
// not name it; it is FILE_DELETE_CHILD from winnt.h.
const fileDeleteChild = 0x0040

// denyWrites prepends an explicit deny entry for the current user to the
// directory's DACL.
//
// A deny entry is required rather than the removal of an allow entry: the
// process usually owns the directory it created, and an owner keeps
// READ_CONTROL and WRITE_DAC, so merely dropping allow entries would leave it
// able to grant itself access back. Deny entries are also evaluated before
// allow entries, so this holds even when the user is an administrator whose
// group is granted full control — the common case for a directory under %TEMP%.
//
// The rights denied are exactly those chmod 0555 takes away from a directory:
// adding a file, adding a subdirectory, and deleting an entry. The entry is not
// inherited, so files already inside stay writable, as they do on POSIX. Denying
// GENERIC_WRITE with inheritance instead would be stricter than the POSIX
// behaviour it stands in for, and would make callers fail earlier and for a
// different reason than they do on Unix.
//
// Restoring reinstates the DACL read before the change, which the owner is
// still permitted to do.
func denyWrites(dir string) (func() error, error) {
	previous, err := windows.GetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return nil, fmt.Errorf("read security info for %s: %w", dir, err)
	}
	previousDACL, _, err := previous.DACL()
	if err != nil {
		return nil, fmt.Errorf("read DACL for %s: %w", dir, err)
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current user token: %w", err)
	}

	denied, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | fileDeleteChild,
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, previousDACL)
	if err != nil {
		return nil, fmt.Errorf("build deny DACL for %s: %w", dir, err)
	}

	if err := windows.SetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil, nil, denied, nil,
	); err != nil {
		return nil, fmt.Errorf("deny writes on %s: %w", dir, err)
	}

	return func() error {
		if err := windows.SetNamedSecurityInfo(
			dir,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			nil, nil, previousDACL, nil,
		); err != nil {
			return fmt.Errorf("restore writes on %s: %w", dir, err)
		}
		return nil
	}, nil
}

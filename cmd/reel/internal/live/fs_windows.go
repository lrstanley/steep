// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package live

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// errRefused is the error for dialing a socket nothing listens on. Windows
// also returns it for a socket that does not exist.
const errRefused = windows.WSAECONNREFUSED

// currentUser returns the SID of the user this process runs as.
func currentUser() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return tu.User.Sid, nil
}

// mkdir creates dir, if it does not exist, owned by the user, and with an
// ACL, not inherited from its parent, that lets only the user open it.
func mkdir(dir string) error {
	// The directory is the user's own choice, or their runtime directory.
	if _, err := os.Lstat(dir); err == nil { //nolint:gosec // G703: see above.
		return nil // checkDir judges it.
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil { //nolint:gosec // G703: see above.
		return err
	}
	user, err := currentUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.String() + "D:P(A;OICI;FA;;;" + user.String() + ")")
	if err != nil {
		return err
	}
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err = windows.CreateDirectory(path, sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	return nil
}

// checkDir reports an error unless dir is a directory, not a symlink, that
// the user owns and only the user (and the system, and administrators, who
// can take any file, and the creator of a file in it, the user) can open:
// as the sockets in it take no credentials, it is what keeps other users
// out of the sessions.
func checkDir(dir string) error {
	info, err := os.Lstat(dir) //nolint:gosec // G703: the directory is the user's own choice, or their runtime directory.
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("session directory %s is not a directory", dir)
	}
	user, err := currentUser()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("session directory %s: %w", dir, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("session directory %s: %w", dir, err)
	}
	if !owner.Equals(user) {
		return fmt.Errorf("session directory %s is not owned by you", dir)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("session directory %s: %w", dir, err)
	}
	if dacl == nil {
		return fmt.Errorf("session directory %s is open to everyone; it must be open only to you", dir)
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("session directory %s: %w", dir, err)
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default: // Such as a conditional allow, whose grant is not checked here.
			return fmt.Errorf("session directory %s has an access entry of a kind reel cannot check (type %d); it must be open only to you", dir, ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.Equals(user) || sid.IsWellKnown(windows.WinLocalSystemSid) ||
			sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) || sid.IsWellKnown(windows.WinCreatorOwnerSid) {
			continue
		}
		who := sid.String()
		if account, domain, _, lerr := sid.LookupAccount(""); lerr == nil {
			who = account
			if domain != "" {
				who = domain + `\` + account
			}
		}
		return fmt.Errorf("session directory %s is open to other users (%s); it must be open only to you", dir, who)
	}
	return nil
}

// errLocked is the error for a lock another process holds.
var errLocked = errors.New("locked")

// lock takes the lock file at path, which closing the returned file
// releases. Windows denies a second exclusive open of the same file.
func lock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if !errors.Is(err, os.ErrExist) {
		return f, err
	}
	// A lock file a running session holds open cannot be removed; one a
	// session that is gone left can.
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errLocked
	}
	f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, errLocked // Another session took it meanwhile.
	}
	return f, err
}

package config

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	getNamedSecurityInfo = syscall.NewLazyDLL("advapi32.dll").NewProc("GetNamedSecurityInfoW")
	localFree            = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

const (
	seFileObject             = 1
	ownerSecurityInformation = 1
)

var administrators = map[string]bool{"S-1-5-32-544": true, "S-1-5-18": true}

// A file under ProgramData proves nothing about who wrote it: BUILTIN\Users may
// create there and the creator keeps full control of what it made. The owner is
// the one thing a planter cannot choose, so a machine-wide answer is read only
// when Administrators or SYSTEM own the file and the directory it is in. This
// is OpenSSH's StrictModes.
func trusted(path string) error {
	for _, p := range []string{path, filepath.Dir(path)} {
		sid, who, err := owner(p)
		if err != nil {
			return fmt.Errorf("%s: cannot tell who owns it: %w", p, err)
		}
		if !administrators[sid] {
			return fmt.Errorf("%s is owned by %s, not by Administrators or SYSTEM, so no administrator wrote it", p, who)
		}
	}
	return nil
}

func owner(path string) (sid, who string, err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", "", err
	}
	var (
		s  *syscall.SID
		sd uintptr
	)
	r, _, _ := getNamedSecurityInfo.Call(uintptr(unsafe.Pointer(p)), seFileObject, ownerSecurityInformation,
		uintptr(unsafe.Pointer(&s)), 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if r != 0 {
		return "", "", syscall.Errno(r)
	}
	defer localFree.Call(sd)
	if sid, err = s.String(); err != nil {
		return "", "", err
	}
	who = sid
	if account, domain, _, err := s.LookupAccount(""); err == nil {
		who = domain + `\` + account + " (" + sid + ")"
	}
	return sid, who, nil
}

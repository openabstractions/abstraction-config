package config

import (
	"errors"
	"sync"
	"syscall"
)

const notifier = "ReadDirectoryChangesW"

var cancelIoEx = syscall.NewLazyDLL("kernel32.dll").NewProc("CancelIoEx")

func notifyDirs(dirs []string) (<-chan struct{}, func(), error) {
	if len(dirs) == 0 {
		return nil, nil, errors.New("no configuration directory exists yet")
	}
	events := make(chan struct{}, 1)
	var open []syscall.Handle
	for _, dir := range dirs {
		name, err := syscall.UTF16PtrFromString(dir)
		if err != nil {
			continue
		}
		h, err := syscall.CreateFile(name, syscall.FILE_LIST_DIRECTORY,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
			nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			continue
		}
		open = append(open, h)
		go report(h, events)
	}
	if len(open) == 0 {
		return nil, nil, errors.New("the configuration directory cannot be opened for notification")
	}
	var once sync.Once
	// Cancelling is the whole of stopping: the pending read returns aborted and
	// the goroutine that issued it closes its own handle, so no handle is closed
	// while a read is still using it.
	return events, func() {
		once.Do(func() {
			for _, h := range open {
				cancelIoEx.Call(uintptr(h), 0)
			}
		})
	}, nil
}

func report(h syscall.Handle, events chan<- struct{}) {
	defer syscall.CloseHandle(h)
	buf := make([]byte, 4096)
	for {
		var n uint32
		err := syscall.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), false,
			syscall.FILE_NOTIFY_CHANGE_FILE_NAME|syscall.FILE_NOTIFY_CHANGE_LAST_WRITE|syscall.FILE_NOTIFY_CHANGE_SIZE,
			&n, nil, 0)
		if err != nil {
			return
		}
		select {
		case events <- struct{}{}:
		default:
		}
	}
}

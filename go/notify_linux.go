package config

import (
	"errors"
	"os"
	"syscall"
)

const notifier = "inotify"

func notifyDirs(dirs []string) (<-chan struct{}, func(), error) {
	if len(dirs) == 0 {
		return nil, nil, errors.New("no configuration directory exists yet")
	}
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		return nil, nil, err
	}
	added := 0
	for _, dir := range dirs {
		mask := uint32(syscall.IN_CLOSE_WRITE | syscall.IN_MOVED_TO | syscall.IN_CREATE | syscall.IN_DELETE)
		if _, err := syscall.InotifyAddWatch(fd, dir, mask); err == nil {
			added++
		}
	}
	if added == 0 {
		syscall.Close(fd)
		return nil, nil, errors.New("the configuration directory cannot be watched")
	}
	// Non-blocking, so the runtime polls it and Close unblocks the read that is
	// waiting on it. A descriptor closed under a blocking read does not.
	f := os.NewFile(uintptr(fd), "inotify")
	events := make(chan struct{}, 1)
	go func() {
		defer f.Close()
		buf := make([]byte, 4096)
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}()
	return events, func() { f.Close() }, nil
}

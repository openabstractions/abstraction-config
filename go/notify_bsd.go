//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package config

import (
	"errors"
	"syscall"
)

const notifier = "kqueue"

func notifyDirs(dirs []string) (<-chan struct{}, func(), error) {
	if len(dirs) == 0 {
		return nil, nil, errors.New("no configuration directory exists yet")
	}
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, nil, err
	}
	var wake [2]int
	if err := syscall.Pipe(wake[:]); err != nil {
		syscall.Close(kq)
		return nil, nil, err
	}
	var open []int
	var changes []syscall.Kevent_t
	for _, dir := range dirs {
		fd, err := syscall.Open(dir, syscall.O_RDONLY, 0)
		if err != nil {
			continue
		}
		open = append(open, fd)
		var ev syscall.Kevent_t
		syscall.SetKevent(&ev, fd, syscall.EVFILT_VNODE, syscall.EV_ADD|syscall.EV_CLEAR)
		ev.Fflags = syscall.NOTE_WRITE | syscall.NOTE_EXTEND | syscall.NOTE_RENAME | syscall.NOTE_DELETE
		changes = append(changes, ev)
	}
	if len(open) == 0 {
		syscall.Close(kq)
		syscall.Close(wake[0])
		syscall.Close(wake[1])
		return nil, nil, errors.New("the configuration directory cannot be watched")
	}
	// The read end of a pipe, so that closing the write end ends the wait
	// without a timeout to check a flag against.
	var stopper syscall.Kevent_t
	syscall.SetKevent(&stopper, wake[0], syscall.EVFILT_READ, syscall.EV_ADD)
	changes = append(changes, stopper)
	if _, err := syscall.Kevent(kq, changes, nil, nil); err != nil {
		syscall.Close(kq)
		syscall.Close(wake[0])
		syscall.Close(wake[1])
		for _, fd := range open {
			syscall.Close(fd)
		}
		return nil, nil, err
	}

	events := make(chan struct{}, 1)
	go func() {
		defer func() {
			syscall.Close(kq)
			syscall.Close(wake[0])
			for _, fd := range open {
				syscall.Close(fd)
			}
		}()
		out := make([]syscall.Kevent_t, len(changes))
		for {
			n, err := syscall.Kevent(kq, nil, out, nil)
			if err == syscall.EINTR {
				continue
			}
			if err != nil {
				return
			}
			for i := range out[:n] {
				if int(out[i].Ident) == wake[0] {
					return
				}
			}
			if n > 0 {
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()
	return events, func() { syscall.Close(wake[1]) }, nil
}

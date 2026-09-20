package config

import (
	"errors"
	"sync"

	"golang.org/x/sys/windows"
)

const notifier = "ReadDirectoryChangesW"

// A directory read is issued on whichever OS thread runs the goroutine at that
// moment. Without a completion port Windows queues the request to that thread,
// and CancelIoEx does not return while that thread is blocked in unrelated
// synchronous I/O (a Go stdin reader, for example). A handle associated with a
// completion port makes the request independent of the issuing thread, so
// cancellation and completion never wait on it.
type directoryNotification struct {
	mu      sync.Mutex
	h       windows.Handle
	port    windows.Handle
	overlap windows.Overlapped
	buffer  [4096]byte
	closed  bool
	done    chan struct{}
}

func (d *directoryNotification) arm() error {
	var n uint32
	err := windows.ReadDirectoryChanges(d.h, &d.buffer[0], uint32(len(d.buffer)), false,
		windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_LAST_WRITE|windows.FILE_NOTIFY_CHANGE_SIZE,
		&n, &d.overlap, 0)
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		return nil
	}
	// A synchronous success still queues its completion packet to the port.
	return err
}

func notifyDirs(dirs []string) (<-chan struct{}, func(), error) {
	if len(dirs) == 0 {
		return nil, nil, errors.New("no configuration directory exists yet")
	}
	events := make(chan struct{}, 1)
	var open []*directoryNotification
	for _, dir := range dirs {
		name, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			continue
		}
		h, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
		if err != nil {
			continue
		}
		port, err := windows.CreateIoCompletionPort(h, 0, 0, 1)
		if err != nil {
			windows.CloseHandle(h)
			continue
		}
		d := &directoryNotification{h: h, port: port, done: make(chan struct{})}
		// Arm synchronously before the initial configuration snapshot can be read.
		// Merely opening a directory does not start change notifications.
		if err := d.arm(); err != nil {
			windows.CloseHandle(h)
			windows.CloseHandle(port)
			continue
		}
		open = append(open, d)
		go d.report(events)
	}
	if len(open) == 0 {
		return nil, nil, errors.New("the configuration directory cannot be opened for notification")
	}
	var once sync.Once
	return events, func() {
		once.Do(func() {
			for _, d := range open {
				d.mu.Lock()
				d.closed = true
				if d.h != 0 {
					// A pending read completes as aborted; a read already completed
					// leaves nothing to cancel, and report sees closed under the lock.
					windows.CancelIoEx(d.h, nil)
				}
				d.mu.Unlock()
			}
			// A handle and its OVERLAPPED storage remain owned until completion drains.
			for _, d := range open {
				<-d.done
			}
		})
	}, nil
}

func (d *directoryNotification) report(events chan<- struct{}) {
	defer close(d.done)
	defer func() {
		d.mu.Lock()
		windows.CloseHandle(d.h)
		d.h = 0
		windows.CloseHandle(d.port)
		d.mu.Unlock()
	}()
	for {
		var n uint32
		var key uintptr
		var overlap *windows.Overlapped
		err := windows.GetQueuedCompletionStatus(d.port, &n, &key, &overlap, windows.INFINITE)
		if overlap == nil {
			// The port itself failed; no completion can arrive through it.
			return
		}
		d.mu.Lock()
		if d.closed || err != nil {
			d.mu.Unlock()
			return
		}
		// Rearm while Close is excluded, then expose the notification. Changes
		// after this read are buffered by Windows for the next request.
		err = d.arm()
		d.mu.Unlock()
		select {
		case events <- struct{}{}:
		default:
		}
		if err != nil {
			return
		}
	}
}

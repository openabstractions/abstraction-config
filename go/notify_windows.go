package config

import (
	"errors"
	"sync"

	"golang.org/x/sys/windows"
)

const notifier = "ReadDirectoryChangesW"

type directoryNotification struct {
	mu      sync.Mutex
	h       windows.Handle
	event   windows.Handle
	overlap windows.Overlapped
	buffer  [4096]byte
	closed  bool
	done    chan struct{}
}

func (d *directoryNotification) arm() error {
	if err := windows.ResetEvent(d.event); err != nil {
		return err
	}
	var n uint32
	err := windows.ReadDirectoryChanges(d.h, &d.buffer[0], uint32(len(d.buffer)), false,
		windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_LAST_WRITE|windows.FILE_NOTIFY_CHANGE_SIZE,
		&n, &d.overlap, 0)
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		return nil
	}
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
		event, err := windows.CreateEvent(nil, 1, 0, nil)
		if err != nil {
			windows.CloseHandle(h)
			continue
		}
		d := &directoryNotification{h: h, event: event, done: make(chan struct{})}
		d.overlap.HEvent = event
		// Arm synchronously before the initial configuration snapshot can be read.
		// Merely opening a directory does not start change notifications.
		if err := d.arm(); err != nil {
			windows.CloseHandle(event)
			windows.CloseHandle(h)
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
		windows.CloseHandle(d.event)
		d.mu.Unlock()
	}()
	for {
		var n uint32
		err := windows.GetOverlappedResult(d.h, &d.overlap, &n, true)
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

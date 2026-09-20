package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	watch "github.com/openabstractions/abstraction-watch/go"
)

// A machine's answer changes while programs are running. Two mechanisms report
// it to the config service, and they answer different halves of the question.
//
// An edit made through Save or Edit is announced here, exactly and at once,
// because this layer knows it happened. It reaches only this process.
//
// A file changed by anything else — a text editor, an installer, another
// machine writing to a share — is noticed by the platform's own directory
// notification, which says that something moved and not what. The service
// rereads its bounded storage after either signal.

// settle is how long the directory must be still before a change is reported.
// A text editor writing a file produces several events and a subscriber wants
// one; below a tenth of a second nobody notices the delay, and above it a
// person who clicked a switch does.
const settle = 100 * time.Millisecond

var live struct {
	mu            sync.Mutex
	invalidations map[*InvalidationSubscription]bool
}

// announce is the service path: an edit this process made, told to this
// process's subscribers before the write has even reached the platform's
// notification. A subscriber that is not listening is not waited for, and with
// nobody subscribed nothing is read.
func announce() {
	live.mu.Lock()
	invalidations := make([]*InvalidationSubscription, 0, len(live.invalidations))
	for subscription := range live.invalidations {
		invalidations = append(invalidations, subscription)
	}
	live.mu.Unlock()
	for _, subscription := range invalidations {
		subscription.signal()
	}
}

// watchable is the directories a configuration file lives in, and only the ones
// that are there. A directory that does not exist yet cannot be watched, and a
// file appearing in one is therefore seen at the next Load rather than at once.
func watchable() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range searchPaths() {
		d := filepath.Dir(s.path)
		if seen[d] {
			continue
		}
		seen[d] = true
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// InvalidationSubscription reports possible changes without loading values.
// Services reread their selected bounded storage after a notification. Signals
// coalesce; they cover existing native watcher locations and in-process edits.
type InvalidationSubscription struct {
	changes chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc
	stop    func()
	once    sync.Once
	mu      sync.Mutex
	closed  bool
}

// WatchInvalidations reuses native directory notification and edit announcements.
// It refuses an unavailable native mechanism; it never installs a polling fallback.
func WatchInvalidations() (*InvalidationSubscription, error) {
	events, stop, e := notifyDirs(watchable())
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &InvalidationSubscription{changes: make(chan struct{}, 1), done: make(chan struct{}), cancel: cancel, stop: stop}
	live.mu.Lock()
	if live.invalidations == nil {
		live.invalidations = map[*InvalidationSubscription]bool{}
	}
	live.invalidations[s] = true
	live.mu.Unlock()
	go func() {
		defer close(s.done)
		defer func() { s.mu.Lock(); s.closed = true; close(s.changes); s.mu.Unlock() }()
		watch.Settle(ctx, events, settle, s.signal)
	}()
	return s, nil
}
func (s *InvalidationSubscription) Changes() <-chan struct{} { return s.changes }
func (s *InvalidationSubscription) signal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.changes <- struct{}{}:
	default:
	}
}
func (s *InvalidationSubscription) Close() error {
	s.once.Do(func() {
		live.mu.Lock()
		delete(live.invalidations, s)
		live.mu.Unlock()
		s.cancel()
		s.stop()
		<-s.done
	})
	return nil
}

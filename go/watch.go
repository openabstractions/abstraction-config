package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	watch "github.com/openabstractions/abstraction-watch/go"
)

// A machine's answer changes while programs are running, and until now the only
// way to find out was to ask again. Two mechanisms say so instead, and they
// answer different halves of the question.
//
// An edit made through Save or Edit is announced here, exactly and at once,
// because this layer knows it happened. That path is authoritative: it cannot
// miss and it does not wait. It reaches only this process, because a file is
// not a service and has nobody to tell.
//
// A file changed by anything else — a text editor, an installer, a supervisor
// on this machine, another machine writing to a share — is noticed by the
// platform's own directory notification, which says that something moved and
// not what. So the answer is read again and Stamp decides whether it differed.
// That path costs one settling period and covers every writer that has never
// heard of us.

// Notice is what this machine has been told, and whether it has stopped
// changing.
type Notice struct {
	Now     Config
	Quiet   bool
	Silence time.Duration
}

// settle is how long the directory must be still before the answer is read
// again. A text editor writing a file produces several events and a subscriber
// wants one; below a tenth of a second nobody notices the delay, and above it
// a person who clicked a switch does.
const settle = 100 * time.Millisecond

// askEvery is the fallback rate where a platform has no directory
// notification. Visible rather than hidden: How says so, and only the latency
// differs.
const askEvery = 2 * time.Second

// Subscription is a live view of what this machine has been told. Current is
// what was true at the last read; Next and Changes are two spellings of one
// stream, so drive a subscription with one of them.
type Subscription struct {
	sub    *watch.Subscription[Config]
	how    string
	stop   func()
	cancel context.CancelFunc
	ch     chan Config
	fan    sync.Once
	end    sync.Once
}

// Watch reports the machine's answer whenever it changes.
func Watch() *Subscription { return WatchQuiet(0) }

// WatchQuiet also reports quiet once nothing has changed for budget.
func WatchQuiet(budget time.Duration) *Subscription {
	s := &Subscription{ch: make(chan Config, 1)}
	watched := watchable()
	events, stop, err := notifyDirs(watched)
	if err != nil {
		every := askEvery
		if budget > 0 && budget < every {
			every = budget
		}
		s.sub = watch.Poll(look, every, budget)
		s.how = "asking every " + every.String() + " — " + err.Error()
		register(s)
		return s
	}
	c := Load()
	ctx, cancel := context.WithCancel(context.Background())
	s.sub, s.stop, s.cancel = watch.Push(c, c.Stamp(), budget), stop, cancel
	s.how = notifier + " on " + strings.Join(watched, ", ")
	go watch.Settle(ctx, events, settle, s.reread)
	register(s)
	return s
}

func look() (Config, string, error) {
	c := Load()
	return c, c.Stamp(), nil
}

func (s *Subscription) reread() {
	c := Load()
	s.sub.Post(c, c.Stamp())
}

// How names the mechanism this subscription is being told by, so that a
// platform which cannot notify is visible to a person rather than hidden in a
// latency.
func (s *Subscription) How() string { return s.how }

// Current is the answer as of the last read.
func (s *Subscription) Current() Config { return s.sub.Current() }

// Next blocks until the answer changed, or until quiet, or until closed.
func (s *Subscription) Next(ctx context.Context) (Notice, error) {
	n, err := s.sub.Next(ctx)
	return Notice{Now: n.Now, Quiet: n.Quiet, Silence: n.Silence}, err
}

// Changes carries the answer each time it differs. A subscriber that is not
// reading is skipped rather than waited for: the latest answer replaces the one
// nobody took.
func (s *Subscription) Changes() <-chan Config {
	s.fan.Do(func() { go s.forward() })
	return s.ch
}

func (s *Subscription) forward() {
	for {
		n, err := s.sub.Next(context.Background())
		if err != nil {
			return
		}
		if n.Quiet {
			continue
		}
		select {
		case <-s.ch:
		default:
		}
		s.ch <- n.Now
	}
}

func (s *Subscription) Close() error {
	s.end.Do(func() {
		forget(s)
		if s.cancel != nil {
			s.cancel()
		}
		if s.stop != nil {
			s.stop()
		}
		s.sub.Close()
	})
	return nil
}

var live struct {
	mu   sync.Mutex
	subs map[*Subscription]bool
}

func register(s *Subscription) {
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.subs == nil {
		live.subs = map[*Subscription]bool{}
	}
	live.subs[s] = true
}

func forget(s *Subscription) {
	live.mu.Lock()
	defer live.mu.Unlock()
	delete(live.subs, s)
}

// announce is the service path: an edit this process made, told to this
// process's subscribers before the write has even reached the platform's
// notification. A subscriber that is not listening is not waited for, and with
// nobody subscribed nothing is read.
func announce() {
	live.mu.Lock()
	subs := make([]*Subscription, 0, len(live.subs))
	for s := range live.subs {
		subs = append(subs, s)
	}
	live.mu.Unlock()
	if len(subs) == 0 {
		return
	}
	c := Load()
	stamp := c.Stamp()
	for _, s := range subs {
		s.sub.Post(c, stamp)
	}
}

// watchable is the directories a configuration file lives in, and only the ones
// that are there. A directory that does not exist yet cannot be watched, and a
// file appearing in one is therefore seen at the next Load rather than at once.
func watchable() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range searchPaths() {
		d := filepath.Dir(p)
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

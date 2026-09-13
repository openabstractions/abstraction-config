package config

import (
	"os"
	"testing"
	"time"
)

func TestInvalidationDoesNotLoadOrFilterConfig(t *testing.T) {
	path := own(t)
	t.Setenv("ABSTRACTION_STORE", "host-override")
	s, e := WatchInvalidations()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// A malformed file is an invalidation, never a synthesized snapshot. In-process
	// announcements remain independent of decoding or host environment filtering.
	if e = os.WriteFile(path, []byte("invalid configuration"), 0600); e != nil {
		t.Fatal(e)
	}
	announce()
	select {
	case <-s.Changes():
	case <-time.After(2 * time.Second):
		t.Fatal("invalid configuration notification was filtered")
	}
	s.Close()
	select {
	case _, ok := <-s.Changes():
		if ok {
			select {
			case _, ok = <-s.Changes():
				if ok {
					t.Fatal("unbounded queue")
				}
			case <-time.After(time.Second):
				t.Fatal("close not observed")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("close not observed")
	}
}

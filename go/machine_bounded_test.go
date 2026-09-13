package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	cas "github.com/openabstractions/abstraction-cas/go"
)

func TestServiceMachineSourceBoundsAndTrustOrdering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.json")
	oversized := bytes.Repeat([]byte("x"), MaxUserFileBytes+1)
	if e := os.WriteFile(path, oversized, 0600); e != nil {
		t.Fatal(e)
	}
	refused := errors.New("machine ownership refused")
	called := false
	_, e := readMachineSource(path, func(got string) error {
		called = true
		if got != path {
			t.Fatal(got)
		}
		return refused
	})
	if !called || !errors.Is(e, refused) {
		t.Fatal("trust did not precede data read", called, e)
	}
	_, e = readMachineSource(path, func(string) error { return nil })
	if !errors.Is(e, cas.ErrTooLarge) {
		t.Fatal("oversize not refused", e)
	}
	after, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(after, oversized) {
		t.Fatal("refusal changed source", e)
	}
}
func TestServiceMachineSourcePreservesDecodeAndMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.json")
	allow := func(string) error { return nil }
	if _, e := readMachineSource(path, allow); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("missing source changed", e)
	}
	for _, tc := range []struct {
		raw  string
		want string
		bad  bool
	}{{`{"store":"machine","future":true}`, "machine", false}, {`{"store":`, "", true}} {
		if e := os.WriteFile(path, []byte(tc.raw), 0600); e != nil {
			t.Fatal(e)
		}
		got, e := readMachineSource(path, allow)
		if (e != nil) != tc.bad || got.Store != tc.want {
			t.Fatal(tc, got, e)
		}
	}
}

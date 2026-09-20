package main

import (
	"fmt"
	"io"
	"os"
	"testing"

	config "github.com/openabstractions/abstraction-config/go"
)

// Provider stderr during a load counts as noise the corpus judges.
func TestProviderStderrIsNoise(t *testing.T) {
	d := newDriver(io.Discard)
	d.hearing(load)
	if got := d.said(); got != "ok silent" {
		t.Fatalf("a silent load was reported as noise: %s", got)
	}
	d.hearing(func() config.Config {
		fmt.Fprintln(os.Stderr, "abstraction: ignoring a file")
		return load()
	})
	if got := d.said(); got != "ok said" {
		t.Fatalf("provider stderr no longer counts as noise: %s", got)
	}
}

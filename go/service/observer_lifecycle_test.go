package service

import (
	"context"
	"github.com/openabstractions/abstraction-identity/listen"
	"net"
	"testing"
	"time"
)

type closeSignalListener struct{ closed chan struct{} }

func (l *closeSignalListener) Accept() (listen.Conn, error) { return nil, net.ErrClosed }
func (l *closeSignalListener) Close() error                 { close(l.closed); return nil }
func TestConfigObservationCannotStartAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	h := &Host{ctx: ctx}
	h.observation.source = func(context.Context) (<-chan struct{}, func(), error) {
		called = true
		return make(chan struct{}), func() {}, nil
	}
	if h.startObservation() {
		h.stopObservation()
		t.Error("closed host admitted observation")
	}
	if called {
		t.Fatal("closed host invoked source")
	}
}
func TestConfigObservationCloseJoinsStartingSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener := &closeSignalListener{closed: make(chan struct{})}
	h := &Host{ctx: ctx, cancel: cancel, listener: listener}
	entered := make(chan struct{})
	release := make(chan struct{})
	stopped := make(chan struct{})
	h.observation.source = func(context.Context) (<-chan struct{}, func(), error) {
		close(entered)
		<-release
		return make(chan struct{}), func() { close(stopped) }, nil
	}
	initialized := make(chan bool, 1)
	go func() { initialized <- h.startObservation() }()
	<-entered
	done := make(chan error, 1)
	go func() { done <- h.Close() }()
	<-listener.closed
	select {
	case <-done:
		t.Fatal("Close skipped in-flight source")
	default:
	}
	close(release)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join initialized source")
	}
	<-initialized
	select {
	case <-stopped:
	default:
		t.Fatal("source stop omitted")
	}
}
func TestConfigObservationFailureAvailability(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &Host{ctx: ctx}
	h.observation.source = func(context.Context) (<-chan struct{}, func(), error) { return nil, nil, net.ErrClosed }
	if !h.ObservationAvailable() {
		t.Fatal("configured support absent")
	}
	if h.startObservation() {
		t.Fatal("failed source accepted")
	}
	if h.ObservationAvailable() {
		t.Fatal("known failed source advertised")
	}
}

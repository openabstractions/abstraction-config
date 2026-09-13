package service

import (
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

func userWire(s config.UserFileSnapshot) wire.UserSnapshot {
	off := s.Values.Off
	if off == nil {
		off = map[string]string{}
	}
	return wire.UserSnapshot{Revision: s.Revision, Values: wire.UserSettings{NasStore: s.Values.NASStore, Store: s.Values.Store, LogSink: s.Values.LogSink, LogService: s.Values.LogService, Off: off}}
}
func (r *receiver) ReadUser() (wire.UserSnapshot, error) {
	if err := r.authorize(); err != nil {
		return wire.UserSnapshot{}, err
	}
	s, err := config.ReadUserStore(r.host.userStore, r.host.userPath)
	if err != nil {
		return wire.UserSnapshot{}, &wire.ServiceError{Code: "storage_unavailable", Message: "user configuration could not be read"}
	}
	return userWire(s), nil
}
func (r *receiver) ReplaceUser(expected string, values wire.UserSettings) (wire.UserReplaceResult, error) {
	if err := r.authorize(); err != nil {
		return wire.UserReplaceResult{}, err
	}
	if expected == "" {
		return wire.UserReplaceResult{}, &wire.ServiceError{Code: "invalid_revision", Message: "expected revision required"}
	}
	s, applied, err := config.ReplaceUserStore(r.host.userStore, r.host.userPath, expected, config.Config{NASStore: values.NasStore, Store: values.Store, LogSink: values.LogSink, LogService: values.LogService, Off: values.Off})
	if err != nil {
		return wire.UserReplaceResult{}, &wire.ServiceError{Code: "storage_unavailable", Message: "user configuration could not be replaced"}
	}
	outcome := wire.UserReplaceOutcomeConflict
	if applied {
		outcome = wire.UserReplaceOutcomeApplied
	}
	return wire.UserReplaceResult{Outcome: outcome, Snapshot: userWire(s)}, nil
}

package service

import (
	"context"
	"errors"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
)

// EditPolicy authorizes a user-rung replacement for the rechecked receiving
// peer. It runs after same-account Program proof and before storage access, on
// every ReplaceUser call. It must honor ctx and be safe for concurrent calls.
// Wrap ErrEditPolicyUnavailable when the decision cannot be obtained; every
// other error is an evaluated refusal.
type EditPolicy func(context.Context, *identity.Peer) error

// ErrEditPolicyUnavailable distinguishes a failed decision lookup from refusal.
var ErrEditPolicyUnavailable = errors.New("config: edit policy unavailable")

// EnableEditPolicy narrows ReplaceUser to callers the policy authorizes.
// Configure it before Serve. Without it same-account Program proof suffices.
func (h *Host) EnableEditPolicy(policy EditPolicy) error {
	h.observation.mu.Lock()
	defer h.observation.mu.Unlock()
	if h.observation.serving || h.ctx.Err() != nil {
		return errors.New("config: configure edit policy before Serve")
	}
	if policy == nil {
		return errors.New("config: explicit edit policy required")
	}
	h.editPolicy = policy
	return nil
}

// OmitMachineRung makes reads answer from the user rung and run overrides
// alone, reading neither the administrator's machine file nor its directory. A
// runtime isolated from the installation configures it with its own user store.
// Configure it before Serve.
func (h *Host) OmitMachineRung() error {
	h.observation.mu.Lock()
	defer h.observation.mu.Unlock()
	if h.observation.serving || h.ctx.Err() != nil {
		return errors.New("config: omit the machine rung before Serve")
	}
	h.withoutMachine = true
	return nil
}

func refusedReplace(outcome wire.UserReplaceOutcome) wire.UserReplaceResult {
	return wire.UserReplaceResult{Outcome: outcome, Snapshot: wire.UserSnapshot{Values: wire.UserSettings{Off: map[string]string{}}}}
}

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
	if policy := r.host.editPolicy; policy != nil {
		ctx := r.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		peer, err := r.call.Peer()
		if err != nil {
			return wire.UserReplaceResult{}, &wire.ServiceError{Code: "caller_unavailable", Message: "caller identity could not be rechecked"}
		}
		if err := policy(ctx, peer); err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrEditPolicyUnavailable) {
				return refusedReplace(wire.UserReplaceOutcomeUnavailable), nil
			}
			return refusedReplace(wire.UserReplaceOutcomeForbidden), nil
		}
		if ctx.Err() != nil {
			return refusedReplace(wire.UserReplaceOutcomeUnavailable), nil
		}
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

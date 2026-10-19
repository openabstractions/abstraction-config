package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
	rights "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// KeyReadPolicy checks the actual receiving peer against runtime registration
// evidence and its exact key grant. A name or self-asserted provider role is not
// evidence. Nil denies every key; it never falls back to same-account identity.
type KeyReadPolicy func(context.Context, *identity.Peer, string) error

// KeyReadSetPolicy is KeyReadPolicy for several keys of one peer at one
// moment, so one registration and image check answers every key. The answer
// holds an entry for each key asked; a missing entry reads as unavailable.
type KeyReadSetPolicy func(context.Context, *identity.Peer, []string) map[string]error

var ErrKeyReadPolicyUnavailable = errors.New("config: key read policy unavailable")

// ErrKeyNotDesignated is a KeyReadPolicy's answer when no registration
// designates the reading peer for the key (CFG-X1): the provider's
// registration names no such key, or its kept image does not match the peer.
// The per-key not_granted then carries identity detail, distinct from a
// designated reader the Rights rule refuses (CFG-E4).
var ErrKeyNotDesignated = errors.New("config: no registration designates this reader for the key")

func boundaryRefusal() *rights.RefusalDetail {
	return &rights.RefusalDetail{Source: rights.RefusalSourceIdentity, Decision: rights.RefusalDecisionDenied, Ask: rights.RefusalAskNone}
}

func policyRefusal(err error, action string) *rights.RefusalDetail {
	decision := rights.RefusalDecisionNotGranted
	if errors.Is(err, ErrKeyNotDesignated) {
		return &rights.RefusalDetail{Source: rights.RefusalSourceIdentity, Decision: decision, Ask: rights.RefusalAskNone, Action: action}
	}
	var evaluated interface{ DecisionOutcome() rights.DecisionOutcome }
	if errors.As(err, &evaluated) && evaluated.DecisionOutcome() == rights.DecisionOutcomeDenied {
		decision = rights.RefusalDecisionDenied
	}
	return &rights.RefusalDetail{Source: rights.RefusalSourceRights, Decision: decision, Ask: rights.RefusalAskNone, Action: action}
}

type TypedOptions struct {
	ReadPolicy KeyReadPolicy
	// ReadPolicyKeys, when set, decides in place of ReadPolicy.
	ReadPolicyKeys KeyReadSetPolicy
	DynamicDefault func(string) (*wire.Value, error)
	RestartValues  map[string]*wire.Value
	// RestartState receives values only after the native consumer admits them.
	RestartState *RestartState
}

func (h *Host) EnableTypedConfig(options TypedOptions) error {
	h.observation.mu.Lock()
	defer h.observation.mu.Unlock()
	if h.observation.serving || h.ctx.Err() != nil {
		return errors.New("config: configure typed sources before Serve")
	}
	h.typedOptions = options
	return nil
}

func (h *Host) typedSources() config.TypedSources {
	machine := config.MachinePath()
	if h.withoutMachine {
		machine = ""
	}
	values := h.typedOptions.RestartValues
	if h.typedOptions.RestartState != nil {
		values, _ = h.typedOptions.RestartState.snapshot()
	}
	return config.TypedSources{UserStore: h.userStore, UserPath: h.userPath, MachinePath: machine, DynamicDefault: h.typedOptions.DynamicDefault, RestartValues: values}
}

type typedReceiver struct{ *receiver }

func typedProblem(err error) *wire.KeyProblem {
	var key *config.KeyError
	if errors.As(err, &key) {
		return &wire.KeyProblem{Key: key.Key, Reason: key.Reason}
	}
	return nil
}
func typedCorrupt(err error) bool {
	var record *config.RecordError
	return errors.As(err, &record)
}
func typedCorruptOffset(err error) int64 {
	var record *config.RecordError
	if errors.As(err, &record) {
		return record.Offset
	}
	return 0
}

func typedCorruptSource(err error) *wire.ConfigSource {
	var record *config.RecordError
	if errors.As(err, &record) {
		return record.Source
	}
	return nil
}

// errNoKeyReadPolicy refuses every key when the host composed no read policy.
var errNoKeyReadPolicy = errors.New("config: no key read policy")

// keyDecisions is one pass of key-read decisions for the calling peer, all
// made at one point of a request. A nil entry admits its key.
type keyDecisions map[string]error

func (d keyDecisions) admitted(key string) bool {
	err, ok := d[key]
	return ok && err == nil
}

func catalogueKeys() []string {
	var keys []string
	for _, d := range wire.ConfigKeyDescriptors() {
		keys = append(keys, d.Key)
	}
	return keys
}

// decideKeys decides keys for the calling peer in one pass. Nothing is kept
// between passes: every check point of a request decides afresh, and every
// check made at the same point shares its pass.
func (r *typedReceiver) decideKeys(keys []string) (keyDecisions, error) {
	out := keyDecisions{}
	if len(keys) == 0 {
		return out, nil
	}
	options := r.host.typedOptions
	if options.ReadPolicyKeys == nil && options.ReadPolicy == nil {
		for _, key := range keys {
			out[key] = errNoKeyReadPolicy
		}
		return out, nil
	}
	peer, err := r.call.Peer()
	if err != nil {
		return nil, ErrKeyReadPolicyUnavailable
	}
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	answers := map[string]error{}
	if options.ReadPolicyKeys != nil {
		answers = options.ReadPolicyKeys(ctx, peer, slices.Clone(keys))
	} else {
		for _, key := range keys {
			if _, done := answers[key]; done {
				continue
			}
			answers[key] = options.ReadPolicy(ctx, peer, key)
			if errors.Is(answers[key], ErrKeyReadPolicyUnavailable) || ctx.Err() != nil {
				return nil, ErrKeyReadPolicyUnavailable
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ErrKeyReadPolicyUnavailable
	}
	for _, key := range keys {
		err, ok := answers[key]
		if !ok || errors.Is(err, ErrKeyReadPolicyUnavailable) {
			return nil, ErrKeyReadPolicyUnavailable
		}
		out[key] = err
	}
	return out, nil
}

func (r *typedReceiver) Read(names []string) (wire.ReadResult, error) {
	if err := r.authorize(); err != nil {
		return wire.ReadResult{Outcome: wire.ReadOutcomeForbidden, Entries: []wire.Entry{}, Refusal: boundaryRefusal()}, nil
	}
	if len(names) == 0 {
		names = catalogueKeys()
	}
	var asked []string
	for _, key := range names {
		if slices.Contains(catalogueKeys(), key) {
			asked = append(asked, key)
		}
	}
	pre, err := r.decideKeys(asked)
	if err != nil {
		return wire.ReadResult{Outcome: wire.ReadOutcomeUnavailable, Entries: []wire.Entry{}}, nil
	}
	result, _ := r.readDecided(names, pre, false)
	return result, nil
}

// readDecided reads names admitted by pre, then decides once more after the
// storage read: a key refused by then is withheld. The second pass covers the
// whole catalogue when all is set or the snapshot carries diagnostics, and is
// returned for the caller's own checks; it is pre when nothing was read.
func (r *typedReceiver) readDecided(names []string, pre keyDecisions, all bool) (wire.ReadResult, keyDecisions) {
	result := wire.ReadResult{Outcome: wire.ReadOutcomeRead, Entries: []wire.Entry{}}
	known := map[string]bool{}
	for _, key := range catalogueKeys() {
		known[key] = true
	}
	allowed := map[string]bool{}
	for _, key := range names {
		if !known[key] {
			result.Refused = append(result.Refused, wire.KeyRefusal{Key: key, Reason: wire.KeyRefusalReasonUnknownKey})
			continue
		}
		if !pre.admitted(key) {
			result.Refused = append(result.Refused, wire.KeyRefusal{Key: key, Reason: wire.KeyRefusalReasonNotGranted, Refusal: policyRefusal(pre[key], "abstraction.config/key.read")})
			continue
		}
		allowed[key] = true
	}
	post := pre
	// Do not touch settings or dynamic providers for a caller with no key grant.
	if len(allowed) != 0 {
		snapshot, err := r.host.typedSources().ReadSnapshot(allowed)
		if err != nil {
			result.Outcome = wire.ReadOutcomeUnavailable
			result.Problem = typedProblem(err)
			if typedCorrupt(err) {
				result.Outcome = wire.ReadOutcomeCorrupt
				result.CorruptOffset = typedCorruptOffset(err)
				result.CorruptSource = typedCorruptSource(err)
			}
			return result, nil
		}
		diagnostics := len(snapshot.IgnoredFields) != 0 || snapshot.IgnoredOmitted != 0
		var again []string
		if all || diagnostics {
			again = catalogueKeys()
		} else {
			for _, entry := range snapshot.Entries {
				if allowed[entry.Key] {
					again = append(again, entry.Key)
				}
			}
		}
		post, err = r.decideKeys(again)
		if err != nil {
			result.Outcome = wire.ReadOutcomeUnavailable
			result.Entries = []wire.Entry{}
			return result, nil
		}
		for _, entry := range snapshot.Entries {
			if allowed[entry.Key] {
				if !post.admitted(entry.Key) {
					result.Refused = append(result.Refused, wire.KeyRefusal{Key: entry.Key, Reason: wire.KeyRefusalReasonNotGranted, Refusal: policyRefusal(post[entry.Key], "abstraction.config/key.read")})
					continue
				}
				result.Entries = append(result.Entries, entry)
			}
		}
		// Diagnostics describe the whole document. Only a reader admitted for
		// every catalogue key may see unrelated member names or their sources.
		if diagnostics && len(result.Entries) != 0 && !slices.ContainsFunc(catalogueKeys(), func(key string) bool { return !post.admitted(key) }) {
			result.IgnoredFields, result.IgnoredOmitted = snapshot.IgnoredFields, snapshot.IgnoredOmitted
		}
	}
	// Revision includes visible origins, locks and restart-pending state.
	data, err := wire.ToJSON(result)
	if err != nil {
		return wire.ReadResult{Outcome: wire.ReadOutcomeUnavailable, Entries: []wire.Entry{}}, nil
	}
	hash := sha256.Sum256(data)
	result.Revision = hex.EncodeToString(hash[:])
	return result, post
}

func (r *typedReceiver) Describe() (wire.DescribeResult, error) {
	if err := r.authorize(); err != nil {
		return wire.DescribeResult{Outcome: wire.ReadOutcomeForbidden, Descriptors: []wire.KeyDescriptor{}, Refusal: boundaryRefusal()}, nil
	}
	return wire.DescribeResult{Outcome: wire.ReadOutcomeRead, Descriptors: wire.ConfigKeyDescriptors()}, nil
}

func (r *typedReceiver) ReadUser() (wire.UserReadResultV2, error) {
	if err := r.authorize(); err != nil {
		return wire.UserReadResultV2{Outcome: wire.ReadOutcomeForbidden, Refusal: boundaryRefusal()}, nil
	}
	snapshot, err := config.ReadTypedUserStore(r.host.userStore, r.host.userPath)
	if err != nil {
		outcome := wire.ReadOutcomeUnavailable
		problem := typedProblem(err)
		if typedCorrupt(err) {
			outcome = wire.ReadOutcomeCorrupt
		}
		return wire.UserReadResultV2{Outcome: outcome, Problem: problem, CorruptOffset: typedCorruptOffset(err)}, nil
	}
	return wire.UserReadResultV2{Outcome: wire.ReadOutcomeRead, Snapshot: &snapshot}, nil
}

func (r *typedReceiver) ReplaceUser(expected string, changes []wire.Change) (wire.UserReplaceResultV2, error) {
	if err := r.authorize(); err != nil {
		return wire.UserReplaceResultV2{Outcome: wire.ReplaceOutcomeForbidden, Refusal: boundaryRefusal()}, nil
	}
	result := wire.UserReplaceResultV2{Outcome: wire.ReplaceOutcomeInvalid}
	if expected == "" {
		return result, nil
	}
	if policy := r.host.editPolicy; policy != nil {
		peer, err := r.call.Peer()
		if err != nil {
			return result, err
		}
		ctx := r.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if err := policy(ctx, peer); err != nil {
			result.Outcome = wire.ReplaceOutcomeNotPermitted
			if errors.Is(err, ErrEditPolicyUnavailable) || ctx.Err() != nil {
				result.Outcome = wire.ReplaceOutcomeUnavailable
			} else {
				result.Refusal = policyRefusal(err, "abstraction.config/user.replace")
			}
			return result, nil
		}
	}
	if err := r.host.typedSources().CheckChanges(changes); err != nil {
		problem := typedProblem(err)
		if problem != nil {
			result.Refused = []wire.KeyProblem{*problem}
			if problem.Reason == "locked:machine" {
				result.Outcome = wire.ReplaceOutcomeNotPermitted
			}
		} else {
			result.Outcome = wire.ReplaceOutcomeUnavailable
		}
		var record *config.RecordError
		if errors.As(err, &record) {
			result.Outcome = wire.ReplaceOutcomeCorrupt
			result.CorruptOffset = record.Offset
		}
		return result, nil
	}
	snapshot, applied, err := config.ReplaceTypedUserStore(r.host.userStore, r.host.userPath, expected, changes)
	if err != nil {
		result.Outcome = wire.ReplaceOutcomeUnavailable
		if problem := typedProblem(err); problem != nil {
			result.Outcome = wire.ReplaceOutcomeInvalid
			result.Refused = []wire.KeyProblem{*problem}
		}
		var record *config.RecordError
		if errors.As(err, &record) {
			result.Outcome = wire.ReplaceOutcomeCorrupt
			result.CorruptOffset = record.Offset
		}
		return result, nil
	}
	result.Snapshot = &snapshot
	result.Outcome = wire.ReplaceOutcomeConflict
	if applied {
		result.Outcome = wire.ReplaceOutcomeApplied
	}
	return result, nil
}

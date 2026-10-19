package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
	"unicode/utf8"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	watch "github.com/openabstractions/abstraction-watch/go"
	watchcursor "github.com/openabstractions/abstraction-watch/go/cursor"
)

type typedGoodSnapshot struct {
	snapshot wire.ReadResult
	at       time.Time
}

// Retain one latest good answer for each current visibility set, with bounded
// memory. Each reuse is authorized afresh. This is not revision history.
func (r *typedReceiver) typedVisibility() (string, error) {
	decisions, err := r.decideKeys(catalogueKeys())
	if err != nil {
		return "", err
	}
	return typedVisibilityOf(decisions)
}

// typedVisibilityOf is the visibility one pass over the catalogue decides.
func typedVisibilityOf(decisions keyDecisions) (string, error) {
	var granted []string
	var refused []wire.KeyRefusal
	for _, key := range catalogueKeys() {
		if decisions.admitted(key) {
			granted = append(granted, key)
		} else {
			refused = append(refused, wire.KeyRefusal{Key: key, Reason: wire.KeyRefusalReasonNotGranted, Refusal: policyRefusal(decisions[key], "abstraction.config/key.read")})
		}
	}
	return typedVisibilityFingerprint(granted, refused)
}

func typedVisibilityFingerprint(granted []string, refused []wire.KeyRefusal) (string, error) {
	sort.Strings(granted)
	sort.Slice(refused, func(i, j int) bool { return refused[i].Key < refused[j].Key })
	data, err := json.Marshal(struct {
		Granted []string
		Refused []wire.KeyRefusal
	}{granted, refused})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func typedSnapshotVisibility(snapshot wire.ReadResult) (string, error) {
	var granted []string
	for _, entry := range snapshot.Entries {
		granted = append(granted, entry.Key)
	}
	refused := append([]wire.KeyRefusal(nil), snapshot.Refused...)
	return typedVisibilityFingerprint(granted, refused)
}

// Cached records carry the authority of their actual contents. A snapshot
// fits when its contents, and the complete-catalogue authority needed by
// diagnostics, match visibility under decisions made after it was read.
func typedSnapshotFits(snapshot wire.ReadResult, visibility string, decisions keyDecisions) bool {
	if decisions == nil {
		return false
	}
	actual, err := typedSnapshotVisibility(snapshot)
	if err != nil || actual != visibility {
		return false
	}
	for _, entry := range snapshot.Entries {
		if !decisions.admitted(entry.Key) {
			return false
		}
	}
	if len(snapshot.IgnoredFields) != 0 || snapshot.IgnoredOmitted != 0 {
		if len(snapshot.Entries) != len(wire.ConfigKeyDescriptors()) || len(snapshot.Refused) != 0 {
			return false
		}
	}
	current, err := typedVisibilityOf(decisions)
	return err == nil && current == visibility
}

// Observe uses the existing native invalidation source and wait gate. The
// effective digest includes lock and pending metadata, without process values.
// Each probe decides the caller's keys twice, before and after its storage
// read, and the answer is checked once more before it leaves.
func (r *typedReceiver) Observe(cursor string, waitMS int64) (wire.ConfigObservationV2, error) {
	refuse := func(outcome string) (wire.ConfigObservationV2, error) {
		word, ok := wire.ParseObservationOutcomeV2(outcome)
		if !ok {
			panic("config: invalid observation outcome")
		}
		return wire.ConfigObservationV2{Outcome: word, Cursor: cursor}, nil
	}
	if err := r.authorize(); err != nil {
		return wire.ConfigObservationV2{Outcome: wire.ObservationOutcomeV2Forbidden, Cursor: cursor, Refusal: boundaryRefusal()}, nil
	}
	if waitMS < 0 || waitMS > 30000 || len(cursor) > 512 || !utf8.ValidString(cursor) {
		return refuse("invalid")
	}
	o := &r.host.observation
	o.mu.Lock()
	configured := o.source != nil
	o.mu.Unlock()
	if !configured {
		return refuse("unsupported")
	}
	if !r.host.startObservation() {
		return wire.ConfigObservationV2{Outcome: wire.ObservationOutcomeV2Failed, Cursor: cursor, Cause: "notification_source_failed"}, nil
	}
	visibility, err := r.typedVisibility()
	if err != nil {
		return refuse("unavailable")
	}
	scope := sha256.Sum256([]byte("abstraction.config/observer@2:" + visibility))
	codec := watchcursor.Codec{Separator: ":", Scoped: true}
	binding := watchcursor.Envelope{Epoch: o.epoch, Scope: hex.EncodeToString(scope[:])}
	if cursor != "" {
		envelope, ok := codec.Parse(cursor)
		if !ok || !envelope.Hex(16, 32, 32) {
			return refuse("invalid")
		}
		if !envelope.Bound(binding.Epoch, binding.Scope) {
			return refuse("gap")
		}
	}
	var answer wire.ConfigObservationV2
	outcome, err := o.gate.Wait(r.call.WaitContext(), time.Duration(waitMS)*time.Millisecond, watch.Probe{
		Read: func() (bool, error) {
			pre, policyErr := r.decideKeys(catalogueKeys())
			if policyErr != nil {
				answer, _ = refuse("unavailable")
				return true, nil
			}
			if current, err := typedVisibilityOf(pre); err != nil || current != visibility {
				answer, _ = refuse("gap")
				return true, nil
			}
			snapshot, post := r.readDecided(catalogueKeys(), pre, true)
			data, err := wire.ToJSON(snapshot)
			if err != nil {
				answer, _ = refuse("unavailable")
				return true, nil
			}
			hash := sha256.Sum256(data)
			nextEnvelope := binding
			nextEnvelope.Payload = hex.EncodeToString(hash[:])
			next := codec.Encode(nextEnvelope)
			if snapshot.Outcome != wire.ReadOutcomeRead {
				answer = typedObservationRefusal(snapshot, next)
				if snapshot.Outcome == wire.ReadOutcomeUnavailable {
					o.mu.Lock()
					good, found := o.typedGood[visibility]
					o.mu.Unlock()
					if found {
						if now, err := r.decideKeys(catalogueKeys()); err == nil && typedSnapshotFits(good.snapshot, visibility, now) {
							age := time.Since(good.at).Milliseconds()
							answer.Snapshot, answer.AgeMs = &good.snapshot, &age
						}
					}
				}
				return next != cursor, nil
			}
			if !typedSnapshotFits(snapshot, visibility, post) {
				answer, _ = refuse("gap")
				return true, nil
			}
			o.mu.Lock()
			if o.typedGood == nil {
				o.typedGood = map[string]typedGoodSnapshot{}
			}
			if _, exists := o.typedGood[visibility]; !exists && len(o.typedGood) >= 64 {
				for key := range o.typedGood {
					delete(o.typedGood, key)
					break
				}
			}
			o.typedGood[visibility] = typedGoodSnapshot{snapshot, time.Now()}
			o.mu.Unlock()
			answer = wire.ConfigObservationV2{Outcome: wire.ObservationOutcomeV2Changed, Cursor: next, Snapshot: &snapshot}
			return next != cursor, nil
		}, Recheck: r.authorize,
	})
	if err != nil {
		if r.authorize() != nil {
			return wire.ConfigObservationV2{Outcome: wire.ObservationOutcomeV2Forbidden, Cursor: cursor, Refusal: boundaryRefusal()}, nil
		}
		return wire.ConfigObservationV2{}, err
	}
	if err := r.authorize(); err != nil {
		return wire.ConfigObservationV2{Outcome: wire.ObservationOutcomeV2Forbidden, Cursor: cursor, Refusal: boundaryRefusal()}, nil
	}
	final, policyErr := r.decideKeys(catalogueKeys())
	if policyErr != nil {
		return refuse("unavailable")
	}
	if current, err := typedVisibilityOf(final); err != nil || current != visibility {
		return refuse("gap")
	}
	if answer.Snapshot != nil && !typedSnapshotFits(*answer.Snapshot, visibility, final) {
		return refuse("gap")
	}
	switch outcome {
	case watch.Answered:
		return answer, nil
	case watch.Unchanged:
		if answer.Outcome == wire.ObservationOutcomeV2Unavailable {
			return answer, nil
		}
		return refuse("unchanged")
	case watch.Failed:
		return wire.ConfigObservationV2{Outcome: wire.ObservationOutcomeV2Failed, Cursor: cursor, Cause: "notification_source_failed"}, nil
	default:
		return refuse("unavailable")
	}
}

func typedObservationRefusal(snapshot wire.ReadResult, cursor string) wire.ConfigObservationV2 {
	word, ok := wire.ParseObservationOutcomeV2(snapshot.Outcome.String())
	if !ok {
		word = wire.ObservationOutcomeV2Failed
	}
	answer := wire.ConfigObservationV2{Outcome: word, Cursor: cursor, Problem: snapshot.Problem, Refusal: snapshot.Refusal}
	if snapshot.Outcome == wire.ReadOutcomeCorrupt {
		answer.CorruptOffset, answer.CorruptSource = snapshot.CorruptOffset, snapshot.CorruptSource
	}
	return answer
}

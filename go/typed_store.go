package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	cas "github.com/openabstractions/abstraction-cas/go"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

// KeyError identifies the setting that made a stored record or edit invalid.
type KeyError struct{ Key, Reason string }

func (e *KeyError) Error() string { return "config: " + e.Key + ": " + e.Reason }

type RecordError struct {
	Err    error
	Offset int64
	Source *wire.ConfigSource
}

func (e *RecordError) Error() string { return "config: corrupt settings record" }
func (e *RecordError) Unwrap() error { return e.Err }

func descriptors() map[string]wire.KeyDescriptor {
	out := map[string]wire.KeyDescriptor{}
	for _, d := range wire.ConfigKeyDescriptors() {
		out[d.Key] = d
	}
	return out
}

// The published off map remains the one physical field for both wire versions.
func storageKey(key string) string {
	if key == "disabled_download_delegates" {
		return "off"
	}
	return key
}
func catalogueKey(key string) string {
	if key == "off" {
		return "disabled_download_delegates"
	}
	return key
}

// ValidateSetting applies the definition-owned descriptor after structural wire
// validation. It is also used for stored values, never only for client writes.
func ValidateSetting(key string, value *wire.Value) error {
	d, ok := descriptors()[key]
	if !ok {
		return &KeyError{key, "unknown_key"}
	}
	if value == nil {
		return nil
	}
	if err := wire.ValidateConfigValue(value); err != nil {
		return &KeyError{key, "invalid_value"}
	}
	if value.Kind == wire.ValueKindUnlimited {
		if !d.AllowsUnlimited {
			return &KeyError{key, "invalid_value"}
		}
		return nil
	}
	if value.Kind != d.Kind {
		return &KeyError{key, "wrong_kind"}
	}
	var n int64
	numeric := false
	switch value.Kind.String() {
	case "integer":
		n = value.Integer
		numeric = true
	case "bytes":
		n = value.Bytes
		numeric = true
	case "duration":
		n = value.DurationMs
		numeric = true
	}
	if numeric && d.Range != nil && (n < d.Range.Min || n > d.Range.Max) {
		return &KeyError{key, "out_of_range"}
	}
	if value.Kind == wire.ValueKindChoice && len(d.Choices) != 0 {
		found := false
		for _, choice := range d.Choices {
			if value.Choice == choice {
				found = true
				break
			}
		}
		if !found {
			return &KeyError{key, "out_of_range"}
		}
	}
	return nil
}

func decodeSetting(key string, data json.RawMessage) (*wire.Value, error) {
	d, ok := descriptors()[key]
	if !ok {
		return nil, &KeyError{key, "unknown_key"}
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, &KeyError{key, "invalid_value"}
	}
	if err := strictJSONStrings(data); err != nil {
		return nil, &KeyError{key, "invalid_value"}
	}
	v := &wire.Value{Kind: d.Kind}
	if d.AllowsUnlimited && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		var unlimited wire.Value
		if err := wire.FromJSON(data, &unlimited); err == nil && unlimited.Kind == wire.ValueKindUnlimited {
			if err := ValidateSetting(key, &unlimited); err == nil {
				return &unlimited, nil
			}
		}
		return nil, &KeyError{key, "invalid_value"}
	}
	var target any
	switch d.Kind.String() {
	case "boolean":
		target = &v.Boolean
	case "integer":
		target = &v.Integer
	case "bytes":
		target = &v.Bytes
	case "duration":
		target = &v.DurationMs
	case "text":
		target = &v.Text
	case "choice":
		target = &v.Choice
	case "text_list":
		target = &v.TextList
	case "text_map":
		target = &v.TextMap
	default:
		return nil, &KeyError{key, "invalid_value"}
	}
	if err := json.Unmarshal(data, target); err != nil {
		return nil, &KeyError{key, "wrong_kind"}
	}
	if d.Kind == wire.ValueKindTextList {
		var elements []json.RawMessage
		if err := json.Unmarshal(data, &elements); err != nil {
			return nil, &KeyError{key, "wrong_kind"}
		}
		for _, element := range elements {
			if bytes.Equal(bytes.TrimSpace(element), []byte("null")) {
				return nil, &KeyError{key, "invalid_value"}
			}
		}
	}
	if d.Kind == wire.ValueKindTextMap {
		decoder := json.NewDecoder(bytes.NewReader(data))
		if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
			return nil, &KeyError{key, "wrong_kind"}
		}
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return nil, &KeyError{key, "invalid_value"}
			}
			name, ok := token.(string)
			if !ok || seen[name] {
				return nil, &KeyError{key, "invalid_value"}
			}
			seen[name] = true
			value, err := decoder.Token()
			if err != nil {
				return nil, &KeyError{key, "invalid_value"}
			}
			if _, ok := value.(string); !ok {
				return nil, &KeyError{key, "wrong_kind"}
			}
		}
	}
	if err := ValidateSetting(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

func encodeSetting(key string, value *wire.Value) (json.RawMessage, error) {
	if err := ValidateSetting(key, value); err != nil {
		return nil, err
	}
	if value.Kind == wire.ValueKindUnlimited {
		return wire.ToJSON(value)
	}
	var native any
	switch value.Kind.String() {
	case "boolean":
		native = value.Boolean
	case "integer":
		native = value.Integer
	case "bytes":
		native = value.Bytes
	case "duration":
		native = value.DurationMs
	case "text":
		native = value.Text
	case "choice":
		native = value.Choice
	case "text_list":
		native = value.TextList
		if value.TextList == nil {
			native = []string{}
		}
	case "text_map":
		native = value.TextMap
		if value.TextMap == nil {
			native = map[string]string{}
		}
	}
	return json.Marshal(native)
}

func recordFields(data []byte) (map[string]json.RawMessage, error) {
	if data == nil {
		return map[string]json.RawMessage{}, nil
	}
	fields, err := rawUserFields(data)
	if err != nil {
		var record *RecordError
		if errors.As(err, &record) {
			return nil, err
		}
		return nil, &RecordError{Err: err}
	}
	return fields, nil
}

func recordRevision(fields map[string]json.RawMessage) string {
	normalized := make(map[string]json.RawMessage, len(fields))
	catalogue := descriptors()
	for name, raw := range fields {
		normalized[name] = raw
		key := catalogueKey(name)
		if _, known := catalogue[key]; known && storageKey(key) == name {
			if value, err := decodeSetting(key, raw); err == nil {
				if canonical, err := encodeSetting(key, value); err == nil {
					normalized[name] = canonical
				}
			}
		}
	}
	// Unknown values are opaque to this version: even their internal spacing
	// may matter to a future owner, and must participate in its CAS revision.
	data := encodeRecord(normalized, nil)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Existing unknown values are opaque bytes, not values of this catalogue.
// Keep those bytes while rebuilding the enclosing object for a CAS edit.
func encodeRecord(fields map[string]json.RawMessage, members map[string][]byte) []byte {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	catalogue := descriptors()
	out.WriteString("{\n")
	for i, key := range keys {
		if i != 0 {
			out.WriteString(",\n")
		}
		name, _ := json.Marshal(key)
		out.WriteString("  ")
		if raw, ok := members[key]; ok && !knownStoredName(key, catalogue) {
			out.Write(raw)
			continue
		}
		out.Write(name)
		out.WriteString(": ")
		out.Write(fields[key])
	}
	out.WriteString("\n}\n")
	return out.Bytes()
}

func userEntries(fields map[string]json.RawMessage, path string) ([]wire.Entry, error) {
	entries := make([]wire.Entry, 0, len(wire.ConfigKeyDescriptors()))
	for _, d := range wire.ConfigKeyDescriptors() {
		e := wire.Entry{Key: d.Key, Origin: wire.KeyOrigin{State: wire.KeyState("default")}}
		if data, ok := fields[storageKey(d.Key)]; ok {
			e.Origin.Location, e.Origin.Path = wire.LocationUser, path
			v, err := decodeSetting(d.Key, data)
			if err != nil {
				e.Origin.State = wire.KeyState("ignored")
				e.Origin.Reason = "invalid_value"
			} else {
				e.Value = v
				e.Origin.State = wire.KeyState("set")
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func typedUserSnapshot(data []byte, path string) (wire.UserSnapshotV2, error) {
	fields, err := recordFields(data)
	if err != nil {
		return wire.UserSnapshotV2{}, err
	}
	entries, err := userEntries(fields, path)
	if err != nil {
		return wire.UserSnapshotV2{}, &RecordError{Err: err}
	}
	return wire.UserSnapshotV2{Entries: entries, Revision: recordRevision(fields)}, err
}

func ReadTypedUserStore(store casapi.Store, path string) (wire.UserSnapshotV2, error) {
	value, err := readUserValue(store, path)
	if err != nil {
		return wire.UserSnapshotV2{}, err
	}
	return typedUserSnapshot(value.Data, path)
}

// ReplaceTypedUserStore edits this same CAS record atomically. An absent Set
// removes the field, and a conflict returns the current normalized snapshot.
func ReplaceTypedUserStore(store casapi.Store, path, expected string, changes []wire.Change) (wire.UserSnapshotV2, bool, error) {
	if len(changes) > 16 {
		return wire.UserSnapshotV2{}, false, errors.New("config: too many changes")
	}
	seen := map[string]bool{}
	for _, c := range changes {
		if seen[c.Key] {
			return wire.UserSnapshotV2{}, false, &KeyError{c.Key, "duplicate_key"}
		}
		seen[c.Key] = true
		if err := ValidateSetting(c.Key, c.Set); err != nil {
			return wire.UserSnapshotV2{}, false, err
		}
	}
	for attempts := 0; attempts < 32; attempts++ {
		base, err := readUserValue(store, path)
		if err != nil {
			return wire.UserSnapshotV2{}, false, err
		}
		record, err := storedRecord(base.Data)
		if err != nil {
			return wire.UserSnapshotV2{}, false, err
		}
		fields := record.Fields
		current, err := typedUserSnapshot(base.Data, path)
		if err != nil {
			return wire.UserSnapshotV2{}, false, err
		}
		if current.Revision != expected {
			return current, false, nil
		}
		for _, c := range changes {
			key := storageKey(c.Key)
			if c.Set == nil {
				delete(fields, key)
				continue
			}
			fields[key], err = encodeSetting(c.Key, c.Set)
			if err != nil {
				return wire.UserSnapshotV2{}, false, err
			}
		}
		data := encodeRecord(fields, record.Members)
		if len(data) > MaxUserFileBytes {
			return wire.UserSnapshotV2{}, false, cas.ErrTooLarge
		}
		next, err := typedUserSnapshot(data, path)
		if err != nil {
			return wire.UserSnapshotV2{}, false, err
		}
		if err := store.Write(path, base, data); errors.Is(err, cas.ErrMoved) {
			continue
		} else if err != nil {
			return wire.UserSnapshotV2{}, false, fmt.Errorf("config: write user settings: %w", err)
		}
		announce()
		return next, true, nil
	}
	return wire.UserSnapshotV2{}, false, cas.ErrMoved
}

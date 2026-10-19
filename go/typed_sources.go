package config

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

// TypedSources selects the existing provider-owned rungs. DynamicDefault is
// supplied by the owning runtime (for example logging's existing drive policy).
// RestartValues is the snapshot actually admitted by a restart-only consumer;
// it is not another persistent settings store.
type TypedSources struct {
	UserStore             casapi.Store
	UserPath, MachinePath string
	DynamicDefault        func(string) (*wire.Value, error)
	RestartValues         map[string]*wire.Value
}

type TypedSnapshot struct {
	Entries        []wire.Entry
	IgnoredFields  []wire.IgnoredField
	IgnoredOmitted int64
}

func (s TypedSources) machineFields() (map[string]json.RawMessage, error) {
	return s.machineFieldsWithTrust(trusted)
}
func (s TypedSources) machineFieldsWithTrust(checkTrust func(string) error) (map[string]json.RawMessage, error) {
	record, err := s.machineRecordWithTrust(checkTrust)
	return record.Fields, err
}
func (s TypedSources) machineRecordWithTrust(checkTrust func(string) error) (userRecord, error) {
	if s.MachinePath == "" {
		return storedRecord(nil)
	}
	if err := checkTrust(s.MachinePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return storedRecord(nil)
		}
		// An untrusted machine file has no administrator authority.
		return storedRecord(nil)
	}
	value, err := readUserValue(casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}, s.MachinePath)
	if err != nil {
		return userRecord{}, err
	}
	return storedRecord(value.Data)
}

// Read returns effective entries without a process rung. Clients apply the
// generated common overlay after this service result; machine locks survive it.
func (s TypedSources) Read() ([]wire.Entry, error) {
	return s.readKeysWithTrust(nil, trusted)
}

// ReadKeys avoids invoking dynamic defaults for keys the caller did not ask for.
// A nil names set selects the complete catalogue.
func (s TypedSources) ReadKeys(names map[string]bool) ([]wire.Entry, error) {
	return s.readKeysWithTrust(names, trusted)
}
func (s TypedSources) readKeysWithTrust(names map[string]bool, checkTrust func(string) error) ([]wire.Entry, error) {
	snapshot, err := s.readSnapshotWithTrust(names, checkTrust)
	return snapshot.Entries, err
}
func (s TypedSources) ReadSnapshot(names map[string]bool) (TypedSnapshot, error) {
	return s.readSnapshotWithTrust(names, trusted)
}
func (s TypedSources) readSnapshotWithTrust(names map[string]bool, checkTrust func(string) error) (TypedSnapshot, error) {
	user, err := readUserValue(s.UserStore, s.UserPath)
	if err != nil {
		return TypedSnapshot{}, err
	}
	userRecord, err := storedRecord(user.Data)
	if err != nil {
		return TypedSnapshot{}, recordSource(err, wire.LocationUser, s.UserPath)
	}
	machineRecord, err := s.machineRecordWithTrust(checkTrust)
	if err != nil {
		return TypedSnapshot{}, recordSource(err, wire.LocationMachine, s.MachinePath)
	}
	userFields, machineFields := userRecord.Fields, machineRecord.Fields
	entries := make([]wire.Entry, 0, len(wire.ConfigKeyDescriptors()))
	for _, d := range wire.ConfigKeyDescriptors() {
		if names != nil && !names[d.Key] {
			continue
		}
		e := wire.Entry{Key: d.Key, Value: d.Default, Origin: wire.KeyOrigin{State: wire.KeyState("default")}}
		machine, hasMachine := machineFields[storageKey(d.Key)]
		user, hasUser := userFields[storageKey(d.Key)]
		var machineValue *wire.Value
		var machineErr error
		if hasMachine {
			machineValue, machineErr = decodeSetting(d.Key, machine)
		}
		machineLocked := hasMachine && machineErr == nil && d.Lockable
		var selected json.RawMessage
		if hasMachine {
			selected = machine
			e.Value = machineValue
			e.Origin = wire.KeyOrigin{Location: wire.LocationMachine, Path: s.MachinePath, State: wire.KeyState("set")}
			if machineLocked {
				e.Origin.State = wire.KeyState("locked")
				if hasUser {
					e.Origin.Shadowed = []wire.ConfigSource{{Location: wire.LocationUser, Path: s.UserPath}}
				}
			}
		}
		if hasUser && !machineLocked {
			selected = user
			origin := wire.KeyOrigin{Location: wire.LocationUser, Path: s.UserPath, State: wire.KeyState("set")}
			if hasMachine {
				origin.Shadowed = []wire.ConfigSource{{Location: wire.LocationMachine, Path: s.MachinePath}}
			}
			e.Origin = origin
		}
		if selected != nil {
			value, decodeErr := decodeSetting(d.Key, selected)
			if decodeErr != nil {
				e.Value = d.Default
				e.Origin.State = wire.KeyState("ignored")
				e.Origin.Reason = "invalid_value"
			} else {
				e.Value = value
			}
		}
		if e.Value == nil && d.Default == nil && d.DefaultDescription != "" {
			if s.DynamicDefault == nil {
				return TypedSnapshot{}, errors.New("config: dynamic default unavailable: " + d.Key)
			}
			e.Value, err = s.DynamicDefault(d.Key)
			if err != nil {
				return TypedSnapshot{}, err
			}
			if e.Value == nil {
				return TypedSnapshot{}, errors.New("config: dynamic default unavailable: " + d.Key)
			}
			if err := ValidateSetting(d.Key, e.Value); err != nil {
				return TypedSnapshot{}, err
			}
		}
		if d.Apply == wire.ApplyModeRestart && e.Origin.State != wire.KeyState("locked") && e.Origin.State != wire.KeyState("ignored") {
			if admitted, ok := s.RestartValues[d.Key]; ok && !reflect.DeepEqual(admitted, e.Value) {
				e.Origin.State = wire.KeyState("pending")
				e.Origin.Reason = "Takes effect after restart"
			}
		}
		entries = append(entries, e)
	}
	ignored, omitted := ignoredStoredFields(sourcedRecord{userRecord, wire.ConfigSource{Location: wire.LocationUser, Path: s.UserPath}}, sourcedRecord{machineRecord, wire.ConfigSource{Location: wire.LocationMachine, Path: s.MachinePath}})
	return TypedSnapshot{Entries: entries, IgnoredFields: ignored, IgnoredOmitted: omitted}, nil
}

func recordSource(err error, location wire.Location, path string) error {
	var record *RecordError
	if errors.As(err, &record) {
		copy := *record
		copy.Source = &wire.ConfigSource{Location: location, Path: path}
		return &copy
	}
	return err
}

// CheckChanges validates the whole batch against the current trusted machine
// rung before any user CAS write. It never silently edits a locked setting.
func (s TypedSources) CheckChanges(changes []wire.Change) error {
	machine, err := s.machineFields()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range changes {
		if seen[c.Key] {
			return &KeyError{c.Key, "duplicate_key"}
		}
		seen[c.Key] = true
		if err := ValidateSetting(c.Key, c.Set); err != nil {
			return err
		}
		if raw, set := machine[storageKey(c.Key)]; set && descriptors()[c.Key].Lockable {
			if _, err := decodeSetting(c.Key, raw); err == nil {
				return &KeyError{c.Key, "locked:machine"}
			}
		}
	}
	return nil
}

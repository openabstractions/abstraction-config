package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openabstractions/abstraction-cas/go"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
)

// MaxUserFileBytes leaves room for nesting and the reply envelope within the 1MiB frame limit.
const MaxUserFileBytes = 256 << 10

// UserFileSnapshot is provider-side user content; it never merges other rungs.
type UserFileSnapshot struct {
	Values   Config
	Revision string
}

func userSnapshot(data []byte) (UserFileSnapshot, error) {
	var values Config
	if data != nil {
		if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			return UserFileSnapshot{}, errors.New("config: user settings must be an object")
		}
		fields, err := recordFields(data)
		if err != nil {
			return UserFileSnapshot{}, err
		}
		for key, value := range fields {
			switch key {
			case "nas_store", "store", "log_sink", "log_service", "off":
			default:
				continue
			}
			if err := strictJSONStrings(value); err != nil {
				return UserFileSnapshot{}, err
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return UserFileSnapshot{}, errors.New("config: null user setting")
			}
			if key == "off" {
				entries := json.NewDecoder(bytes.NewReader(value))
				start, err := entries.Token()
				if err != nil || start != json.Delim('{') {
					return UserFileSnapshot{}, errors.New("config: off must be an object")
				}
				for entries.More() {
					if _, err := entries.Token(); err != nil {
						return UserFileSnapshot{}, err
					}
					entry, err := entries.Token()
					if err != nil {
						return UserFileSnapshot{}, err
					}
					if _, ok := entry.(string); !ok {
						return UserFileSnapshot{}, errors.New("config: off reason must be a string")
					}
				}
				if _, err := entries.Token(); err != nil {
					return UserFileSnapshot{}, err
				}
			}
		}
		legacy := map[string]json.RawMessage{}
		for _, key := range Keys {
			if value, ok := fields[key]; ok {
				legacy[key] = value
			}
		}
		projected, err := json.Marshal(legacy)
		if err != nil {
			return UserFileSnapshot{}, err
		}
		decoder := json.NewDecoder(bytes.NewReader(projected))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&values); err != nil {
			return UserFileSnapshot{}, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return UserFileSnapshot{}, errors.New("config: trailing user settings")
		}
	}
	normalized, err := json.Marshal(values)
	if err != nil {
		return UserFileSnapshot{}, err
	}
	if data != nil {
		fields, err := recordFields(data)
		if err != nil {
			return UserFileSnapshot{}, err
		}
		// Include new keys in the opaque @1 revision so an @2 edit conflicts.
		for _, key := range Keys {
			delete(fields, key)
		}
		if len(fields) != 0 {
			legacy := map[string]json.RawMessage{}
			if err := json.Unmarshal(normalized, &legacy); err != nil {
				return UserFileSnapshot{}, err
			}
			for key, value := range fields {
				legacy[key] = value
			}
			normalized = encodeRecord(legacy, nil)
		}
	}
	hash := sha256.Sum256(normalized)
	return UserFileSnapshot{values, "user-v1:" + hex.EncodeToString(hash[:])}, nil
}

// ReadUserFile reads the provider-selected user path. Only a missing file is
// empty; unsupported content and storage errors remain errors.
func ReadUserFile(path string) (UserFileSnapshot, error) {
	return ReadUserStore(casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}, path)
}

// ReadUserStore reads provider-selected mutable state through the shared direct
// CAS contract. Config owns parsing and normalized revisions. Missing is empty;
// an existing empty/corrupt record or storage failure remains an error.
func ReadUserStore(store casapi.Store, path string) (UserFileSnapshot, error) {
	_, snapshot, err := readUserStore(store, path)
	return snapshot, err
}
func readUserValue(store casapi.Store, path string) (casapi.Value, error) {
	if store == nil || strings.TrimSpace(path) == "" {
		return casapi.Value{}, errors.New("config: user storage unavailable")
	}
	value, err := store.Read(path)
	if err != nil {
		return value, err
	}
	if len(value.Data) > MaxUserFileBytes {
		return value, cas.ErrTooLarge
	}
	return value, nil
}
func readUserStore(store casapi.Store, path string) (casapi.Value, UserFileSnapshot, error) {
	value, err := readUserValue(store, path)
	if err != nil {
		return value, UserFileSnapshot{}, err
	}
	snapshot, err := userSnapshot(value.Data)
	return value, snapshot, err
}

func ReplaceUserFile(path, expected string, values Config) (UserFileSnapshot, bool, error) {
	return ReplaceUserStore(casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}, path, expected, values)
}

// ReplaceUserStore compares config's revision and uses the exact byte snapshot
// as the shared Store comparison base. Moved rereads the revision; it never
// overwrites an intervening different value. Contention exhaustion is an error.
func ReplaceUserStore(store casapi.Store, path, expected string, values Config) (UserFileSnapshot, bool, error) {
	legacy, err := json.Marshal(values)
	if err != nil {
		return UserFileSnapshot{}, false, err
	}
	if _, err := userSnapshot(legacy); err != nil {
		return UserFileSnapshot{}, false, err
	}
	for attempts := 0; attempts < 32; attempts++ {
		base, current, err := readUserStore(store, path)
		if err != nil {
			return UserFileSnapshot{}, false, err
		}
		if current.Revision != expected {
			return current, false, nil
		}
		record, err := storedRecord(base.Data)
		if err != nil {
			return UserFileSnapshot{}, false, err
		}
		fields := record.Fields
		for _, key := range Keys {
			delete(fields, key)
		}
		var replacement map[string]json.RawMessage
		if err := json.Unmarshal(legacy, &replacement); err != nil {
			return UserFileSnapshot{}, false, err
		}
		for key, value := range replacement {
			fields[key] = value
		}
		data := encodeRecord(fields, record.Members)
		if len(data) > MaxUserFileBytes {
			return UserFileSnapshot{}, false, cas.ErrTooLarge
		}
		next, err := userSnapshot(data)
		if err != nil {
			return UserFileSnapshot{}, false, err
		}
		if err = store.Write(path, base, data); errors.Is(err, cas.ErrMoved) {
			continue
		} else if err != nil {
			return UserFileSnapshot{}, false, err
		}
		announce()
		return next, true, nil
	}
	return UserFileSnapshot{}, false, cas.ErrMoved
}

// LoadWithUserStore composes the same user snapshot used by editing with the
// existing trusted machine rung and explicit caller run overrides. Provenance
// stays config-owned; a selected user backend failure is not an empty setting.
func LoadWithUserStore(store casapi.Store, path string, overrides map[string]string) (Config, error) {
	return LoadWithSources(store, path, MachinePath(), overrides)
}

// LoadWithSources is LoadWithUserStore with the machine rung read from
// machinePath. An empty machinePath reads no machine rung: a runtime isolated
// from the installation reads neither the administrator's file nor its
// directory.
func LoadWithSources(store casapi.Store, path, machinePath string, overrides map[string]string) (Config, error) {
	return loadWithSources(store, path, machinePath, trusted, overrides)
}

func loadWithSources(store casapi.Store, path, machinePath string, trust func(string) error, overrides map[string]string) (Config, error) {
	value, err := readUserValue(store, path)
	if err != nil {
		return Config{}, err
	}
	var machine Config
	if machinePath != "" {
		if loaded, e := readMachineSource(machinePath, trust); e == nil {
			machine = loaded
		} else {
			var readErr *machineReadError
			if errors.As(e, &readErr) {
				return Config{}, e
			}
		}
	}
	// Keep the merged reader's established malformed-source fallback. Editing
	// still requires the strict userSnapshot parser and never clobbers it.
	user, decodeErr := decodeSource(value.Data, path, User)
	if decodeErr != nil {
		user = Config{}
	}
	for _, key := range Keys {
		if user.set(key) {
			user.from(key, User, path)
		}
	}
	return applyEnvironment(merge(machine, user), func(name string) string { return overrides[name] }), nil
}

// Validate the original strings before encoding/json can replace unsupported
// UTF-8 or unpaired UTF-16 escapes. Record field names are exact and unique.
func strictUserFields(data []byte) (map[string]json.RawMessage, error) {
	if err := strictJSONStrings(data); err != nil {
		return nil, err
	}
	return rawUserFields(data)
}

func strictJSONStrings(data []byte) error {
	bad := errors.New("config: unsupported user record")
	if !utf8.Valid(data) {
		return bad
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		i++
		for i < len(data) && data[i] != '"' {
			if data[i] != '\\' {
				i++
				continue
			}
			escapeStart := i
			i++
			if i >= len(data) {
				return &RecordError{Err: bad, Offset: int64(escapeStart)}
			}
			if data[i] != 'u' {
				i++
				continue
			}
			if i+4 >= len(data) {
				return &RecordError{Err: bad, Offset: int64(escapeStart)}
			}
			value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			if err != nil {
				return &RecordError{Err: bad, Offset: int64(escapeStart)}
			}
			i += 5
			if value >= 0xdc00 && value <= 0xdfff {
				return &RecordError{Err: bad, Offset: int64(escapeStart)}
			}
			if value >= 0xd800 && value <= 0xdbff {
				if i+6 > len(data) || data[i] != '\\' || data[i+1] != 'u' {
					return &RecordError{Err: bad, Offset: int64(escapeStart)}
				}
				low, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return &RecordError{Err: bad, Offset: int64(escapeStart)}
				}
				i += 6
			}
		}
	}
	return nil
}

type userRecord struct {
	Fields  map[string]json.RawMessage
	Offsets map[string]int64
	Members map[string][]byte
}

func rawUserFields(data []byte) (map[string]json.RawMessage, error) {
	record, err := parseUserRecord(data)
	return record.Fields, err
}
func parseUserRecord(data []byte) (userRecord, error) {
	bad := errors.New("config: unsupported user record")
	if !utf8.Valid(data) {
		for offset := 0; offset < len(data); {
			r, n := utf8.DecodeRune(data[offset:])
			if r == utf8.RuneError && n == 1 {
				return userRecord{}, &RecordError{Err: bad, Offset: int64(offset)}
			}
			offset += n
		}
	}
	first := len(data) - len(bytes.TrimLeft(data, " \r\n\t"))
	if first == len(data) || data[first] != '{' {
		return userRecord{}, &RecordError{Err: bad, Offset: int64(first)}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	failure := func(err error) error {
		offset := decoder.InputOffset()
		var syntax *json.SyntaxError
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			offset = int64(len(data))
		} else {
			// Decoder.Token's stream scanner does not count punctuation like a
			// whole-record decoder. Locate a syntax failure in the original bytes.
			var raw json.RawMessage
			completeErr := json.Unmarshal(data, &raw)
			if errors.As(completeErr, &syntax) {
				offset = syntax.Offset - 1
			}
		}
		if offset < 0 {
			offset = 0
		}
		return &RecordError{Err: err, Offset: offset}
	}
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		if err != nil {
			return userRecord{}, failure(err)
		}
		return userRecord{}, &RecordError{Err: bad, Offset: 0}
	}
	fields := map[string]json.RawMessage{}
	offsets := map[string]int64{}
	members := map[string][]byte{}
	for decoder.More() {
		keyStart := decoder.InputOffset()
		for keyStart < int64(len(data)) && data[keyStart] != '"' {
			keyStart++
		}
		token, err := decoder.Token()
		if err != nil {
			return userRecord{}, failure(err)
		}
		key, ok := token.(string)
		if !ok {
			return userRecord{}, &RecordError{Err: bad, Offset: keyStart}
		}
		if err := strictJSONStrings(data[keyStart:decoder.InputOffset()]); err != nil {
			offset := keyStart
			var scalar *RecordError
			if errors.As(err, &scalar) {
				offset += scalar.Offset
			}
			return userRecord{}, &RecordError{Err: err, Offset: offset}
		}
		if _, duplicate := fields[key]; duplicate {
			return userRecord{}, &RecordError{Err: bad, Offset: keyStart}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return userRecord{}, failure(err)
		}
		fields[key] = value
		offsets[key] = keyStart
		members[key] = append([]byte(nil), data[keyStart:decoder.InputOffset()]...)
	}
	if _, err := decoder.Token(); err != nil {
		return userRecord{}, failure(err)
	}
	end := decoder.InputOffset()
	rest := bytes.TrimLeft(data[end:], " \r\n\t")
	if len(rest) != 0 {
		return userRecord{}, &RecordError{Err: bad, Offset: int64(len(data) - len(rest))}
	}
	return userRecord{Fields: fields, Offsets: offsets, Members: members}, nil
}

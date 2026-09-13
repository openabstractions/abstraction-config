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
		fields, err := strictUserFields(data)
		if err != nil {
			return UserFileSnapshot{}, err
		}
		for key, value := range fields {
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
		decoder := json.NewDecoder(bytes.NewReader(data))
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
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return UserFileSnapshot{}, false, err
	}
	data = append(data, '\n')
	if len(data) > MaxUserFileBytes {
		return UserFileSnapshot{}, false, cas.ErrTooLarge
	}
	next, err := userSnapshot(data)
	if err != nil {
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
	value, err := readUserValue(store, path)
	if err != nil {
		return Config{}, err
	}
	var machine Config
	if p := MachinePath(); p != "" {
		if loaded, e := readMachineBounded(p); e == nil {
			machine = loaded
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
	bad := errors.New("config: unsupported user record")
	if !utf8.Valid(data) {
		return nil, bad
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
			i++
			if i >= len(data) {
				return nil, bad
			}
			if data[i] != 'u' {
				i++
				continue
			}
			if i+4 >= len(data) {
				return nil, bad
			}
			value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			if err != nil {
				return nil, bad
			}
			i += 5
			if value >= 0xdc00 && value <= 0xdfff {
				return nil, bad
			}
			if value >= 0xd800 && value <= 0xdbff {
				if i+6 > len(data) || data[i] != '\\' || data[i+1] != 'u' {
					return nil, bad
				}
				low, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return nil, bad
				}
				i += 6
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, bad
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, bad
		}
		switch key {
		case "nas_store", "store", "log_sink", "log_service", "off":
		default:
			return nil, bad
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, bad
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, bad
	}
	return fields, nil
}

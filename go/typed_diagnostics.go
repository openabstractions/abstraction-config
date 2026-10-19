package config

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

func storedRecord(data []byte) (userRecord, error) {
	if data == nil {
		return userRecord{Fields: map[string]json.RawMessage{}, Offsets: map[string]int64{}, Members: map[string][]byte{}}, nil
	}
	return parseUserRecord(data)
}

func knownStoredName(name string, catalogue map[string]wire.KeyDescriptor) bool {
	// These remain supported through the published @1 projection.
	if name == "store" || name == "log_sink" || name == "log_service" {
		return true
	}
	key := catalogueKey(name)
	_, known := catalogue[key]
	return known && storageKey(key) == name
}

func nameExcerpt(name string) string {
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return utf8.RuneError
		}
		return r
	}, strings.ToValidUTF8(name, "\ufffd"))
	if len(text) <= 64 {
		return text
	}
	prefix := text[:61]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + "…"
}

type sourcedRecord struct {
	userRecord
	source wire.ConfigSource
}

func ignoredStoredFields(records ...sourcedRecord) ([]wire.IgnoredField, int64) {
	catalogue := descriptors()
	var fields []wire.IgnoredField
	var omitted int64
	for _, record := range records {
		names := make([]string, 0, len(record.Fields))
		for name := range record.Fields {
			if !knownStoredName(name, catalogue) {
				names = append(names, name)
			}
		}
		sort.Slice(names, func(i, j int) bool { return record.Offsets[names[i]] < record.Offsets[names[j]] })
		for _, name := range names {
			if len(fields) == 16 {
				omitted++
				continue
			}
			reason := wire.IgnoredFieldReason("unknown_key")
			if _, err := wire.ToJSON(&wire.Change{Key: name}); err != nil {
				reason = wire.IgnoredFieldReason("invalid_name")
			}
			fields = append(fields, wire.IgnoredField{Offset: record.Offsets[name], NameExcerpt: nameExcerpt(name), Reason: reason, Source: record.source})
		}
	}
	return fields, omitted
}

package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

func typedEntry(t *testing.T, entries []wire.Entry, key string) wire.Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.Key == key {
			return entry
		}
	}
	t.Fatal("missing entry", key)
	return wire.Entry{}
}

func TestRetiredPromptCacheKeysPreserveStoredContent(t *testing.T) {
	expected := []string{"cache_max_use", "cache_max_retention", "tmp_max_retention", "logs_max_use", "logs_max_retention", "messages_start_questions", "messages_start_notes", "messages_start_per_hour", "messages_start_open_app", "prompt_cache_persist", "prompt_cache_max_use", "model_context", "nas_store", "disabled_download_delegates"}
	descriptors := wire.ConfigKeyDescriptors()
	if len(descriptors) != len(expected) {
		t.Fatalf("catalogue size: %d", len(descriptors))
	}
	for i, key := range expected {
		if descriptors[i].Key != key {
			t.Fatalf("catalogue[%d]: %s", i, descriptors[i].Key)
		}
	}
	for _, key := range []string{"prompt_cache_max_retention", "prompt_cache_off_for"} {
		var problem *KeyError
		if err := ValidateSetting(key, nil); !errors.As(err, &problem) || problem.Reason != "unknown_key" {
			t.Fatalf("retired key %s: %v", key, err)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	store := casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	const retiredAge = `"prompt_cache_max_retention": [ 86400000 , null ]`
	const retiredApps = `"prompt_cache_off_for": [ "fixture.app" , "future.app" ]`
	if err := os.WriteFile(path, []byte(`{`+retiredAge+`,`+retiredApps+`,"prompt_cache_persist":false,"prompt_cache_max_use":1234}`), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceTypedUserStore(store, path, first.Revision, []wire.Change{{Key: "logs_max_retention", Set: &wire.Value{Kind: wire.ValueKindDuration, DurationMs: 1000}}}); err != nil || !applied {
		t.Fatal(applied, err)
	}
	legacy, err := ReadUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceUserStore(store, path, legacy.Revision, Config{NASStore: "fixture-address"}); err != nil || !applied {
		t.Fatal(applied, err)
	}
	current, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if value := typedEntry(t, current.Entries, "prompt_cache_persist").Value; value == nil || value.Boolean {
		t.Fatal("retained future toggle changed", value)
	}
	if value := typedEntry(t, current.Entries, "prompt_cache_max_use").Value; value == nil || value.Bytes != 1234 {
		t.Fatal("retained future bound changed", value)
	}
	data, err := os.ReadFile(path)
	// Unknown member value bytes survive both owners. Whole-file layout may change.
	if err != nil || !bytes.Contains(data, []byte(`[ 86400000 , null ]`)) || !bytes.Contains(data, []byte(`[ "fixture.app" , "future.app" ]`)) {
		t.Fatal("retired stored content changed", string(data), err)
	}
}
func TestTypedStoreLegacyWritePreservesNewKeysAndConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	if err := os.WriteFile(path, []byte(`{"future_setting":{"nested": [ false, 0 , null ]},"future_null":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := ReadUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	next, applied, err := ReplaceTypedUserStore(store, path, initial.Revision, []wire.Change{{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean, Boolean: true}}})
	if err != nil || !applied {
		t.Fatal(next, applied, err)
	}
	if _, applied, err := ReplaceUserStore(store, path, legacy.Revision, Config{NASStore: "stale"}); err != nil || applied {
		t.Fatal("@1 missed @2 conflict", applied, err)
	}
	legacy, err = ReadUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceUserStore(store, path, legacy.Revision, Config{NASStore: "new", Off: map[string]string{"http": "operator"}}); err != nil || !applied {
		t.Fatal(applied, err)
	}
	reopened, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if v := typedEntry(t, reopened.Entries, "messages_start_notes").Value; v == nil || !v.Boolean {
		t.Fatal("@1 removed @2 setting", v)
	}
	if v := typedEntry(t, reopened.Entries, "disabled_download_delegates").Value; v == nil || v.TextMap["http"] != "operator" {
		t.Fatal("legacy map projection", v)
	}
	if _, applied, err := ReplaceTypedUserStore(store, path, reopened.Revision, []wire.Change{{Key: "messages_start_notes"}}); err != nil || !applied {
		t.Fatal(applied, err)
	}
	final, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if typedEntry(t, final.Entries, "messages_start_notes").Value != nil {
		t.Fatal("unset retained a value")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(`"future_setting"`)) || !bytes.Contains(data, []byte(`{"nested": [ false, 0 , null ]}`)) {
		t.Fatal("future content was dropped", string(data), err)
	}
}

func TestTypedStoreInvalidBatchIsAtomic(t *testing.T) {
	for _, invalid := range []wire.Change{
		{Key: "unknown", Set: &wire.Value{Kind: wire.ValueKindBoolean}},
		{Key: "messages_start_questions", Set: &wire.Value{Kind: wire.ValueKindText, Text: "false"}},
		{Key: "messages_start_per_hour", Set: &wire.Value{Kind: wire.ValueKindChoice, Choice: "3"}},
		{Key: "prompt_cache_max_use", Set: &wire.Value{Kind: wire.ValueKindBytes, Bytes: -1}},
		{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean, Text: "extra payload"}},
	} {
		t.Run(invalid.Key+invalid.Set.Kind.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			original := []byte(`{"nas_store":"retained"}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			store := casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}
			snapshot, err := ReadTypedUserStore(store, path)
			if err != nil {
				t.Fatal(err)
			}
			changes := []wire.Change{{Key: "nas_store", Set: &wire.Value{Kind: wire.ValueKindText, Text: "changed"}}, invalid}
			if _, applied, err := ReplaceTypedUserStore(store, path, snapshot.Revision, changes); err == nil || applied {
				t.Fatal("invalid batch accepted", applied, err)
			}
			data, _ := os.ReadFile(path)
			if !bytes.Equal(data, original) {
				t.Fatal("invalid batch changed file", string(data))
			}
		})
	}
}

func TestTypedSourcesMachineLockProcessOverlayAndPending(t *testing.T) {
	dir := t.TempDir()
	user, machine := filepath.Join(dir, "user"), filepath.Join(dir, "machine")
	if err := os.WriteFile(user, []byte(`{"messages_start_questions":true,"nas_store":"new"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(machine, []byte(`{"messages_start_questions":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	sources := TypedSources{UserStore: casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}, UserPath: user, MachinePath: machine, RestartValues: map[string]*wire.Value{"nas_store": {Kind: wire.ValueKindText, Text: "old"}}}
	names := map[string]bool{"messages_start_questions": true, "nas_store": true}
	entries, err := sources.readKeysWithTrust(names, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	locked := typedEntry(t, entries, "messages_start_questions")
	if locked.Value.Boolean || locked.Origin.State != wire.KeyState("locked") || len(locked.Origin.Shadowed) != 1 {
		t.Fatal(locked)
	}
	if pending := typedEntry(t, entries, "nas_store"); pending.Origin.State != wire.KeyState("pending") {
		t.Fatal(pending)
	}
	overlaid, err := wire.OverlayConfigProcess(entries, []wire.Change{{Key: "messages_start_questions", Set: &wire.Value{Kind: wire.ValueKindBoolean, Boolean: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if typedEntry(t, overlaid, "messages_start_questions").Value.Boolean {
		t.Fatal("process bypassed machine lock")
	}
	entries, err = sources.readKeysWithTrust(names, func(string) error { return errors.New("untrusted planter") })
	if err != nil {
		t.Fatal(err)
	}
	if entry := typedEntry(t, entries, "messages_start_questions"); !entry.Value.Boolean || entry.Origin.State == wire.KeyState("locked") {
		t.Fatal("untrusted machine gained authority", entry)
	}
}

func TestTypedStoredInvalidValueIsIgnored(t *testing.T) {
	for key, data := range map[string]string{"messages_start_per_hour": `{"messages_start_per_hour":"3"}`, "messages_start_questions": `{"messages_start_questions":"true"}`, "messages_start_notes": `{"messages_start_notes":null}`, "nas_store": `{"nas_store":"\ud800"}`} {
		snapshot, err := typedUserSnapshot([]byte(data), "test")
		if err != nil {
			t.Fatal(err)
		}
		entry := typedEntry(t, snapshot.Entries, key)
		if entry.Origin.State != wire.KeyState("ignored") || entry.Origin.Reason != "invalid_value" || entry.Value != nil {
			t.Fatal("missing ignored key", data, entry)
		}
	}
}

func TestTypedRecordFailureByteOffsets(t *testing.T) {
	for _, data := range []string{`{"messages_start_notes":!}`, `{"messages_start_notes":`, `{"messages_start_notes":true,"messages_start_notes":false}`, `{} {}`, `{"\ud800":true}`, " \n[]", ""} {
		_, err := typedUserSnapshot([]byte(data), "test")
		var record *RecordError
		if !errors.As(err, &record) {
			t.Fatal("missing corrupt record", data, err)
		}
		want := int64(len(data))
		if at := strings.IndexByte(data, '!'); at >= 0 {
			want = int64(at)
		}
		if at := strings.LastIndex(data, `"messages_start_notes"`); at > 1 {
			want = int64(at)
		}
		if at := strings.Index(data, `\ud800`); at >= 0 {
			want = int64(at)
		}
		if data == `{} {}` {
			want = 3
		}
		if data == " \n[]" {
			want = 2
		}
		if record.Offset != want {
			t.Fatal("wrong byte offset", data, record.Offset, want)
		}
	}
}

func TestTypedUnlimitedZeroAndUnsetRemainDistinct(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	sources := TypedSources{UserStore: store, UserPath: path, DynamicDefault: func(key string) (*wire.Value, error) { return &wire.Value{Kind: wire.ValueKindBytes, Bytes: 999}, nil }}
	for _, value := range []*wire.Value{{Kind: wire.ValueKindUnlimited}, {Kind: wire.ValueKindBytes}, nil} {
		before, err := ReadTypedUserStore(store, path)
		if err != nil {
			t.Fatal(err)
		}
		if _, applied, err := ReplaceTypedUserStore(store, path, before.Revision, []wire.Change{{Key: "logs_max_use", Set: value}}); err != nil || !applied {
			t.Fatal(applied, err)
		}
		entries, err := sources.ReadKeys(map[string]bool{"logs_max_use": true})
		if err != nil {
			t.Fatal(err)
		}
		got := typedEntry(t, entries, "logs_max_use").Value
		if value == nil {
			if got.Kind != wire.ValueKindBytes || got.Bytes != 999 {
				t.Fatal("unset lost dynamic default", got)
			}
		} else if got.Kind != value.Kind || got.Bytes != value.Bytes {
			t.Fatal("unlimited or zero changed", value, got)
		}
	}
	before, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceTypedUserStore(store, path, before.Revision, []wire.Change{{Key: "prompt_cache_max_use", Set: &wire.Value{Kind: wire.ValueKindUnlimited}}}); err == nil || applied {
		t.Fatal("unlimited bypassed catalogue restriction", applied, err)
	}
}

func TestTypedIgnoredValueKeepsOtherSettingsAndCanBeRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	if err := os.WriteFile(path, []byte(`{"messages_start_questions":"bad","messages_start_notes":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	sources := TypedSources{UserStore: store, UserPath: path}
	entries, err := sources.ReadKeys(map[string]bool{"messages_start_questions": true, "messages_start_notes": true})
	if err != nil {
		t.Fatal(err)
	}
	ignored := typedEntry(t, entries, "messages_start_questions")
	valid := typedEntry(t, entries, "messages_start_notes")
	if ignored.Value == nil || !ignored.Value.Boolean || ignored.Origin.State != wire.KeyState("ignored") || ignored.Origin.Reason != "invalid_value" || !valid.Value.Boolean || valid.Origin.State != wire.KeyState("set") {
		t.Fatal(ignored, valid)
	}
	before, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceTypedUserStore(store, path, before.Revision, []wire.Change{{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean}}}); err != nil || !applied {
		t.Fatal("valid edit blocked by other ignored key", applied, err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte(`"messages_start_questions": "bad"`)) {
		t.Fatal("unrelated ignored raw value clobbered", string(data))
	}
	before, err = ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceTypedUserStore(store, path, before.Revision, []wire.Change{{Key: "messages_start_questions", Set: &wire.Value{Kind: wire.ValueKindBoolean}}}); err != nil || !applied {
		t.Fatal("could not repair ignored key", applied, err)
	}
	entries, err = sources.ReadKeys(map[string]bool{"messages_start_questions": true})
	if err != nil {
		t.Fatal(err)
	}
	if got := entries[0]; got.Value.Boolean || got.Origin.State != wire.KeyState("set") {
		t.Fatal(got)
	}
}

func TestTypedAuthorityAndGeneratedOverlayAgree(t *testing.T) {
	cases := []struct {
		key   string
		value *wire.Value
		valid bool
	}{
		{"logs_max_use", &wire.Value{Kind: wire.ValueKindUnlimited}, true},
		{"prompt_cache_max_use", &wire.Value{Kind: wire.ValueKindUnlimited}, false},
		{"cache_max_use", &wire.Value{Kind: wire.ValueKindBytes}, true},
		{"cache_max_retention", &wire.Value{Kind: wire.ValueKindDuration}, true},
		{"tmp_max_retention", &wire.Value{Kind: wire.ValueKindDuration}, false},
		{"logs_max_retention", &wire.Value{Kind: wire.ValueKindDuration, DurationMs: 9223372036855}, false},
		{"messages_start_questions", &wire.Value{Kind: wire.ValueKindBoolean}, true},
		{"messages_start_questions", &wire.Value{Kind: wire.ValueKindBoolean, Text: "non-tag payload"}, false},
		{"messages_start_per_hour", &wire.Value{Kind: wire.ValueKindChoice, Choice: "3"}, false},
		{"messages_start_per_hour", &wire.Value{Kind: wire.ValueKindChoice, Choice: "1"}, true},
		{"nas_store", &wire.Value{Kind: wire.ValueKindText}, true},
		{"disabled_download_delegates", &wire.Value{Kind: wire.ValueKindTextMap}, true},
		{"model_context", &wire.Value{Kind: wire.ValueKindTextMap, TextMap: map[string]string{"qwen3.6-35b-a3b": "131072"}}, true},
		{"prompt_cache_off_for", &wire.Value{Kind: wire.ValueKindTextList}, false},
		{"messages_start_questions", &wire.Value{Kind: wire.ValueKindInteger}, false},
		{"unknown_future_key", nil, false},
		{"messages_start_questions", nil, true},
	}
	for _, c := range cases {
		authority := ValidateSetting(c.key, c.value)
		_, client := wire.OverlayConfigProcess(nil, []wire.Change{{Key: c.key, Set: c.value}})
		if (authority == nil) != c.valid || (client == nil) != c.valid {
			t.Fatal("client/service admission drift", c.key, c.value, c.valid, authority, client)
		}
	}
}

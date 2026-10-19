package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

func TestTypedMixedDiagnosticSourcesIdentifyActualFile(t *testing.T) {
	dir := t.TempDir()
	user, machine := filepath.Join(dir, "user.json"), filepath.Join(dir, "machine.json")
	for _, path := range []string{user, machine} {
		if err := os.WriteFile(path, []byte(`{"future":1}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources := TypedSources{UserStore: casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}, UserPath: user, MachinePath: machine}
	snapshot, err := sources.readSnapshotWithTrust(map[string]bool{"nas_store": true}, func(string) error { return nil })
	if err != nil || len(snapshot.IgnoredFields) != 2 {
		t.Fatal(snapshot, err)
	}
	if snapshot.IgnoredFields[0].Source.Location != wire.LocationUser || snapshot.IgnoredFields[0].Source.Path != user || snapshot.IgnoredFields[1].Source.Location != wire.LocationMachine || snapshot.IgnoredFields[1].Source.Path != machine {
		t.Fatal(snapshot.IgnoredFields)
	}
	if err := os.WriteFile(machine, []byte(" !"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = sources.readSnapshotWithTrust(map[string]bool{"nas_store": true}, func(string) error { return nil })
	var corrupt *RecordError
	if !errors.As(err, &corrupt) || corrupt.Offset != 1 || corrupt.Source == nil || corrupt.Source.Location != wire.LocationMachine || corrupt.Source.Path != machine {
		t.Fatal(err, corrupt)
	}
}

func TestTypedUnknownDiagnosticsAreBoundedAndMembersStayVerbatim(t *testing.T) {
	name := "prefix\n" + strings.Repeat("界", 30)
	encodedName, _ := json.Marshal(name)
	kept := `"\u0066uture_kept" :  { "array" : [ 1,  2 ] }`
	var original bytes.Buffer
	original.WriteString("{\n  ")
	original.Write(encodedName)
	original.WriteString(": true,\n  ")
	original.WriteString(kept)
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&original, ",\n  \"future_%d\": %d", i, i)
	}
	original.WriteString(",\n  \"messages_start_questions\": true,\n  \"store\": \"legacy\"\n}\n")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, original.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	store := casapi.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	sources := TypedSources{UserStore: store, UserPath: path}
	snapshot, err := sources.ReadSnapshot(map[string]bool{"messages_start_questions": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || len(snapshot.IgnoredFields) != 16 || snapshot.IgnoredOmitted != 66 {
		t.Fatal("unbounded or incomplete diagnostics", snapshot)
	}
	first := snapshot.IgnoredFields[0]
	if first.Source.Location != wire.LocationUser || first.Source.Path != path {
		t.Fatal("wrong source", first.Source)
	}
	if first.Offset != int64(bytes.Index(original.Bytes(), encodedName)) || first.Reason != wire.IgnoredFieldReason("invalid_name") || !strings.HasSuffix(first.NameExcerpt, "…") {
		t.Fatal(first)
	}
	for _, field := range snapshot.IgnoredFields {
		if len(field.NameExcerpt) > 64 || !utf8.ValidString(field.NameExcerpt) {
			t.Fatal("bad excerpt bound", field)
		}
		for _, r := range field.NameExcerpt {
			if unicode.IsControl(r) {
				t.Fatal("control in display excerpt", field)
			}
		}
	}
	before, err := ReadTypedUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceTypedUserStore(store, path, before.Revision, []wire.Change{{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean}}}); err != nil || !applied {
		t.Fatal(applied, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Contains(after, []byte(kept)) {
		t.Fatal("typed edit reserialized unknown member", string(after))
	}
	legacy, err := ReadUserStore(store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ReplaceUserStore(store, path, legacy.Revision, Config{Store: "replacement"}); err != nil || !applied {
		t.Fatal(applied, err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Contains(after, []byte(kept)) {
		t.Fatal("legacy edit reserialized unknown member", string(after))
	}
}

func TestTypedFutureBytesParticipateInBothUserRevisions(t *testing.T) {
	first := []byte(`{"future":{"a":1}}`)
	moved := []byte(`{"future":{"a": 1}}`)
	layout := []byte("{\n \"future\" : {\"a\":1}\n}")
	a, _ := typedUserSnapshot(first, "test")
	b, _ := typedUserSnapshot(moved, "test")
	c, _ := typedUserSnapshot(layout, "test")
	if a.Revision == b.Revision || a.Revision != c.Revision {
		t.Fatal("unknown byte changes lost, or outer layout hashed", a.Revision, b.Revision, c.Revision)
	}
	x, _ := userSnapshot(first)
	y, _ := userSnapshot(moved)
	z, _ := userSnapshot(layout)
	if x.Revision == y.Revision || x.Revision != z.Revision {
		t.Fatal("legacy revision lost opaque future bytes", x.Revision, y.Revision, z.Revision)
	}
}

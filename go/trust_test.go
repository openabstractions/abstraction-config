package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAMachineFileNobodyPrivilegedWroteIsIgnored(t *testing.T) {
	dir := t.TempDir()
	planted := filepath.Join(dir, Name, "config.json")
	if err := Save(planted, Config{Store: "planted"}); err != nil {
		t.Fatal(err)
	}
	err := trusted(planted)
	if err == nil {
		t.Skip("this process's files pass the ownership test, so it is privileged and cannot plant anything")
	}
	t.Log(err)
	if runtime.GOOS != "windows" {
		return
	}
	t.Setenv("ProgramData", dir)
	t.Setenv(EnvVars["store"], "")
	if got := Load().Store; got == "planted" {
		t.Fatalf("Load took %s, a machine-wide file this unprivileged process wrote", planted)
	}
	if _, err := os.Stat(planted); err != nil {
		t.Fatal("the file was removed rather than ignored:", err)
	}
}

// [CFG-T4], and the corpus cannot reach it: the corpus is one transcript both
// languages print and only the Go half of this layer writes.
func TestAMachineFileThisProcessCannotMakeTrustedIsNotWritten(t *testing.T) {
	if runtime.GOOS != "windows" {
		// /etc cannot be redirected, so the choice here is between touching the
		// real machine rung and proving nothing. Said out loud, because a skip
		// that prints ok is the same defect as passing by abstaining.
		fmt.Fprintf(os.Stderr, "UNREACHABLE  config CFG-T4 on %s — the machine rung is /etc, "+
			"which no test can redirect, so the rule is UNPROVEN here\n", runtime.GOOS)
		t.Skip("unreachable, and said so on stderr")
	}
	dir := t.TempDir()
	t.Setenv("ProgramData", dir)

	probe := filepath.Join(dir, Name+"-probe", "config.json")
	if err := Save(probe, Config{Store: "probe"}); err != nil {
		t.Fatal(err)
	}
	if trusted(probe) == nil {
		t.Skip("this process's files pass the ownership test, so it is privileged and this write should succeed")
	}

	path := MachinePath()
	for _, write := range []struct {
		how string
		do  func() error
	}{
		{"Save", func() error { return Save(path, Config{Store: "M"}) }},
		{"Edit", func() error { return Edit(path, func(c *Config) error { c.Store = "M"; return nil }) }},
	} {
		err := write.do()
		if err == nil {
			t.Fatalf("%s wrote %s, which no reader will trust", write.how, path)
		}
		t.Log(write.how, err)
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Fatalf("%s left %s behind, and whoever owns that directory owns the machine rung: %v",
				write.how, filepath.Dir(path), err)
		}
	}

	user := filepath.Join(dir, "user", "config.json")
	if err := Save(user, Config{Store: "U"}); err != nil {
		t.Fatal("a rule about the machine rung caught the user rung:", err)
	}
}

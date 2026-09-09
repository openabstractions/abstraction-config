package config

import (
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

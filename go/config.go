// Package config is how an application finds the tiers this machine has,
// without being told about any of them.
//
// # The problem this exists for
//
// Every abstraction in this repository delegates to whatever is installed: a
// download goes to a NAS if one is set up, else to the OS transfer service, else
// in-process. That is only useful if an application which knows nothing about
// any of it — a fork of Lemonade, say — picks the right tier by itself.
//
// The first version discovered tiers from environment variables, and that was a
// bad answer. Lemonade will not have ABSTRACTION_NAS_STORE set. Nobody launching
// a GUI from a Start Menu shortcut has anything set. An abstraction that only
// works when every application is separately configured has moved the problem
// rather than solved it.
//
// # What SLF4J actually did
//
// SLF4J did not ask applications to configure a binding. It asked them to depend
// on the facade, and then bound to whatever implementation was PRESENT on the
// classpath at startup. Presence was the configuration. That is the property
// worth copying, and the machine-level equivalent of a classpath is a
// well-known location that setup writes once and every process reads.
//
//	installing the NAS tier      writes it here, once
//	Lemonade, ComfyUI, modelget  read it, knowing nothing
//
// So there is exactly one configuration step per machine, performed by whoever
// sets a tier up, and zero per application. An application calls Load() and gets
// the truth about the machine it is running on.
//
// # Order of precedence, and why
//
//  1. environment      an override, for tests and one-off runs
//  2. per-user file    what this user set up
//  3. machine file     what an administrator set up for everyone
//
// The environment wins because overriding one run must not require editing a
// file that other programs are reading. The user file beats the machine file
// because a user must be able to opt out of something an administrator turned
// on without needing an administrator.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	cas "github.com/openabstractions/abstraction-cas/go"
)

// Config is what a machine has. Every field is optional: absent means "this
// machine does not have that tier", which is a normal answer and not an error.
type Config struct {
	// NASStore is a job store on a share that a supervisor elsewhere watches.
	NASStore string `json:"nas_store,omitempty"`

	// Store is the local job store. Empty means the default.
	Store string `json:"store,omitempty"`

	// LogSink is a file every tool appends structured records to.
	LogSink string `json:"log_sink,omitempty"`

	// LogService is a local socket that attests identity. See logging/identity.go.
	LogService string `json:"log_service,omitempty"`

	// Off names tiers this machine will not hand work to, each with the reason a
	// person gave. A tier absent from here is used if the machine has it.
	//
	// Negative on purpose, and the same shape as the download layer's host
	// refusals. Forcing a tier that is not configured or not reachable cannot
	// work, so there is nothing to force; switching one off always can. Read at
	// the moment work is handed on rather than at startup, so a person throwing
	// the switch is obeyed by the next job and nothing restarts.
	Off map[string]string `json:"off,omitempty"`

	// From records which file this came from, for diagnostics. A user who cannot
	// work out why their downloads are going somewhere unexpected needs to be
	// able to find the file that says so.
	From string `json:"-"`
}

// Overridden names the fields the environment is deciding, whatever any file
// says. An override that nothing can see is how a person ends up editing a file
// and watching nothing change, which is the complaint this package was written
// for one level down.
func Overridden() []string {
	var out []string
	for _, field := range []string{"nas_store", "store", "log_sink", "log_service"} {
		if os.Getenv(EnvVars[field]) != "" {
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out
}

// Stamp differs whenever the answer differs. A process that has been running for
// days asks for it rather than rebuilding everything to find out whether it
// needs to.
func (c Config) Stamp() string {
	c.From = ""
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(b)
}

// Name is the directory and file this looks for.
const Name = "abstraction"

// EnvVars holds the environment variable for each field, and these are
// overrides rather than the mechanism. Keeping them means a test, a container
// or a one-off run can redirect a tier without writing to a file that other
// processes on the machine are reading.
var EnvVars = map[string]string{
	"nas_store":   "ABSTRACTION_NAS_STORE",
	"store":       "ABSTRACTION_STORE",
	"log_sink":    "ABSTRACTION_LOG",
	"log_service": "ABSTRACTION_LOG_SERVICE",
}

// Load returns the machine's configuration. It never fails: a machine with
// nothing set up is a machine with no extra tiers, which every caller already
// has to handle.
func Load() Config {
	var c Config
	for _, path := range searchPaths() {
		if loaded, err := read(path); err == nil {
			c = merge(c, loaded)
			if c.From == "" {
				c.From = path
			}
		}
	}
	return applyEnv(c)
}

// UserPath is where a per-user configuration belongs on this OS, following the
// platform's own convention rather than inventing one. A tool that scatters
// dotfiles where the OS did not ask for them is the thing this project keeps
// complaining about.
func UserPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, Name, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "."+Name, "config.json")
}

// MachinePath is where an administrator's configuration belongs.
func MachinePath() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, Name, "config.json")
		}
		return ""
	}
	return filepath.Join("/etc", Name, "config.json")
}

// searchPaths returns files in increasing order of precedence, so later entries
// win.
func searchPaths() []string {
	var out []string
	if p := MachinePath(); p != "" {
		out = append(out, p)
	}
	if p := UserPath(); p != "" {
		out = append(out, p)
	}
	return out
}

func read(path string) (Config, error) {
	b, err := cas.Read(path)
	if err != nil {
		return Config{}, err
	}
	if b == nil {
		return Config{}, os.ErrNotExist
	}
	if path == MachinePath() {
		if err := trusted(path); err != nil {
			fmt.Fprintf(os.Stderr, "abstraction: ignoring %s: %v\n", path, err)
			return Config{}, err
		}
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		// A malformed config must not take the application down, and must not be
		// silent either — the whole point is that somebody can find out why a
		// tier is not being used.
		fmt.Fprintf(os.Stderr, "abstraction: ignoring %s: %v\n", path, err)
		return Config{}, err
	}
	c.From = path
	return c, nil
}

func merge(base, over Config) Config {
	if over.NASStore != "" {
		base.NASStore = over.NASStore
	}
	if over.Store != "" {
		base.Store = over.Store
	}
	if over.LogSink != "" {
		base.LogSink = over.LogSink
	}
	if over.LogService != "" {
		base.LogService = over.LogService
	}
	if len(over.Off) > 0 {
		base.Off = over.Off
	}
	if over.From != "" {
		base.From = over.From
	}
	return base
}

func applyEnv(c Config) Config {
	if v := os.Getenv(EnvVars["nas_store"]); v != "" {
		c.NASStore, c.From = v, "environment"
	}
	if v := os.Getenv(EnvVars["store"]); v != "" {
		c.Store, c.From = v, "environment"
	}
	if v := os.Getenv(EnvVars["log_sink"]); v != "" {
		c.LogSink, c.From = v, "environment"
	}
	if v := os.Getenv(EnvVars["log_service"]); v != "" {
		c.LogService, c.From = v, "environment"
	}
	return c
}

// Save writes a configuration to path, creating the directory. This is what a
// setup step calls — once per machine — so that every application afterwards
// needs to know nothing.
func Save(path string, c Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("abstraction: no path to save to")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := cas.Change(path, func([]byte) ([]byte, error) { return append(b, '\n'), nil }); err != nil {
		return err
	}
	announce()
	return nil
}

// Edit changes one configuration file in place, under the same lock and atomic
// rename Save uses.
//
// Save takes a whole Config and would therefore delete every answer the caller
// did not happen to be holding. A control panel changes one answer at a time and
// the rest of the file was written by somebody else — a setup step, an
// administrator, an earlier version of this program — so it has to survive.
func Edit(path string, change func(*Config) error) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("abstraction: no path to edit")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	err := cas.Change(path, func(cur []byte) ([]byte, error) {
		var c Config
		if len(cur) > 0 {
			if err := json.Unmarshal(cur, &c); err != nil {
				return nil, fmt.Errorf("abstraction: %s is not readable, so it will not be overwritten: %w", path, err)
			}
		}
		if err := change(&c); err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	})
	if err != nil {
		return err
	}
	announce()
	return nil
}

// Describe explains what was found and where, for a `status` command. The
// failure this prevents is a user unable to discover why their downloads are
// going somewhere they did not expect.
func (c Config) Describe() string {
	var b strings.Builder
	if c.From == "" {
		return "no configuration found; only built-in tiers are available"
	}
	fmt.Fprintf(&b, "from %s\n", c.From)
	for _, kv := range [][2]string{
		{"nas store", c.NASStore},
		{"local store", c.Store},
		{"log sink", c.LogSink},
		{"log service", c.LogService},
	} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "  %-12s %s\n", kv[0], kv[1])
		}
	}
	for _, name := range sorted(c.Off) {
		fmt.Fprintf(&b, "  %-12s off — %s\n", name, c.Off[name])
	}
	if over := Overridden(); len(over) > 0 {
		fmt.Fprintf(&b, "\nthe environment is deciding %s, whatever this file says\n",
			strings.Join(over, ", "))
	}
	return b.String()
}

func sorted(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// JobStore is where jobs live on this machine.
//
// Configuration first, then the default. An existing ~/.modelget keeps being
// the store, because moving the default on upgrade would strand whatever is in
// flight — a store is a directory of real work, not a cache.
//
// # Why it is here and not in download
//
// It used to be download.StoreRoot, exported, and that was two mistakes at
// once. Jobs are not downloads: a store holds work of every kind, and the
// download layer had no business being the place other programs asked where it
// lives. And the name said `Root` — a filesystem word in the public API of a
// layer whose whole claim is that it does not know what a file is.
//
// It cannot live in the job package either, and that is deliberate rather than
// awkward: job has no dependencies at all, which is what lets three languages
// implement the semantics without inheriting anybody's configuration format.
// Resolving "what has this machine been told" is exactly this package's job,
// and it already answers the same shape of question in UserPath and
// MachinePath.
func JobStore() (string, error) {
	if v := Load().Store; v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if legacy := filepath.Join(home, ".modelget"); dirExists(legacy) {
		return legacy, nil
	}
	return filepath.Join(home, "."+Name), nil
}

func dirExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

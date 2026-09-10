// configdriver applies a config scenario and prints what an observer saw.
// conformance/DRIVER.md is the contract; config/testdata/scenarios holds the
// corpus, and the transcript this prints is compared with the Python driver's
// byte for byte.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	config "github.com/openabstractions/abstraction-config/go"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--capabilities" {
		fmt.Print(strings.Join(capabilities(), " ") + "\n")
		return
	}
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: configdriver <workdir> <scenario>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// The machine rung is reachable only where this process can choose the machine
// directory. Windows reads it from ProgramData, which a driver can point into
// its own workdir; /etc cannot be moved and cannot be written by an ordinary
// user, so on those platforms the scenarios needing it are out of reach and say
// so rather than passing.
func capabilities() []string {
	if runtime.GOOS == "windows" {
		return []string{"config", "machine"}
	}
	return []string{"config"}
}

type driver struct {
	out   *bufio.Writer
	noise *bytes.Buffer
	last  string
	first bool
}

func run(workdir, scenario string) error {
	b, err := os.ReadFile(scenario)
	if err != nil {
		return err
	}
	if err := settle(workdir); err != nil {
		return err
	}
	d := &driver{out: bufio.NewWriter(os.Stdout), noise: &bytes.Buffer{}, first: true}
	defer d.out.Flush()
	n := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
		fmt.Fprintf(d.out, "%02d %s -> %s\n", n, line, d.apply(line))
	}
	return nil
}

// settle puts every directory this layer looks in inside the workdir, and takes
// away every variable a machine running the corpus might already have set, so a
// scenario decides the whole answer and the machine decides none of it.
func settle(workdir string) error {
	home := filepath.Join(workdir, "home")
	dirs := map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"AppData":         filepath.Join(workdir, "appdata"),
		"ProgramData":     filepath.Join(workdir, "programdata"),
		"XDG_CONFIG_HOME": filepath.Join(workdir, "xdg"),
	}
	for k, v := range dirs {
		if err := os.MkdirAll(v, 0o755); err != nil {
			return err
		}
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	os.Unsetenv("HOMEDRIVE")
	os.Unsetenv("HOMEPATH")
	for _, key := range config.Keys {
		if v := config.EnvVars[key]; v != "" {
			os.Unsetenv(v)
		}
	}
	return isolated(workdir)
}

// A machine rung this driver cannot point into its own workdir is a rung the
// machine decides, and a corpus that read one would print whatever the machine
// running it happens to have. Where nothing is there the rung contributes
// nothing and the run is honest; where something is, the run stops and says so
// rather than recording a transcript nobody else can reproduce.
func isolated(workdir string) error {
	m := config.MachinePath()
	if m == "" || strings.HasPrefix(m, workdir) {
		return nil
	}
	if _, err := os.Stat(m); err == nil {
		return fmt.Errorf("%s is outside this workdir and exists, so the machine "+
			"rung cannot be isolated and the corpus would read this machine's answer", m)
	}
	return nil
}

func (d *driver) apply(line string) string {
	op, rest, _ := strings.Cut(line, " ")
	switch op {
	case "user":
		return d.plant(config.UserPath(), rest)
	case "machine":
		return d.plant(config.MachinePath(), rest)
	case "env":
		return d.env(rest)
	case "load":
		return d.load()
	case "key":
		return d.key(rest)
	case "stamp":
		return d.stamp()
	case "noise":
		return d.said()
	}
	return "unknown-op"
}

func (d *driver) plant(path, body string) string {
	if path == "" {
		return "refused no-such-rung"
	}
	if body == "-" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "refused unwritable"
		}
		return "ok"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "refused unwritable"
	}
	if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
		return "refused unwritable"
	}
	return "ok"
}

func (d *driver) env(rest string) string {
	key, value, ok := strings.Cut(rest, " ")
	name := config.EnvVars[key]
	if !ok || name == "" {
		return "invalid"
	}
	if value == "-" {
		os.Unsetenv(name)
		return "ok"
	}
	if value == "empty" {
		value = ""
	}
	os.Setenv(name, value)
	return "ok"
}

func (d *driver) load() string {
	c := d.hearing(config.Load)
	var out []string
	for _, key := range config.Keys {
		o := c.Origin(key)
		if o.Rung == config.Default {
			continue
		}
		out = append(out, key+"="+value(c, key)+"/"+o.Rung)
	}
	if len(out) == 0 {
		return "ok -"
	}
	return "ok " + strings.Join(out, " ")
}

func (d *driver) key(name string) string {
	if name == "" {
		return "invalid"
	}
	c := d.hearing(config.Load)
	return "ok value=" + value(c, name) + " from=" + c.Origin(name).Rung
}

func (d *driver) stamp() string {
	c := d.hearing(config.Load)
	s := c.Stamp()
	was, first := d.last, d.first
	d.last, d.first = s, false
	switch {
	case first:
		return "ok first"
	case s == was:
		return "ok same"
	}
	return "ok differs"
}

// said reports whether anything reached stderr since it was last asked. A file
// ignored silently and a file ignored loudly are the same transcript without
// this, and the difference is the whole of the rule.
func (d *driver) said() string {
	if d.noise.Len() == 0 {
		return "ok silent"
	}
	d.noise.Reset()
	return "ok said"
}

// hearing runs one call with stderr redirected into a buffer, so said() can
// report it without the text itself ever reaching a transcript two languages
// have to agree on.
func (d *driver) hearing(f func() config.Config) config.Config {
	r, w, err := os.Pipe()
	if err != nil {
		return f()
	}
	was := os.Stderr
	os.Stderr = w
	c := f()
	os.Stderr = was
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	r.Close()
	d.noise.Write(buf.Bytes())
	return c
}

func value(c config.Config, key string) string {
	switch key {
	case "nas_store":
		return c.NASStore
	case "store":
		return c.Store
	case "log_sink":
		return c.LogSink
	case "log_service":
		return c.LogService
	case "off":
		names := make([]string, 0, len(c.Off))
		for k := range c.Off {
			names = append(names, k)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	return ""
}

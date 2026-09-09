# abstraction-config

A machine answers, from one file written once, which optional services it has,
so no application is configured on its own.

## The problem

The other layers delegate work to whatever is installed: a download goes to a
NAS if one is set up, otherwise to the operating system's transfer service,
otherwise to the calling process. That only helps if a program which knows
nothing about any of it can discover what is available. Reading environment
variables does not achieve that — a GUI launched from a Start Menu shortcut
inherits none, and asking every application to be separately configured moves
the problem instead of solving it. This layer puts the answer in a file that a
setup step writes once per machine and every process afterwards reads. `Load()`
never fails; a machine with nothing set up reports no optional services, which
is a normal answer.

## Words

| word | meaning |
|---|---|
| **machine file** | `%ProgramData%\abstraction\config.json` on Windows, `/etc/abstraction/config.json` elsewhere |
| **user file** | `os.UserConfigDir()/abstraction/config.json`; overrides the machine file field by field |
| **environment** | overrides both, field by field, for a test or a one-off run |
| **`From`** | where each value came from; not serialised |

No rule on this page carries a tag, and no conformance scenario cites this
layer.

## Obtain

- **Go.** `go get github.com/openabstractions/abstraction-config/go`. The
  module path ends in `/go`; the package is `config`, so import it with an
  explicit alias. The newest tag is `go/v0.1.0`; `@main` is the tree as it
  stands.
- **Python, C++.** None.

## Example

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	config "github.com/openabstractions/abstraction-config/go"
)

func main() {
	dir, err := os.MkdirTemp("", "config-example")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	logFile := filepath.Join(dir, "abstraction.jsonl")

	// A setup step writes a file like this once per machine, to
	// config.UserPath() or config.MachinePath(). This example writes to a
	// temporary path instead, so that running it changes nothing.
	if err := config.Save(filepath.Join(dir, "config.json"), config.Config{LogSink: logFile}); err != nil {
		panic(err)
	}

	// The environment overrides any file, which is how a test or a one-off run
	// redirects a service without editing what other processes are reading.
	os.Setenv(config.Env["log_sink"], logFile)

	// Every application afterwards asks the machine, knowing nothing.
	c := config.Load()
	fmt.Println("log sink:", c.LogSink)
	fmt.Println("from:", c.From)

	// Describe() renders everything this machine has, so its output depends on
	// what is set up here.
	fmt.Print(c.Describe())
}
```

The first two lines of output are the same everywhere:

```
log sink: <temp dir>/abstraction.jsonl
from: environment
```

## API overview

`Config` has four optional fields, plus `From`, which records where the values
came from and is not serialised. An empty field means the machine does not have
that service.

- `NASStore` — a directory of pending work on a network share that some other
  machine is watching.
- `Store` — the local directory of pending work.
- `LogSink` — a file every tool appends structured log records to.
- `LogService` — the address of a local socket that receives log records.

`Load() Config` reads the machine file, then the per-user file, then the
environment, each overriding the last field by field. It returns no error: a
file that cannot be read or parsed is skipped, with one line on stderr saying
which. `Config.Describe() string` renders what was found and where, for a
`status` command.

`Save(path string, c Config) error` writes the JSON through a temporary file and
a rename, so a concurrent reader never sees a partial file. It creates the
parent directory.

`UserPath() string` and `MachinePath() string` give the conventional locations
for this platform. `MachinePath` returns an empty string on Windows when
`ProgramData` is unset.

`Env` maps each JSON field name to the environment variable that overrides it:
`ABSTRACTION_NAS_STORE`, `ABSTRACTION_STORE`, `ABSTRACTION_LOG`,
`ABSTRACTION_LOG_SERVICE`. `Name` is the constant `"abstraction"`, used as the
directory and file stem.

`JobStore() (string, error)` answers where jobs live: the configured `Store`, or
an existing `~/.modelget` directory if one is present, or `~/.abstraction`.

## Today

Experimental, version 0.1.0. **Go only**, one file, consumed indirectly by
`abstraction-model`.

- No tests. The precedence rules described above are asserted by reading the
  code, not by anything that runs.
- No implementation in any other language, so a Python or C++ process on the
  same machine cannot read the same file through this layer.
- The field set is fixed and small. Adding a service means adding a field here,
  which is a poor fit for anything out of tree.
- Nothing validates the values. A `Store` pointing at a path that does not exist
  is returned unchanged.
- `Load()` re-reads both files on every call; there is no caching.

## Conformance

None. There is one implementation and no test of it.

## Where it sits

Below: [abstraction-cas](https://github.com/openabstractions/abstraction-cas)
writes the file. Above:
[abstraction-download](https://github.com/openabstractions/abstraction-download)
reads which store and which tiers a machine has, and
[abstraction-facade](https://github.com/openabstractions/abstraction-facade)
reads it once at `Discover()`.

One layer of [openabstractions](https://github.com/openabstractions/abstractions).
Every layer names one thing local tools rebuild on their own; the name means the
same in each language that implements it, and the conformance scenarios are what
hold an implementation to it.

## Requirements

Go 1.26 or newer. No dependencies outside the standard library. Tested on
Windows and Linux.

## Licence

Apache-2.0. See [LICENSE](LICENSE).

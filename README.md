# abstraction-config

**Development API.** The service client reads configuration without opening
configuration files in the application. Tags exist and no version number is typed on this
page: [the tag list](https://github.com/openabstractions/abstraction-config/tags)
is the answer to "which release", because a tag is the only thing that cannot
drift.

A configuration service answers which optional services are configured, with
the origin of each value. The service owns access to machine and user files;
an application may supply its own explicit overrides for that call.

## Application clients

Use the resolved ConfigReader, ConfigEditor and ConfigObserver contracts through
[the facade](https://github.com/openabstractions/abstraction-facade). The service
reads existing configuration records and owns revision-checked user replacement.
Applications supply run overrides explicitly and receive values with provenance.
A provenance path is diagnostic; it does not authorize opening provider files.

`ConfigEditor.ReplaceUser` reports `applied`, `conflict`, `forbidden` or
`unavailable`. `conflict` returns the current user values; reread and decide
again. A host with an edit policy returns `forbidden` for an evaluated refusal.
It returns `unavailable` when no decision could be obtained, and the caller may
retry. Neither writes storage. The installed runtime asks the rights service for
`abstraction.config/user.replace` on `abstraction.config/editor@1`. The
[editor contract](CONTRACT.md) gives the full table.

```go
editor, err := facade.Discover().ResolveConfigEditor(ctx, facade.Requirements{})
if err != nil {
	return err
}
current, err := editor.ReadUserContext(ctx)
if err != nil {
	return err
}
values := current.Values
values.LogSink = ""
result, err := editor.ReplaceUserContext(ctx, current.Revision, values)
switch {
case err != nil:
	return err
case result.Outcome == "conflict":
	// result.Snapshot holds the values another writer stored.
case result.Outcome == "unavailable":
	// No policy decision; nothing was written.
}
```

See [Python setup](py/README.md) and [C++ setup](cpp/README.md). Missing or refused
services stay explicit. Default installed Go/C++/Python bindings retain independent
server trust. A custom host requires independently configured expectations.

## Retained provider documentation

The native file-loading, saving and watching APIs below describe explicitly
selected provider compatibility. They are not the normal resolved application
entrypoint. Language and platform statements below apply to those native APIs;
service client availability is described in the linked package pages.

## Service ownership and provider implementation

Applications obtain settings and provenance through the service API. The primary
facade resolves that service; it reads no shared configuration file during
`Discover()`. The runtime's resolver supplies capability availability separately.
Configuration values are settings, and grant no authority to perform an operation.

The `LegacyLoad()` API and file formats below describe the service's provider and
explicit legacy integrations. Their permissive defaults are provider behavior.
The service client reports an unavailable service as an error.

The unprefixed Go file-provider entry points were removed:

| removed | applications use | deliberate provider adopters call |
| --- | --- | --- |
| Go `Load` | `facade.Discover().ResolveConfig` | `LegacyLoad` |
| Go `JobStore` | the resolved job service and its receipts | `LegacyJobStore` |
| Go `Watch`, `WatchQuiet` | a resolved config reader's snapshot observation | `LegacyWatchQuiet` |

The unprefixed Python names are deprecated and keep their behavior:

| deprecated | applications use | deliberate provider adopters call |
| --- | --- | --- |
| Python `load`, `watch` | `Machine.resolve_config()` | `legacy_load`, `legacy_watch` |
| Python `job_store` | the resolved job service and its receipts | `legacy_job_store` |

Python raises `LegacyConfigDeprecationWarning`, a `DeprecationWarning` subclass
carrying `api`, `replacement` and `adoption`.

## Words

| word | meaning |
|---|---|
| **machine file** | `%ProgramData%\abstraction\config.json` on Windows, `/etc/abstraction/config.json` elsewhere |
| **user file** | `os.UserConfigDir()/abstraction/config.json`; overrides the machine file field by field |
| **environment** | overrides both, field by field, for a test or a one-off run |
| **`From`** | where each value came from; not serialised |

The [contract](CONTRACT.md) and checked-in scenarios define the existing
configuration behavior. Service checks report their narrower IPC scope.

## Obtain

- **Go.** `go get github.com/openabstractions/abstraction-config/go`. The
  module path ends in `/go`; the package is `config`, so import it with an
  explicit alias.
  [Releases, newest first](https://github.com/openabstractions/abstraction-config/tags);
  pin the exact tag you tested against, or `@main` for the tree as it stands.
- **Python.** Not on any index —
  [what to install, import and call](python/README.md). It reads; it does not
  write, so there is no `Save` there.
- **C++.** [Generated interface and service client](cpp/README.md), using the
  shared IPC runtime and a Go service.

Whether to adopt this at all, what it costs and what is not proven:
[Adopting](CONTRIBUTING.md#adopting).

## Explicit native-provider example

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
	c := config.LegacyLoad()
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

`LegacyLoad() Config` reads the machine file, then the per-user file, then the
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

`LegacyJobStore() (string, error)` answers where jobs live: the configured `Store`, or
an existing `~/.modelget` directory if one is present, or `~/.abstraction`.

## Today

Experimental. **Go and Python**, one file each, consumed indirectly by
`abstraction-model` and directly by `abstraction-download`.

- **The Python side reads and does not write.** There is no `Save` there, so a
  machine is configured by the Go side or by a setup step.
- **No C++ implementation**, so a C++ process on the same machine cannot read
  the same file through this layer.
- The field set is fixed and small. Adding a service means adding a field here,
  which is a poor fit for anything out of tree.
- Nothing validates the values. A `Store` pointing at a path that does not exist
  is returned unchanged.
- `LegacyLoad()` re-reads both files on every call; there is no caching.

## Conformance

**None.** No scenario in the suite cites this layer, so it carries no verdict
attributed to the conformance tree —
[what is proven and what is not](https://openabstractions.org/coverage.html).
Each implementation has its own tests; that is a weaker claim, and the two must
not be read as one.

## Where it sits

Below: [abstraction-cas](https://github.com/openabstractions/abstraction-cas)
writes the file. Above:
[abstraction-download](https://github.com/openabstractions/abstraction-download)
reads which store and which tiers a machine has, and
[abstraction-facade](https://github.com/openabstractions/abstraction-facade)
resolves the service through `ResolveConfig`; shared files stay with the provider.

One layer of [openabstractions](https://github.com/openabstractions/abstractions).
Every layer names one thing local tools rebuild on their own; the name means the
same in each language that implements it, and the conformance scenarios are what
hold an implementation to it.

## Requirements

Go 1.26 or newer. No dependencies outside the standard library. Tested on
Windows and Linux.

## Licence

Apache-2.0. See [LICENSE](LICENSE).

# abstraction-config

Read the settings that tell this computer where OA services keep jobs, content
and logs, and show whether each value came from the machine, the user, the
environment or a default. The fixed schema contains `nas_store`, `store`,
`log_sink`, `log_service` and the closed `off` feature map.

Applications resolve the reader through the facade. Operator software resolves
the editor to replace the complete user rung conditionally. Observers long-poll
the latest effective snapshot and rebuild after a typed `gap`. Run overrides are
explicit caller input. The service alone reads machine and user files.

## Service contracts

Three generated services share the identity-bound `config` endpoint, declared
in [config.thrift](config.thrift) and normatively described in
[CONTRACT.md](CONTRACT.md):

| service | wire name | does |
| --- | --- | --- |
| `ConfigReader` | `abstraction.config/reader@1` | `Read(overrides)` returns a `Snapshot`: the five values, per-key provenance and a stamp |
| `ConfigEditor` | `abstraction.config/editor@1` | `ReadUser()` and `ReplaceUser(revision, values)` read and revision-check the user rung only |
| `ConfigObserver` | `abstraction.config/observer@1` | `Observe(overrides, cursor, wait_ms)` long-polls the latest snapshot |

`ReplaceUser` returns `applied`, `conflict`, `forbidden` or `unavailable`.
`conflict` writes nothing and returns the current snapshot; reread and decide
again. `forbidden` reports an evaluated edit-policy refusal; `unavailable`
reports that no decision could be obtained and the caller may retry. The
installed runtime asks the rights service for `abstraction.config/user.replace`
on `abstraction.config/editor@1` before a replacement reaches storage. Every
value is diagnostic settings data; a path in provenance names a file and grants
no authority to open it.

## Application clients

Use the resolved clients through [the facade](https://github.com/openabstractions/abstraction-facade).
Go, C++, Python and Rust (the facade's `rust-config` crate, `ConfigMachine`)
resolve all three services. JavaScript resolves through the facade's
`Machine.resolveService` and calls the generated `ConfigReaderClient`,
`ConfigEditorClient` and `ConfigObserverClient` on the binding.

```go
package main

import (
	"context"
	"fmt"

	facade "github.com/openabstractions/abstraction-facade/go"
)

func main() {
	ctx := context.Background()
	editor, err := facade.Discover().ResolveConfigEditor(ctx, facade.Requirements{})
	if err != nil {
		panic(err)
	}
	current, err := editor.ReadUserContext(ctx)
	if err != nil {
		panic(err)
	}
	values := current.Values
	values.LogSink = "configured-provider-setting"
	result, err := editor.ReplaceUserContext(ctx, current.Revision, values)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Outcome) // "applied", or "conflict" with result.Snapshot
}
```

```python
from abstraction.facade.client import Machine
from abstraction.config import RunOverrides

machine = Machine(timeout=2)
reader = machine.resolve_config(scope="local")
snapshot = reader.read(RunOverrides())
print(snapshot.store, snapshot.origins.store.rung)
```

```cpp
#include <abstraction/facade/client.hpp>
auto config = abstraction::facade::discover().resolve_config();
auto value = config.read();
```

The Go client lives in `go/client`. See [Python setup](py/README.md) and
[C++ setup](cpp/README.md). A missing or refused service is an error; no
resolved client falls back to a local file.

## Legacy local provider

`go/config.go` and `python/abstraction_config.py` retain the embedded
file-reading provider the services above now front. Applications do not call
it; it is what a service, a setup step, or a deliberate out-of-tree adopter
calls.

| removed name | applications use | a deliberate provider adopter calls |
| --- | --- | --- |
| Go `Load`, `LegacyLoad` | `facade.Discover().ResolveConfig` | `config.LoadWithOverrides(values)` |
| Go `JobStore`, `LegacyJobStore` | the resolved job service and its receipts | nothing: the legacy job store has no locator since 0.1.8 |
| Go `Watch`, `WatchQuiet`, `LegacyWatchQuiet` | a resolved config reader's snapshot observation | `config.WatchInvalidations`, then reread |
| Python `load`, `watch`, `job_store` | `Machine.resolve_config()` | `legacy_load`, `legacy_watch`, `legacy_job_store` |

Both are read paths: `LoadWithOverrides`/`legacy_load` read the machine file,
then the user file, then the supplied overrides (Python: the environment), each
overriding the last field by field, and
return no error — a file that cannot be read or parsed is skipped, with one
line on stderr saying which ([CFG-T2](CONTRACT.md)). Only the Go side writes:
`config.Save(path, c)` and `config.Edit(path, change)` write through a
temporary file and an atomic rename. There is no `Save` in Python; a machine is
configured by the Go side, by an installer, or by hand.

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
	if err := config.Save(filepath.Join(dir, "config.json"), config.Config{LogSink: logFile}); err != nil {
		panic(err)
	}
	c := config.LoadWithOverrides(map[string]string{config.EnvVars["log_sink"]: logFile})
	fmt.Println("log sink:", c.LogSink)
	fmt.Println("from:", c.Origin("log_sink"))
	fmt.Print(c.Describe())
}
```

The first two lines of output are the same everywhere:

```
log sink: <temp dir>/abstraction.jsonl
from: environment
```

`Config` has four optional string fields (`NASStore`, `Store`, `LogSink`,
`LogService`) plus `Off`, a map of tiers this machine will not hand work to
with the reason a person gave. `Origin(key) Origin` and `Config.Describe()
string` answer which authority set each key and render it for a `status`
command. `UserPath()` and `MachinePath()` give this platform's conventional
locations; `MachinePath` returns an empty string on Windows when
`ProgramData` is unset. `EnvVars` maps each JSON field to the environment
variable that overrides it: `ABSTRACTION_NAS_STORE`, `ABSTRACTION_STORE`,
`ABSTRACTION_LOG`, `ABSTRACTION_LOG_SERVICE`.

`LoadWithOverrides` never reads this process's environment: a service passes
the run overrides its caller supplied. The Python package's `legacy_user_path`
is `user_path`; the
Python object has no writer and raises `Untrusted` for a machine-wide file no
administrator owns ([python/README.md](python/README.md)).

## Obtain

- **Go.** `go get github.com/openabstractions/abstraction-config/go`. The
  module path ends in `/go`; the package is `config`. Import it with an
  explicit alias. [Releases, newest first](https://github.com/openabstractions/abstraction-config/tags);
  pin the exact tag you tested against, or `@main` for the tree as it stands.
- **Python.** Not on any package index. [What to install, import and call for
  the resolved service client](py/README.md); [the legacy local
  provider](python/README.md).
- **C++.** [Generated interface and service client](cpp/README.md), using the
  shared IPC runtime and the Go service.
- **Rust, JavaScript.** Generated wire types, codecs and service clients
  (`rust/Cargo.toml` over `rs/abstraction/config`, `javascript/package.json`
  over `javascript/js/abstraction/config`). Resolution comes from the facade:
  the `rust-config` crate for Rust, `Machine.resolveService` for JavaScript.

## Run the service

```sh
openabstractions serve config --endpoint <endpoint>
```

One process per user, serving only the account it runs as: it checks the
kernel-bound caller's account against its own before reading the provider.
`ABSTRACTION_CONFIG_ENDPOINT` overrides the endpoint for clients and host; the
default is the shared `config-v1` endpoint convention. The default `openabstractions serve runtime` also registers
configuration alongside logging and jobs (see
[abstraction-facade](https://github.com/openabstractions/abstraction-facade)).

## Today

The rungs, provenance and machine-file trust rules are stated in
[CONTRACT.md](CONTRACT.md), each rule tagged and accounted for in
[testdata/scenarios/rules.tsv](testdata/scenarios/rules.tsv). `go/corpus_test.go`
and `python/test_corpus.py` replay the scenarios under
[testdata/scenarios](testdata/scenarios) and compare each language's transcript
against the recorded `.expected` file byte for byte; Go and Python agree
through it. That corpus is internal to this layer, not yet cited by the
parent project's cross-repository conformance suite
(`conformance/capabilities.list`, `conformance/contracts.list`).

- A key the schema does not name is ignored today, not refused
  ([CFG-S1](CONTRACT.md)); refusal is stated and unbuilt.
- A farther rung locking a key against a nearer one ([CFG-R3](CONTRACT.md)) is
  stated and unbuilt; nothing exercises it.
- The editor bounds a persisted user record to 256 KiB; oversized storage
  returns `storage_unavailable` and writes nothing.
- Generated clients provide the wire interface; the Python artifact is
  vocabulary and transport injection only, with no separate Python config
  host — the service host is Go.
- Every published transcript was produced on Windows or Linux; macOS is
  unproven throughout.

## Where it sits

Below: [abstraction-cas](https://github.com/openabstractions/abstraction-cas)
and [abstraction-watch](https://github.com/openabstractions/abstraction-watch)
back the legacy file provider's reads and writes. Above:
[abstraction-facade](https://github.com/openabstractions/abstraction-facade)
resolves all three services, and
[abstraction-rights](https://github.com/openabstractions/abstraction-rights)
decides `ReplaceUser`'s edit policy.

One layer of [openabstractions](https://github.com/openabstractions/abstractions).
Every layer names one thing local tools rebuild on their own; the name means the
same in each language that implements it, and the conformance scenarios are what
hold an implementation to it.

## Requirements

Go 1.26 or newer for the Go module. Python 3.9 or newer for the legacy provider,
3.10 or newer for the generated protocol package. C++17 for the generated client.
No dependency outside the standard library and this project's own packages.
Tested on Windows and Linux; macOS is unproven.

## Licence

Apache-2.0. See [LICENSE](LICENSE).

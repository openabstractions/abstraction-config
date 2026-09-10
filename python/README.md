# abstraction-config, in Python

Where a machine keeps its answer to "which store", and who said so. An
administrator's machine-wide file, then this user's file, then the environment
for one run — each overriding the last, key by key, with the origin of every
answer kept beside it.

An application asks the machine and names no path. A key nothing set is absent,
which means this machine does not have that tier: a normal answer, not an error.

This page is the Python package. The Go implementation, the file format and what
is `UNPROVEN` are on
[the repository](https://github.com/openabstractions/abstraction-config).

## Install

Not on PyPI, and the names on PyPI are not ours. Clone the three repositories
beside each other and install them in dependency order:

    git clone https://github.com/openabstractions/abstraction-cas
    git clone https://github.com/openabstractions/abstraction-watch
    git clone https://github.com/openabstractions/abstraction-config

    pip install ./abstraction-cas/python ./abstraction-watch/python
    pip install ./abstraction-config/python

Python 3.9 or later.

## An example that runs

```python
import abstraction_config as config

store, why = config.job_store()
print("jobs live at:", store)
print("because:", why)

print(config.load().describe())
```

On a machine nobody has configured it prints the default and says so. It reads
files and writes none, so running it changes nothing.

## What an application calls

| call | what it does |
|---|---|
| `job_store()` | `(path, why)` — where jobs live on this machine, and what decided |
| `load()` | a `Config`: `nas_store`, `store`, `log_sink`, `log_service`, `off`. A file that cannot be read or parsed is skipped, with one line on stderr saying which |
| `Config.origin(key)` | which authority answered for one key, and which file said so |
| `Config.describe()` | what was found and where, for a `status` command |
| `Config.stamp()` | differs whenever the answer differs, so a long-running process can ask whether anything moved instead of rebuilding |
| `overridden()` | the keys the environment is deciding, whatever the files say |
| `user_path()` / `machine_path()` | the conventional locations on this platform |
| `watch(budget)` | a subscription that re-reads when the answer changes |

The environment variables that override a file, one per key:
`ABSTRACTION_NAS_STORE`, `ABSTRACTION_STORE`, `ABSTRACTION_LOG`,
`ABSTRACTION_LOG_SERVICE`.

## What may break

- **This module reads; it does not write.** There is no `save` here. A setup
  step or the Go implementation writes the file.
- **A machine-wide file no administrator owns raises `Untrusted`** rather than
  being obeyed. Ownership is what is checked, so a file on a filesystem that
  does not carry it is refused.
- **No conformance verdict.** No scenario in the suite cites this layer yet —
  [what is proven and what is not](https://openabstractions.org/coverage.html).
- **Not on any package index**, and no release carries an API stability promise.
  Pin a commit you have read.
- Every published transcript was produced on Windows or Linux. macOS is
  `UNPROVEN` throughout.

Apache-2.0. See [LICENSE](LICENSE).

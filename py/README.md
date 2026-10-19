# Python configuration service protocol

*For Python applications.*

Use this generated client with the Go Config service. The former handwritten
`../python` local provider was retired from current 0.8 source and distribution;
its old implementation remains in Git history.

Install the runtime first: https://openabstractions.org/adopt.html

The generated ConfigReader and ConfigEditor use the shared native IPC transport
through the resolved facade. Install the coordinated identity, config
and facade `py/` packages and configure the installed native IPC library as
specified in the facade Python README.

```python
from abstraction.facade import Scope
from abstraction.facade.client import Machine
from abstraction.config import RunOverrides

machine = Machine(timeout=2)
reader = machine.resolve_config(scope=Scope.LOCAL)
snapshot = reader.read(RunOverrides())
print(snapshot.store, snapshot.origins.store.rung)
editor = machine.resolve_config_editor(scope=Scope.LOCAL)
user = editor.read_user()
user.values.log_sink = "configured-provider-setting"
replacement = editor.replace_user(user.revision, user.values)
if replacement.outcome == "conflict":
    print("Settings changed; read and edit the current snapshot")
```

Paths in provenance describe the service's sources. Applications do not open
those paths. Caller environment overrides must be supplied as RunOverrides;
the service process environment is never substituted. Missing/forbidden services
raise an error, and no local provider is selected automatically. Resolve returns
a fixed endpoint with the Machine deadline/cancellation options. Repeat a later
operation using a fresh budget when an explicit deadline has expired.

The service preserves existing configuration records. User replacement compares
the revision and writes only the user rung. Version 0.0.0 is development packaging
metadata, with no published-release or native macOS claim.

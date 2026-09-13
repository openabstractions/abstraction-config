# Python configuration service protocol

The generated ConfigReader and ConfigEditor use the shared native IPC transport
through the resolved facade. Install the coordinated identity, logging, config
and facade `py/` packages and configure the installed native IPC library as
specified in the facade Python README.

```python
from abstraction.facade.client import Machine
from abstraction.config.rec import RunOverrides

machine = Machine(timeout=2)
reader = machine.resolve_config(scope="local")
snapshot = reader.Read(RunOverrides())
print(snapshot.store, snapshot.origins.store.rung)
editor = machine.resolve_config_editor(scope="local")
user = editor.ReadUser()
user.values.log_sink = "configured-provider-setting"
replacement = editor.ReplaceUser(user.revision, user.values)
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
the revision and writes only the user rung. The old `../python` package remains
an explicitly documented legacy provider. Version 0.0.0 is development packaging
metadata, with no published-release or native macOS claim.

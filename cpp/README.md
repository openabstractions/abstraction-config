# Configuration service client

The C++ client reads the existing per-user configuration provider through the identity-bound Go service. It does not read configuration files, create a store, run a provider, or fall back when the service is absent.

```cpp
#include <abstraction/config/client.hpp>
abstraction::config::Client config;
auto value = config.Read();
// value.store is configuration data, not a client-side store handle.
// value.origins.store gives its rung and provider file path.
```

Consume the installed package with `find_package(abstraction_config CONFIG REQUIRED)` and link `abstraction::config_client`. The package depends on `abstraction_ipc`; CMake accepts an installed dependency or the sibling identity checkout and never downloads one. The header is C++17. Generated types/interfaces/protocol come from `config.thrift`; `cpp/abstraction/config/rec.h` is installed into the public include tree.

The service is registered as `openabstractions serve config --endpoint <endpoint>`. Its default endpoint is the shared platform spelling for `config-v1`; `ABSTRACTION_CONFIG_ENDPOINT` explicitly overrides it for both clients. It runs as the user whose configuration files it reads. It is not a privileged cross-user broker. The service checks the kernel-bound caller's numeric UID or Windows SID against its own account before reading the provider.

`Read()` supplies the caller process's four existing nonempty run overrides: ABSTRACTION_NAS_STORE, ABSTRACTION_STORE, ABSTRACTION_LOG and ABSTRACTION_LOG_SERVICE. Empty variables do not override files. `ReadWithOverrides(RunOverrides)` supplies those values explicitly; an empty object means file/default answers only. These values are claims, not proof of the caller's environment, and are never used as authorization. The host's ABSTRACTION_* settings are not substituted for them.

Snapshot preserves the existing five values, every key's provenance, and the provider's value-only stamp. Missing tiers remain empty/default answers; malformed or untrusted files are ignored with existing provider diagnostics. A missing service is a transport error, not an empty configuration. Same-user refusal is a generated ServiceError with code wrong_user. Unknown request fields are refused by the generated schema. The client exposes read only: save/edit/watch, provider handles and policy remain outside this endpoint. Reading a path does not authorize opening service-owned state.

Build and stage without a persistent install:

```
cmake -S . -B build
cmake --build build --config Release
cmake --install build --config Release --prefix /absolute/staging/prefix
```

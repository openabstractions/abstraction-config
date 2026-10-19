# abstraction.config contract

Remote profile: the current service methods retain `local` (explicit or
default); the general paired listener does not offer this capability. Retained
capability-specific delegation keeps its existing contract and is separate
from that listener. Held by: `idl/gen/service_test.go`
(`TestDispatcherTableCarriesRemoteProfile`) and `serve/runtime_remote_test.go`
(`TestRemoteRuntimeDescribeUsesLiveGrantAndExplicitServices`).

Binds: `config.thrift`

`config.thrift` defines `abstraction.config/reader@1`, which reads
machine, user and process settings with per-key provenance;
`abstraction.config/editor@1`, which lets a same-account, [Program](https://openabstractions.github.io/reference.html#concept-identity.listener-program-need)-proven
caller replace the user location's settings under a compared revision; and
`abstraction.config/observer@1`, which long-polls the latest effective
snapshot. A caller of the legacy file provider calls
`LoadWithOverrides(values)` with its run overrides and is told what job
stores and log sinks this machine has. It is never told how to find them, and it is never
separately configured: there is one settings step per machine, performed by
whoever installs one, and zero per application.
[README.md](README.md) is the door — what this module is, how to obtain it,
one example that runs. That page cites rules by their ids and defines none.

## Reading this page

The conventions every OA contract carries are stated once in [CONVENTIONS.md](https://github.com/openabstractions/abstractions/blob/main/idl/CONVENTIONS.md); a rule here that applies one cites its `CONV-` id.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY" and "OPTIONAL" in this page are to be
interpreted as described in RFC 2119 and RFC 8174, when, and only when, they
appear in all capitals, as shown here.

A rule id such as `CFG-R1` is declared once, in bold brackets before its
title, at the head of the rule it names; tests and refusals cite it the same
way. A retired id is never reused; [HISTORY.md](HISTORY.md) keeps it with
the release it left. `HISTORY.md` also carries this contract's design
records and what was measured or tested, linked from here and linking back.

| letter | topic |
| --- | --- |
| R | location — which of machine, user or process answers a key |
| W | writing and editing — the process override, watching and the user editor |
| T | trust — the machine location's ownership check |
| S | schema — the settings schema |
| P | provenance — which location and path answered each key |
| A | admission — declared actions and request authority |
| E | error and outcome — the outcome words a call can return |
| X | extension — what may be added to the schema |

| Prose | Wire (until its own release) |
| --- | --- |
| location, values `machine`, `user`, `process` | `Origin.rung`, values `machine`, `user`, `environment`; the `RunOverrides` struct (`reader@2`, `editor@2`, in the 0.8.0 plan's W2-03 window; `research/vocabulary/RENAME-PLAN.md` N14) |

[testdata/scenarios/rules.tsv](testdata/scenarios/rules.tsv) is the
accounting for the legacy file provider's rules: every tag with the
scenarios that reach it, or the reason it is unreached. Full accounting,
never full coverage — a rule no sequence of driver operations can detect
says so there with its reason, and a rule nothing implements yet says
that. `go/corpus_test.go` reads the scenarios beside that file and compares its
transcript with the recorded `.expected` byte for byte; it refuses a tag no
rule names and a covered rule no expectation cites. Generated Python clients
use this Go service through the shared native binding.
That corpus holds [CFG-R1], [CFG-R2], [CFG-P1], [CFG-P2], [CFG-T1]
through [CFG-T4], [CFG-W1] through [CFG-W4], [CFG-S1] and [CFG-X1]; the
editor service's rules, [CFG-W5] through [CFG-W15], are held by
`go/config_test.go` instead (CFG-P2), and a citation that resolves to no
rule here is a defect in one of the two.

## The locations, and which one wins

A location is one of the three ranked sources a setting can come from:
machine, user or process (the environment).

**[CFG-R1] The nearer location wins: process over user over machine.**
For @2 the service MUST resolve user over machine. The generated client MUST
apply explicit process values after the service answer, under the same typed
catalogue validation. A trusted machine lock ([CFG-R4]) MUST survive that
overlay. No process values cross the @2 wire or enter its observation cursor.
For published @1, the service continues to resolve caller-supplied
`RunOverrides` over user over machine. Empty overrides leave farther values.

**[CFG-R2] A site location is reserved and absent.** Every ancestor has one
and the service MUST NOT implement it. The name is spent rather than free,
so a fourth location added later cannot mean something else. No operation
can detect this: an absent location answers nothing, so nothing tells
*reserved* from *never thought of*.

There is no application location, and that is settled rather than pending:
per-application policy is the `rights` module's record, not a setting,
because a setting is inherited by the process it would constrain.

## Writing and editing

**[CFG-W1] A subscription reports the answer when it differs.** A
subscription MUST report the answer when it differs, from whichever process
wrote it, and MUST name its mechanism. A platform that must be asked on a
timer MUST say so rather than hiding it in a latency. Held over wall time
and across processes by `abstraction-config/go/watch_test.go`; a transcript
compared byte for byte carries neither, so the corpus reaches the
comparison half only, as [CFG-W2].

**[CFG-W2] The stamp follows the answer, not the location that gave it.**
The stamp MUST differ whenever the answer differs and MUST stay unchanged
when the same values arrive from a different location. A process running
for days asks for the stamp rather than rebuilding everything to find out
whether it needs to, so a stamp that moved when only provenance moved would
make it rebuild for nothing. This is the comparison [CFG-W1] leaves to the
client, in the one form a transcript can hold.

**[CFG-W3] The environment is an override for the process location, and an
empty variable is not one.** Taking the variable away MUST give the file
its key back, which is what makes it an override rather than an edit. It
exists so a test, a container or a one-off run can redirect a location
without writing to a file other processes on the machine are reading.

A variable set to the empty string MUST NOT override. Shells export empty
strings for variables nobody set, and a location that took them would
silently unconfigure every machine that has one.

**[CFG-W4] A value taken from the environment is never observed to
change.** The service MUST NOT emit a change notice for it. An absence,
and it is why the environment is an override and not the mechanism: no
sequence of calls can observe a notice that is never sent.

**[CFG-W5] Settings carry no storage authority.** A path-valued setting
MUST NOT convey authority to open provider storage. `ConfigEditor` exposes
the same four strings and `off` map as settings.

**[CFG-W6] The user backing file is chosen, not supplied.** The service
MUST select its user-location path at startup; no request MUST supply that
path. The service MUST create that path's directory before observing, so a
profile with no configuration directory is observable.

**[CFG-W7] Reading the user location.** `ReadUser` MUST return user values
and an opaque normalized-content revision, excluding the machine and
process locations. Missing storage MUST read as empty values. Unreadable,
corrupt or unsupported storage MUST return `storage_unavailable`.

**[CFG-W8] Replacing the user location.** `ReplaceUser` MUST compare the
supplied revision under the existing CAS edit lock and MUST replace
atomically only on a match. A stale revision MUST return `conflict` with
the current user content and MUST NOT write. An empty expected revision
MUST return `invalid_revision`. A storage failure MUST return
`storage_unavailable`. A match MUST return `applied` with the written
snapshot.

For `config@2`, only `applied` and `conflict` MUST carry `UserSnapshotV2`.
Every other outcome MUST omit the snapshot and disclose no user state,
including `forbidden` and `not_permitted`.

**[CFG-W9] Clearing a user override.** An empty string or an empty `off`
map MUST remove the corresponding user override. Farther locations MUST
remain unchanged.

**[CFG-W10] Revision is a content token.** Equal normalized content MUST
have equal revision, including after restoring earlier values. The
revision MUST NOT stand for an operation identity or a monotonic sequence.

**[CFG-W11] Edit policy.** A host MAY configure `EnableEditPolicy` before
`Serve`. `ReplaceUser` MUST evaluate it for the rechecked peer after
same-account proof and before storage access. An evaluated refusal MUST
return `forbidden`; a failed or cancelled decision MUST return
`unavailable`, which a caller MAY retry. Both outcomes MUST carry empty
values and an empty revision and MUST leave storage untouched. The
installed runtime asks the rights service for
`abstraction.config/user.replace` on `abstraction.config/editor@1`.
`ReadUser` keeps same-account access alone. A client generated before
these outcomes MUST refuse to decode them.

**[CFG-W12] Waiting never grants a retry by itself.** An expired or
cancelled wait MUST NOT grant a write retry; the caller MUST reread and
reconcile an uncertain replacement before a new edit. The Go reader,
editor and observer MUST wait `client.DefaultTimeout`, two seconds, per
call unless the transport or the call's context sets another limit. An
observation call MUST wait its `wait_ms` plus `client.ObserveMargin`, five
seconds, whenever the transport's limit is shorter
(`TestObserveCallLimitCoversItsWait`). An
edit policy decision MUST answer within that wait: the installed runtime
bounds it to 500 ms and MUST answer `unavailable` past it.

**[CFG-W13] The machine location is omitted for an isolated runtime.** A
host MAY call `OmitMachineRung` before `Serve`. Its reads MUST then answer
from the user location and process overrides alone, reading neither the
machine file nor its directory. `serve runtime --state-dir` (every
`--isolated` runtime) MUST omit it beside its own user file.

**[CFG-W14] Unknown persisted fields are refused.** The editor MUST refuse
an unknown persisted field rather than drop it during replacement. This
strict boundary MUST NOT change `LoadWithOverrides` behavior.

**[CFG-W15] The persisted record is bounded.** A persisted editor record
MUST NOT exceed 256 KiB, leaving room for the generated snapshot and reply
envelope within the shared 1 MiB config frame budget. Both a CAS read and
a proposed replacement MUST enforce the bound. Oversized storage MUST
return `storage_unavailable`, and no partial replacement is written.

## The machine location is trusted when an administrator wrote it

**[CFG-T1] The machine location is read only when an administrator wrote it.**
The service MUST read the machine location only when the file passes
storage's `STO-L4` (`abstraction-storage/CONTRACT.md`), the one text of
the machine trust rule: the file and every folder from it up to the machine
root are owned by, and writable only by, an administrator. It MUST ignore
an untrusted file rather than obey it. A machine-wide file an ordinary process could write is a
machine-wide file anyone could write, so it does not get to speak for the
machine. Ignored is not deleted: the file stays where it is.

**[CFG-T2] An ignored file is said on stderr, never silently.** This
applies to every reason a file is not used: untrusted ownership, and a
file that will not parse. A malformed file MUST NOT take the application
down, and MUST NOT pass unmentioned either — the whole point of this rule
is that somebody can find out why a location is not being used, and
silence is the failure mode it exists to prevent.

**[CFG-T3] A machine file an administrator did own is read.** Untestable
by the corpus, and untestable for the reason that makes [CFG-T1] worth
having: a driver running as an ordinary user cannot create a root- or
Administrators-owned fixture, because the rule under test is exactly what
stops it writing one.

**[CFG-T4] A write to the machine location that no reader would trust is
refused, and leaves nothing behind.** [CFG-T1] read from the writing side.
Writing a file every reader ignores succeeds into silence — the tool
reports the machine configured and the machine disagrees — and the
directory matters more than the file: `C:\ProgramData` grants
`BUILTIN\Users` `(CI)(WD,AD,WEA,WA)`, so whoever creates
`%ProgramData%\abstraction` owns it and keeps it, and a machine location
whose directory an ordinary account owns can never be read by anybody
again, including the administrator who installs afterwards. An
unprivileged write MUST be refused, and refusing it is what leaves an
administrator somewhere to write.

Ownership can be tried and not predicted — the same call is
`Administrators` from an installer and the person from a shell — so the
directory is created, asked who owns it, and removed again when the
answer is wrong.

## The schema

**[CFG-S1] Unknown fields cannot erase future settings.** The deprecated
projection MUST ignore unnamed user-record fields when reading and preserve
them when replacing its five fields. Typed edits MUST refuse an unknown
catalogue key. A parseable known stored value of the wrong kind or outside
its bounds MUST resolve to its default with `ignored` and `invalid_value`.
Other valid keys continue to resolve. An unparseable record is `corrupt`
with the zero-based first failing byte offset; outcome qualifies offset zero.
Unknown stored members have bounded display diagnostics: at most sixteen
sanitized name excerpts and offsets, with a count of omitted diagnostics.
Excerpts are never keys or selectors. Untouched unknown member bytes MUST be
spliced into the same CAS edit; whole-file layout need not be preserved.
The wire schema lives in `config.thrift`; its key catalogue lives in the
single sibling `keys.json`, validated against `KeyDescriptor` when bindings
are generated.

The unpublished @2 catalogue retires `prompt_cache_max_retention` and
`prompt_cache_off_for` before 0.8.0 publication (owner 2026-11-05 D2;
coordinator 664/669). They are unknown keys, and any stored member bytes remain
preserved under CFG-S2's ordinary unknown-member rule. `prompt_cache_persist`
and `prompt_cache_max_use` remain catalogue entries for the accepted future
conversation-memory design; 0.8.0 has no consumer and offers neither control.
The existing engine-profile RAM cache and its fixed defaults are unchanged.

`model_context` maps a local model family to the person's per-conversation
context in decimal tokens; an absent family is Automatic (architect decision D,
2026-11-08). The Panel's Models page writes it. modelhostd reads it at each load
as a designated provider whose facet names the key (CFG-X1), watches it, and
unloads a loaded model of a changed family once no request uses it. The runtime
reads it for the inventory's per-conversation context. modelhostd logs a
refused read.

**[CFG-X1] Values carry no secret; delivery to a provider is a grant.** A
runtime-designated provider reads only its exact registered keys under
`abstraction.config/key.read`. Native receiving-peer and registration evidence
MUST establish designation; a supplied provider name or role is not proof.
Absent native designation MUST refuse. Ordinary application read gating is
deferred; settings never contain credentials or permission decisions.
A key read never raises a question; a read without a rule is refused.
A remote or paired peer's key read keeps the ordinary decision path.

## Provenance

**[CFG-P1] Every answer carries provenance per key.** @2 provenance MUST
identify the location and diagnostic path that answered, with state and any
shadowed settings. A default answer MUST have no location and an empty path;
`KeyState.default` identifies it. Published @1 keeps its `rung` and `default`
provenance words.

| Prose | Published @1 wire | @2 wire |
| --- | --- | --- |
| location | `Origin.rung` | `KeyOrigin.location` |
| process | `run` / `RunOverrides` | `process`, applied by the generated client |
| default | `default` location | absent location, `KeyState.default` |

**[CFG-P2] The path half of provenance names the file that answered.**
Held by `abstraction-config/go/config_test.go` rather than by the
corpus: a path is one machine's, so no transcript two machines compare
byte for byte can carry it.

## Outcomes

**[CFG-E3] Only a resolved answer carries settings.**
`storage_unavailable`, `caller_unavailable`, `identity_required`,
`wrong_user`, `forbidden`, `unavailable` and `invalid_revision` MUST carry
no settings; `conflict` MUST carry the current user snapshot without
applying the proposed one; `applied` MUST carry the written snapshot.

| Call | Outcome | Meaning |
| --- | --- | --- |
| `Read` | success | Returns a `Snapshot`: the five keys, per-key provenance and a stamp (CFG-P1, CFG-W2). |
| `Read`, `ReadUser` | `caller_unavailable` | The caller's account could not be established. |
| `Read`, `ReadUser` | `identity_required` | The transport carries no identity to check. |
| `Read`, `ReadUser` | `wrong_user` | The caller does not own the service's account. |
| `Read` | `storage_unavailable` | A trusted machine or user file could not be read (CFG-T1, CFG-T3). A user file that reads but does not parse answers as an empty user location. The merged loader refuses trusted machine-source I/O errors before returning settings. |
| `ReadUser` | success | Returns user values and a revision (CFG-W7). |
| `ReadUser` | `storage_unavailable` | Unreadable, corrupt or unsupported storage; missing storage is empty values, not this outcome (CFG-W7). |
| `ReplaceUser` | `applied` | The compared revision matched; returns the written snapshot (CFG-W8). |
| `ReplaceUser` | `conflict` | A stale revision; returns the current snapshot, no write (CFG-W8). |
| `ReplaceUser` | `invalid_revision` | The expected revision is empty (CFG-W8). |
| `ReplaceUser` | `storage_unavailable` | The write could not reach storage (CFG-W8, CFG-W15). |
| `ReplaceUser` | `forbidden` | An evaluated edit policy refused the write (CFG-W11). |
| `ReplaceUser` | `unavailable` | The edit policy decision could not be obtained; retryable (CFG-W11). |
| `Observe` | `snapshot` | A new revision; carries values, provenance and a new cursor (CFG-W1). |
| `Observe` | `unchanged` | The same revision; carries the original cursor, no snapshot (CFG-W1). |
| `Observe` | `gap` | The provider restarted or the override binding changed; requires an empty-cursor restart. |
| `Observe` | `unsupported` | The provider offers no change notification. |
| `Observe` | `unavailable` | Bounded waiter exhaustion or provider notification failure. |
| `Observe` | `invalid` | The cursor or an override exceeds its bound. |

## Bounds

| what | bound | rule |
| --- | --- | --- |
| persisted editor record | at most 256 KiB | CFG-W15 |
| config wire frame | at most 1 MiB (shared) | CFG-W15 |
| Go client per-call wait | 2 seconds (`client.DefaultTimeout`) | CFG-W12 |
| Go observation call wait | `wait_ms` + 5 seconds (`client.ObserveMargin`) | CFG-W12 |
| edit policy decision | at most 500 ms | CFG-W12 |

## Typed settings (`reader@2`, `editor@2`, `observer@2`)

**[CFG-R4] Trusted machine locks.** A catalogue `lockable` key set in the
existing administrator-trusted machine file MUST win over user and process
values. An unset machine value falls through. An untrusted machine file
confers no lock. Provenance names `machine`, `locked`, its diagnostic path
and shadowed locations; an edit of a locked key is `not_permitted` with
`locked:machine`. External Group Policy and MDM are deferred.

**[CFG-S2] Typed catalogue entries.** Keys are `Entry` values described by
`KeyDescriptor`, never record fields. `Value.kind` selects one payload;
nonselected payloads MUST be zero. A selected zero/false/empty payload is a
value, while absent `Entry.value` or `Change.set` is unset. `unlimited` carries
no payload and is admitted only by `allows_unlimited`; zero never means
unlimited. All five generated
bindings expose the same catalogue and generic atomic process overlay.

`Read(names)` returns effective service entries without process overrides.
Unknown requested names are refused as `unknown_key`; ungranted names are
`not_granted`, never unset entries. The generated process overlay validates
the whole batch, retains machine locks and never inserts an omitted key.
`Describe` supplies catalogue metadata, including bounds, choices, presets,
default, lockability and live/restart apply mode. `logs_max_use` has a runtime
default supplied by logging's existing drive policy; its description is
"10% of the drive, at most 4 GB".

Ignored document fields are reported only to callers admitted for every
catalogue key. A key-scoped reader's revision excludes those diagnostics.
Each ignored field identifies its actual `ConfigSource` location and path.
A corrupt effective read carries `corrupt_source`; user-only results have
the selected user source implicitly. `corrupt_offset` is meaningful only
under `corrupt`, including omitted offset zero. Native producers emit zero
under every other outcome.

Only `cache_max_use`, `cache_max_retention`, `logs_max_use` and
`logs_max_retention` allow unlimited values. Duration bounds fit the existing
retention owners' representation; consumers still perform checked conversion.

`ReadUser` and `ReplaceUser(expected_revision, changes)` use the same direct
CAS record as @1. A batch sets or explicitly unsets all requested keys or
none. A conflict returns the current user snapshot; its revision is distinct
from the effective read revision. @1 replacement changes only its five
deprecated fields and preserves every new and unknown raw field. The @2 public
`disabled_download_delegates` key projects the existing native `off` reason
map; there is one stored map, with unchanged @1 projection.

The observer uses the existing native invalidation source and bounded wait
gate. A lock or pending-state change changes the effective revision. A restart
key differing from the consumer's actually admitted startup snapshot is
`pending`; neither process overrides nor caller-supplied claims enter its
cursor. There is no periodic polling, revision-history store or restore API.
The lazy NAS executor publishes its admitted value after successful share
preparation. That publication invalidates the same native observation gate.
The cursor also binds a corrupt or unavailable effective read. Repeating that
cursor waits on native invalidation, and a repaired source yields a new
snapshot; a consumer needs no polling loop to wait for repair.

The published @1 records and services remain unchanged. @2 uses new records,
including `KeyOrigin`, and the typed profiles; it does not widen @1 closed
records or add `RunOverrides` to the new wire surface.


**[CFG-A1] The action catalogue is closed.** The IDL `resource_actions`
catalogue MUST contain every action of this service. `abstraction.config/key.read`
authorizes an exact named-key read; `abstraction.config/user.replace`
authorizes the atomic user edit. Metadata grants no access. Unknown actions
MUST refuse. Both retain `abstraction.rights/legacy-exact-string@1` until a
typed key profile has its own accepted contract.

**[CFG-S3] Describe returns one bounded catalogue.** `DescribeResult` MUST
carry `read` and at most 64 descriptors, or a refusing outcome with no
descriptors. The catalogue is a definition-owned constant. Its fixed bound
is the declared CONV-L1 divergence below.

**[CFG-E4] Typed outcomes and refusals.** @2 call outcomes MUST refuse unknown
words (CONV-E1). A boundary identity refusal is `forbidden` with identity
`RefusalDetail`; an evaluated rule refusal is `not_permitted` with Rights
detail. A per-key `not_granted` MUST carry that detail when the Rights rule
refuses a designated reader. When no registration designates the reader for
the key (CFG-X1), because its registration names no such key or its kept
image does not match, the per-key `not_granted` MUST carry identity detail
with decision `not_granted` instead
(`TestTypedReadNamesAMissingDesignationApartFromAMissingRule`).
`unknown_key` carries none. Machine locks retain `KeyProblem` with `locked:machine`.

**[CFG-W16] Typed observation follows CONV-O1.** A new revision returns
`changed` and its snapshot; expiry at the same good revision returns
`unchanged`. A read failure returns `unavailable`, retaining the latest
snapshot for the caller's current visibility with `age_ms` when one exists.
Retained values MUST be authorized again before delivery. The service keeps
at most 64 current visibility snapshots in memory. Eviction yields no retained
snapshot and absent age. Failure of native notification returns `failed` with
cause `notification_source_failed`; the consumer ends its observation.

## Divergences

- **CFG-S3 / CONV-L1.** Describe returns the complete constant catalogue,
  bounded to 64 descriptors and 256 KiB, in one reply. Paging adds no
  visibility or memory bound to this definition-owned finite list.

- **CFG-P2.** The file lives at the platform's settings directory —
  `%AppData%`, `~/Library/Application Support`, `$XDG_CONFIG_HOME`
  ([os.UserConfigDir](https://pkg.go.dev/os#UserConfigDir)) — never the
  registry, plists or dconf: three languages already read that one file
  byte for byte, and no platform store is readable from all three without
  a platform binding per language. The seat is the POSIX kind: each
  platform furnishes scope, provenance and notification its own way, and
  there is no common call an application can make on all three.

## Not built

- C++ observation: the C++ facade resolves `reader@1` and `editor@1` and
  has no typed `observer@1` client.
- The remote form. An application reaching this service on another
  machine's runtime has no binding. When it is built, a remote receiver
  MUST refuse every request that needs a person with the outcome `forbidden`
  (`ReplaceUser`'s refusal, CFG-W11) and the reason "no person reachable on
  this runtime", until remote questions exist. A forwarded
  caller is the forwarding runtime's certificate subject; the agent behind
  it is an unverified label.


- A typed `config-key@1` Rights resource profile remains pending.
- `nas_store` remains provider settings pending the delegate/share design.
- Cache and prompt retention/application consumers require their owning runtime
  implementations; catalogue declarations alone do not activate them.
- Remote provenance must omit local diagnostic paths when a remote form ships.

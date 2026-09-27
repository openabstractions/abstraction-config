# abstraction.config contract

Binds: `config.thrift`

`config.thrift` defines `abstraction.config/reader@1`, which reads
machine, user and process settings with per-key provenance;
`abstraction.config/editor@1`, which lets a same-account, Program-proven
caller replace the user location's settings under a compared revision; and
`abstraction.config/observer@1`, which long-polls the latest effective
snapshot. A caller of the legacy file provider calls
`LoadWithOverrides(values)` with its run overrides and is told what tiers
this machine has. It is never told how to find them, and it is never
separately configured: there is one settings step per machine, performed by
whoever installs a tier, and zero per application.
[README.md](README.md) is the door — what this module is, how to obtain it,
one example that runs. That page cites rules by their ids and defines none.

## Reading this page

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
| E | error and outcome — the outcome words a call can return |
| X | extension — what may be added to the schema |

| Prose | Wire (until its own release) |
| --- | --- |
| location, values `machine`, `user`, `process` | `Origin.rung`, values `machine`, `user`, `environment`; the `RunOverrides` struct (`reader@2`, `editor@2`, `research/vocabulary/RENAME-PLAN.md` §4 "second") |

[testdata/scenarios/rules.tsv](testdata/scenarios/rules.tsv) is the
accounting for the legacy file provider's rules: every tag with the
scenarios that reach it, or the reason it is unreached. Full accounting,
never full coverage — a rule no sequence of driver operations can detect
says so there with its reason, and a rule nothing implements yet says
that. `go/corpus_test.go` and `python/test_corpus.py` read the scenarios
beside that file and compare each language's transcript with the recorded
`.expected` byte for byte, so the two languages agree through it; the same
tests refuse a tag no rule names and a covered rule no expectation cites.
That corpus holds [CFG-R1], [CFG-R2], [CFG-P1], [CFG-P2], [CFG-T1]
through [CFG-T4], [CFG-W1] through [CFG-W4], [CFG-S1] and [CFG-X1]; the
editor service's rules, [CFG-W5] through [CFG-W15], are held by
`go/config_test.go` instead (CFG-P2), and a citation that resolves to no
rule here is a defect in one of the two.

## The locations, and which one wins

A location is one of the three ranked sources a setting can come from:
machine, user or process (the environment).

**[CFG-R1] The nearer location wins: process over user over machine.** The
service MUST resolve each key from the nearest location that answered it:
process, then user, then machine. The process location MUST be the
environment. The user location MUST beat the machine location, so a user
can opt out of what an administrator turned on without needing an
administrator, except a retention key ([CFG-R4]): the administrator owns
the disk.

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
and across processes by `abstraction-config/go/watch_test.go` and
`abstraction-config/python/test_abstraction_config.py`; a transcript
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
path.

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

## The machine location is trusted by ownership

**[CFG-T1] The machine location is read only when trusted by ownership.**
The service MUST read the machine location only when an administrator owns
the file and its directory, and MUST ignore an untrusted file rather than
obey it. A machine-wide file an ordinary process could write is a
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

**[CFG-S1] An unnamed key is ignored today, not refused.** The service
MUST ignore a key the schema does not name; refusing it is
[Not built](#not-built). The schema's home MUST be this module's one
definition in the IDL profile, the same way `abstraction-job/job.thrift`
is the job module's.

**[CFG-X1] No secret and no policy is a setting.** Nobody is refused a
read and nothing needs to be, because this module carries pointers to
tiers and limits the service enforces. A key that would need a read
restriction is in the wrong module. This is a rule about what may be
added to the schema rather than about any answer, so no sequence of calls
can detect it.

## Provenance

**[CFG-P1] Every answer carries provenance per key: a location and a
path, or `environment`, or `default`.** Provenance MUST be reported per
key and not per struct. A machine that sets `nas_store` in the machine
file and `store` in the user file has two answers from two authorities,
and one string for the whole settings document can only name one of them
— a person who cannot work out why their downloads are going somewhere
unexpected needs the file that says so for the key they asked about.

A key nothing set MUST report the `default` location. That is an answer,
not a missing one: absent means this machine does not have that tier,
which is normal.

**[CFG-P2] The path half of provenance names the file that answered.**
Held by `abstraction-config/go/config_test.go` and
`abstraction-config/python/test_abstraction_config.py` rather than by the
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
| `Read` | `storage_unavailable` | A trusted machine or user file could not be read (CFG-T1, CFG-T3). |
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
| edit policy decision | at most 500 ms | CFG-W12 |

## Divergences

- **CFG-P2.** The file lives at the platform's settings directory —
  `%AppData%`, `~/Library/Application Support`, `$XDG_CONFIG_HOME`
  ([os.UserConfigDir](https://pkg.go.dev/os#UserConfigDir)) — never the
  registry, plists or dconf: three languages already read that one file
  byte for byte, and no platform store is readable from all three without
  a platform binding per language. The seat is the POSIX kind: each
  platform furnishes scope, provenance and notification its own way, and
  there is no common call an application can make on all three.

## Not built

- [CFG-S1] Refusing an unknown key. Both implementations ignore one
  today instead.

### Next major (`reader@2`, `editor@2`, `observer@2`)

**[CFG-R4] Locked retention key.** A retention key set at the machine
location MUST win over the user location; an unset machine value MUST
answer from the user location. This is the reversal [CFG-R1] otherwise
forbids, reserved for these five keys alone.

**[CFG-S2] Retention keys.** The schema gains five optional keys, absent
as default: `cache_max_use`, `cache_max_retention`, `tmp_max_retention`,
`logs_max_use` and `logs_max_retention`.

`Snapshot`, `Origins` and `RunOverrides` refuse an unknown field
(`config.thrift`), so five new fields are a version, not an addition:
[CFG-R4] and [CFG-S2] land together at `reader@2`, `editor@2` and
`observer@2`, carrying [CFG-R1]'s location values beside them
(`research/storage-classes/DECISION.md` §8,
`research/vocabulary/RENAME-PLAN.md` §4 "second").

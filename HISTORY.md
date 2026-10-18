# History: abstraction.config/reader@1, editor@1, observer@1

Linked from [CONTRACT.md](CONTRACT.md)'s "Reading this page."

## 2026-10-06: the job store leaves the home folder root

The file job store's default moved from `~/.abstraction` to `job-store`
under the per-user data folder: Go `LegacyJobStore()` over `DataDir()`,
Python `legacy_job_store()` over `data_dir()`. File History backs up the
home folder root whole, and the old store held a socket reparse point.
Two fallbacks are removed: an existing `~/.modelget` is no longer the
store, and `UserPath()`/`user_path()` no longer fall back to
`~/.abstraction/config.json`. For one release, a default store with no jobs
beside a `~/.abstraction` or `~/.modelget` that has jobs prints one line
naming that folder and `ABSTRACTION_STORE`.

Go and Python diverge on FreeBSD and other Unix systems: Go `DataDirFor`
refuses any platform other than windows, darwin and linux, and Python
`data_dir()` treats them as Linux (`$XDG_DATA_HOME`, default
`~/.local/share`).

`abstraction-model/go/go.mod` now requires `abstraction-config/go` directly
for `LegacyJobStore`. A `GOWORK=off` build of modelget fails until
`abstraction-config/go` is released with `datadir.go` and the requirement is
bumped to that version.

## Superseded ids

**CFG-R3**, "No rung locks a key against a nearer one today," is retired
this release. [CFG-R1] held without exception until the retention keys
(`research/storage-classes/DECISION.md` §8): a farther location locking a
key against a nearer one is now the one designed reversal of [CFG-R1], and
[CFG-R4] (`Not built`, "Next major") is its reason and its replacement.

`CFG-E1` and `CFG-E2`, "The environment is an override for one run" and "A
value taken from the environment is never observed to change," are retired
as ids and continue as [CFG-W3] and [CFG-W4]: `E` is reserved for error and
outcome in every contract (`research/vocabulary/DECISION.md` S12), and this
page's environment rules continue the `W` ("writing and editing") sequence
instead (`research/vocabulary/REVIEW.md` finding 1).

`CFG-R1`, `CFG-R2`, `CFG-P1`, `CFG-P2`, `CFG-T1`..`CFG-T4`, `CFG-W1`,
`CFG-W2`, `CFG-S1` and `CFG-X1` keep their numbers, restated in the
`**[CFG-R1] Title.**` form this shape requires. `CFG-W5`..`CFG-W15` and
the outcomes rule are declared for the first time: the "User editing
service" table and the outcome words `config.thrift` already carried as
doc comments and refusal codes, with no rule id a test or refusal could
cite.

The outcomes rule, "Only a resolved answer carries settings," was declared
`CFG-E1` on 2026-09-23 and is `CFG-E3` from 2026-09-24. `CFG-E1` had been
the environment rule, now [CFG-W3], so a stale citation of the environment
rule resolved against the outcomes rule (`research/vocabulary/DECISION.md`
§7 N1). S3 as amended numbers a returning letter after the highest number
it carried. `CFG-E1` and `CFG-E2` stay retired with both meanings:

| retired | now | rule |
| --- | --- | --- |
| `CFG-E1` | [CFG-W3]; [CFG-E3] | the environment is an override (to 2026-09-23); only a resolved answer carries settings (2026-09-23) |
| `CFG-E2` | [CFG-W4] | a value taken from the environment is never observed to change |
| `CFG-R3` | none; [CFG-R4] replaces it | no rung locks a key against a nearer one |

## Design records

- `research/vocabulary/DECISION.md` §2 — D6 (layer → module), D20 (rung,
  tier → **location**, values machine, user, **process**; wire `rung` and
  `run` stay until `reader@2`), D30 (a still-shipped earlier interface →
  deprecated, its removal release named or "kept"). Applied this release:
  D6 and D20 in prose throughout; D30 in `README.md`'s "Legacy local
  provider" section, no removal release named in
  `research/vocabulary/RENAME-PLAN.md` or `DECISION.md`, so recorded
  "kept."
- `research/vocabulary/DECISION.md` §3 (S1–S13) — the contract shape this
  page and its "Reading this page" section follow. The page code is `CFG`
  (S4). S12 reserves `A`, `E` and `X` with one meaning in every contract;
  every other letter (`R`, `W`, `T`, `S`, `P`) is declared in this page's
  own table (S12, `research/vocabulary/REVIEW.md` finding 1, which names
  this module by name: "config's editing rules continue `CFG-W`").
- `research/vocabulary/RENAME-PLAN.md` "config (7 steps)" — the seven rows
  this release applies, cited by number in "Applied and skipped" below.
- `research/vocabulary/RENAME-PLAN.md` §4 "second" release — schedules
  `config/reader@2` and `editor@2` carrying `location` and `process` onto
  the wire; recorded as the S13 prose-to-wire table entry and as a
  "Not built" line in CONTRACT.md until that release.
- `research/storage-classes/DECISION.md` §8, applying
  `research/storage-classes/REVIEW.md` finding 6 ("accept with changes"):
  the five retention keys and the machine-location lock. [CFG-R4] and
  [CFG-S2] are declared in CONTRACT.md's "Not built" / "Next major"
  section rather than as live rules, because `Snapshot`, `Origins` and
  `RunOverrides` refuse an unknown field (`config.thrift`), which makes
  the five new fields a major version (`reader@2`, `editor@2`,
  `observer@2`) rather than an additive minor — the finding's own
  rejection of "a minor mechanism that hid the keys from old clients."
  `CFG-R3` moves to this file with [CFG-R4] as its reason, as the finding
  states; [CFG-R1]'s rationale gains the retention-key exception and
  [CFG-X1] reads "pointers to tiers and limits the service enforces," both
  applied live this release, not deferred to the next major.

## Tested

```bash
(cd go && go test ./...)
(cd python && python -m unittest discover -p 'test_*.py')
```

## Prior art

Group Policy, dconf, `UserDefaults` and GSettings order locations, lock one
against a nearer location, and answer per-key provenance in the shapes this
module's locations, [CFG-R4] (superseding `CFG-R3`) and [CFG-P1] take; the
rationale is in the commit history.

## Applied and skipped

- Step 1 — rung, tier → location; run → process (D20); wire `rung`/`run`
  at `reader@2` and `editor@2`: applied throughout CONTRACT.md and
  README.md's prose, except two senses the cited lines exclude and this
  page keeps: "tiers" as the `off` map's storage backends (nas/local,
  `CONTRACT.md`'s opening paragraph and README's "Legacy local provider"
  table) and "run overrides" as the established name paired with the
  unchanged `RunOverrides` wire struct. Neither appears in the plan's
  cited line ranges (`CONTRACT.md:3`, `:17–37`, `:48`, `:67`, `:95`,
  `:130`; `README.md:8`), and both keep a different, correct meaning.
  `ref:69`, `ref:116` and `ref:250` are the site glossary's anchors, owned
  by `openabstractions-flat/openabstractions.github.io/reference.html`,
  outside this module's `WRITES`; not touched here.
- Step 2 — letter table: R location, W writing and editing (`CFG-E` →
  `CFG-W` continues), T, S, P, X: applied. Ids `CFG-R1`, `CFG-R2`
  (location); `CFG-W1`..`CFG-W15` (writing and editing, continuing past
  the retired `CFG-E1`/`CFG-E2`); `CFG-T1`..`CFG-T4` (trust); `CFG-S1`
  (schema); `CFG-P1`, `CFG-P2` (provenance); `CFG-X1` (extension); `CFG-E1`
  declared fresh for outcomes, the letter S12 reserves and this page had
  never used. The former "User editing service" table (old lines
  182–214) becomes `CFG-W5`..`CFG-W15`, one rule per row plus the
  `OmitMachineRung`, unknown-field and 256 KiB paragraphs that followed
  it, all one topic under the continued `W`.
- Step 3 — "Declared divergence" → "Divergences" (S6): applied. The one
  divergence (the settings file's platform directory, never the registry,
  plists or dconf) is tied to [CFG-P2], the provenance rule about the
  path that names the file.
- Step 4 — Tested, Prior art → `HISTORY.md`; Not built stays: applied.
  Both sections above; CONTRACT.md's "Not built" keeps `CFG-S1` and gains
  the storage-classes "Next major" subsection.
- Step 5 — layer → module (D6): applied across CFG-P1, (the retired)
  CFG-R3's reason, CFG-S1 and CFG-X1, and the nine lines the plan's
  citation elsewhere in this contract.
- Step 6 — legacy file provider, Load/Watch/JobStore → deprecated with
  removal release (D30): applied in README.md's "Legacy local provider"
  section and table heading; no release is named anywhere in
  `RENAME-PLAN.md` or `DECISION.md` for this module's legacy provider, so
  it is recorded "kept," matching the asks module's precedent for the
  same unresolved case.
- Step 7 — Settings "Disabled tiers" → "Disabled locations"; "NAS
  store"/"Local store" → "NAS job directory (deprecated)"/"Job directory
  (deprecated)": **not applied here.** `monitor/settings_page.go:7` and
  `:20` belong to the monitor module, outside this module's `WRITES`; its
  dual reader and caption text land with that module, not this one.

## Reviewed

`research/vocabulary/REVIEW.md` finding 1 ("S12 against S4 and the plan's
letters") names this module directly: "config's editing rules continue
`CFG-W`," applied as step 2 above. `research/vocabulary/REVIEW.md` finding
2 ("D20 'level'") settles `location`, values `machine`, `user`, `process`,
applied throughout. `research/storage-classes/REVIEW.md` finding 6 ("the
lock contradicts CFG-R3, CFG-R1, the glossary and CFG-X1") is applied in
full: `CFG-R3` moved here with `CFG-R4` as its reason, `CFG-R1`'s
exception and `CFG-X1`'s reworded sentence live in CONTRACT.md now, and
`CFG-R4`/`CFG-S2` are declared under "Not built" / "Next major" pending the
`reader@2`/`editor@2`/`observer@2` major the finding requires. The
glossary's *Rung* entry citing `CFG-R4` and the Panel's Settings caption
are outside this module's `WRITES`.

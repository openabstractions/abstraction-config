# Contract

Every rule this layer states, each carrying a tag, in the order they were
decided. A conformance scenario cites the tag it tests on its `# expect` line,
and a citation that resolves to no rule here is a defect in one of the two.

[README.md](README.md) is the door — what this layer is, how to obtain it, one
example that runs. No rule on that page carries a tag.

An application calls `Load()` and is told what tiers this machine has. It is
never told how to find them, and it is never separately configured: there is one
configuration step per machine, performed by whoever installs a tier, and zero
per application.

## The rungs, and which one wins

**[CFG-R1] The nearer rung wins: run over user over machine.** Three rungs,
farthest first — machine, user, run — and the run rung is the environment. The
user rung beats the machine rung so that a user can opt out of what an
administrator turned on without needing an administrator.

The ancestors order their scopes the same way and phrase it as ours is phrased.
Group Policy processes local, site, domain and OU in that order, and "settings
that are applied later can override settings that are applied earlier"
([Group Policy processing and precedence](https://learn.microsoft.com/en-us/previous-versions/windows/it-pro/windows-server-2003/cc785665(v=ws.10)));
dconf lists `user-db` then `system-db:local` then `system-db:site`, where "the
first database in a profile is the write-to database and the remaining databases
are read-only" ([dconf profiles](https://help.gnome.org/admin/system-admin-guide/stable/dconf-profiles.html.en));
`UserDefaults` searches managed, argument, app, suite, global and registration
domains in that order
([UserDefaults](https://developer.apple.com/documentation/foundation/userdefaults));
XDG names `$XDG_CONFIG_HOME` and the "preference-ordered set" `$XDG_CONFIG_DIRS`
([XDG Base Directory](http://specifications.freedesktop.org/basedir/latest/)).

**[CFG-R2] A site rung is reserved and absent.** Every ancestor has one and we
do not implement it. The name is spent rather than free, so a fourth rung added
later cannot mean something else. No operation can detect this: an absent rung
answers nothing, so nothing tells *reserved* from *never thought of*.

There is no application rung, and that is settled rather than pending:
per-application policy is the `rights` layer's record, not a setting, because a
setting is inherited by the process it would constrain.

**[CFG-R3] A farther rung may lock a key against a nearer one.** Not built. This
is the one place [CFG-R1] is reversed, and every ancestor has it: dconf reverses
the order of precedence for locks
([dconf lockdown](https://help.gnome.org/admin/system-admin-guide/stable/dconf-lockdown.html.en)),
Group Policy's **Enforced** applies even with a block from above set, and Apple's
managed domain is searched first because "apps can't change the value of managed
keys"
([objectIsForced](https://developer.apple.com/documentation/foundation/userdefaults/objectisforced(forkey:))).
Ours is stated and unbuilt: no rung can lock a key today, so there is nothing for
a scenario to reach.

## Provenance

**[CFG-P1] Every answer carries provenance per key: a rung and a path, or
`environment`, or `default`.** Per key and not per struct. A machine that sets
`nas_store` in the machine file and `store` in the user file has two answers from
two authorities, and one string for the whole configuration can only name one of
them — a person who cannot work out why their downloads are going somewhere
unexpected needs the file that says so for the key they asked about.

A key nothing set came from the `default` rung. That is an answer, not a missing
one: absent means this machine does not have that tier, which is normal.

Answering per key is what the ancestors do. Apple's `objectIsForced(forKey:)`
answers, per key, whether an administrator provided the value; GSettings
distinguishes `g_settings_get_user_value` from `g_settings_get_default_value` per
key ([GSettings](https://docs.gtk.org/gio/class.Settings.html)).

**[CFG-P2] The path half of provenance names the file that answered.** Held by
`abstraction-config/go/config_test.go` and `abstraction-config/python/test_abstraction_config.py` rather
than by the corpus: a path is one machine's, so no transcript two machines
compare byte for byte can carry it.

## The machine rung is trusted by ownership

**[CFG-T1] The machine rung is read only when an administrator owns the file and
its directory, and an untrusted file is ignored rather than obeyed.** A
machine-wide file an ordinary process could write is a machine-wide file anyone
could write, so it does not get to speak for the machine. Ignored is not deleted:
the file stays where it is.

The ancestor is OpenSSH `StrictModes`, and the check is the same one — ownership
of the file and of the directory holding it, not permission bits alone.

**[CFG-T2] An ignored file is said on stderr, never silently.** This applies to
every reason a file is not used: untrusted ownership, and a file that will not
parse. A malformed file does not take the application down and does not pass
unmentioned either — the whole point of this layer is that somebody can find out
why a tier is not being used, and silence is the failure mode it exists to
prevent.

**[CFG-T3] A machine file an administrator did own is read.** Untestable by the
corpus, and untestable for the reason that makes [CFG-T1] worth having: a driver
running as an ordinary user cannot create a root- or Administrators-owned
fixture, because the rule under test is exactly what stops it writing one.

**[CFG-T4] A write to the machine rung that no reader would trust is refused,
and leaves nothing behind.** [CFG-T1] read from the writing side. Writing a file
every reader ignores succeeds into silence — the tool reports the machine
configured and the machine disagrees — and the directory matters more than the
file: `C:\ProgramData` grants `BUILTIN\Users` `(CI)(WD,AD,WEA,WA)`, so whoever
creates `%ProgramData%\abstraction` owns it and keeps it, and a machine rung
whose directory an ordinary account owns can never be read by anybody again,
including the administrator who installs afterwards. So an unprivileged write is
what takes the rung away, and refusing it is what leaves an administrator
somewhere to write.

Ownership can be tried and not predicted — the same call is `Administrators`
from an installer and the person from a shell — so the directory is created,
asked who owns it, and removed again when the answer is wrong.

## The environment is an override for one run

**[CFG-E1] The environment is an override for one run, and an empty variable is
not one.** Taking the variable away gives the file its key back, which is what
makes it an override rather than an edit. It exists so that a test, a container
or a one-off run can redirect a tier without writing to a file other processes on
the machine are reading.

A variable set to the empty string is not an override. Shells export empty
strings for variables nobody set, and a rung that took them would silently
unconfigure every machine that has one.

**[CFG-E2] A value taken from the environment is never observed to change.** An
absence, and it is why the environment is an override and not the mechanism.
Kubernetes reports the same shape from the other side: mounted ConfigMaps "are
updated automatically" after a kubelet sync period, and environment variables are
not updated
([ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)). No
sequence of calls can observe a notice that is never sent.

## Watching

**[CFG-W1] A subscription reports the answer when it differs, from whichever
process wrote it, and names its mechanism.** A platform that must be asked on a
timer says so rather than hiding it in a latency. Held over wall time and across
processes by `abstraction-config/go/watch_test.go` and
`abstraction-config/python/test_abstraction_config.py`; a transcript compared byte for byte
carries neither, so the corpus reaches the comparison half only, as [CFG-W2].

Every ancestor splits the two paths and none of them promises what changed.
`RegNotifyChangeKeyValue` "detects a single change", says nothing about what
changed, and must be re-armed
([winreg](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-regnotifychangekeyvalue));
Apple's `didChangeNotification` is "posted when the current process changes the
value" and is not generated when a different process changes the settings, while
KVO "reports all updates to setting values, regardless of which process made the
change"
([didChangeNotification](https://developer.apple.com/documentation/foundation/userdefaults/didchangenotification));
Consul's blocking queries return with "no guarantee of a change", so the client
compares
([Consul blocking](https://developer.hashicorp.com/consul/api-docs/features/blocking)).

**[CFG-W2] The stamp follows the answer, not the rung that gave it.** The stamp
differs whenever the answer differs and is unchanged when the same values arrive
from a different rung. A process running for days asks for the stamp rather than
rebuilding everything to find out whether it needs to, so a stamp that moved when
only provenance moved would make it rebuild for nothing. This is the comparison
[CFG-W1] leaves to the client, in the one form a transcript can hold.

## The schema

**[CFG-S1] A key the schema does not name is refused, not ignored.** Not built:
both implementations ignore an unknown key today. The schema is closed, and its
ancestor is GSettings, where "you have to specify a schema that describes the
keys in your settings and their types and default values". Its home is this
layer's one definition in the IDL profile, the same way `abstraction-job/job.thrift` is the
job layer's.

**[CFG-X1] No secret and no policy is a setting.** Nobody is refused a read and
nothing needs to be, because this layer carries pointers to tiers and nothing
else. A key that would need a read restriction is in the wrong layer. This is a
rule about what may be added to the schema rather than about any answer, so no
sequence of calls can detect it.

## Declared divergence: where the file is

We do not store in the registry, in plists or in dconf. One JSON file goes at the
platform's configuration directory — `%AppData%`, `~/Library/Application
Support`, `$XDG_CONFIG_HOME`
([os.UserConfigDir](https://pkg.go.dev/os#UserConfigDir)) — because three
languages already read that one file byte for byte, and no platform store is
readable from all three without a platform binding per language.

The seat is the POSIX kind: each platform furnishes scope, provenance and
notification its own way, and there is no common call an application can make on
all three.

## Backward compatibility

**Every key is optional and absent is an answer.** A file written before a key
existed loads unchanged, and the key reads as `default` with [CFG-P1]
provenance. Adding a key is therefore not a flag day.

**An incompatible change to a key's meaning is a new key, never an edit to this
one.** A reader too old to know a key ignores it today, which is [CFG-S1]'s
absence stated from the other side: until unknown keys are refused, an edited
meaning reaches an old reader as the old meaning and nothing says so.

**Provenance is not part of the stored document.** It is derived at load from
which rung answered, so it cannot go stale in a file and [CFG-W2] holds.

## Accounting

[testdata/scenarios/rules.tsv](testdata/scenarios/rules.tsv) is the accounting
for this page: every tag above with the scenarios that reach it, or the reason it
is unreached. Full accounting, never full coverage — a rule no sequence of driver
operations can detect says so there with its reason, and a rule nothing
implements yet says that.

`abstraction-config/go/corpus_test.go` and `abstraction-config/python/test_corpus.py` read the scenarios
beside that file and compare each language's transcript with the recorded
`.expected` byte for byte, so the two languages agree through it. The same tests
refuse a tag no rule names and a covered rule no expectation cites.

## Tested

```bash
(cd go && go test ./...)
(cd python && python -m unittest discover -p 'test_*.py')
```

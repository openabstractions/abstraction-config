"""Apply a config scenario and print what an observer saw.

conformance/DRIVER.md is the contract; abstraction-config/testdata/scenarios holds the
corpus, and the transcript this prints is compared with the Go driver's byte
for byte.

    driver.py --capabilities
    driver.py <workdir> <scenario>
"""

import contextlib
import io
import os
import sys

for _sibling in ("cas", "watch"):
    sys.path.insert(0, os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "..", "..", "abstraction-" + _sibling, "python"))
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import abstraction_config as config


def capabilities():
    """The machine rung is reachable only where this process can choose the
    machine directory. Windows reads it from ProgramData, which a driver can
    point into its own workdir; /etc cannot be moved and cannot be written by
    an ordinary user, so on those platforms the scenarios needing it are out of
    reach and say so rather than passing."""
    return ["config", "machine"] if sys.platform == "win32" else ["config"]


def settle(workdir):
    """Put every directory this layer looks in inside the workdir, and take
    away every variable a machine running the corpus might already have set, so
    a scenario decides the whole answer and the machine decides none of it."""
    home = os.path.join(workdir, "home")
    for name, value in (("HOME", home),
                        ("USERPROFILE", home),
                        ("AppData", os.path.join(workdir, "appdata")),
                        ("ProgramData", os.path.join(workdir, "programdata")),
                        ("XDG_CONFIG_HOME", os.path.join(workdir, "xdg"))):
        os.makedirs(value, exist_ok=True)
        os.environ[name] = value
    for name in ("HOMEDRIVE", "HOMEPATH") + tuple(config.ENV_VARS.values()):
        os.environ.pop(name, None)
    isolated(workdir)


def isolated(workdir):
    """A machine rung this driver cannot point into its own workdir is a rung
    the machine decides, and a corpus that read one would print whatever the
    machine running it happens to have. Where nothing is there the rung
    contributes nothing and the run is honest; where something is, the run
    stops and says so rather than recording a transcript nobody else can
    reproduce."""
    m = config.machine_path()
    if not m or m.startswith(workdir) or not os.path.exists(m):
        return
    sys.exit("%s is outside this workdir and exists, so the machine rung cannot "
             "be isolated and the corpus would read this machine's answer" % m)


class Driver:
    def __init__(self):
        self.noise = ""
        self.last = ""
        self.first = True

    def apply(self, line):
        op, _, rest = line.partition(" ")
        if op == "user":
            return self.plant(config.user_path(), rest)
        if op == "machine":
            return self.plant(config.machine_path(), rest)
        if op == "env":
            return self.env(rest)
        if op == "load":
            return self.load()
        if op == "key":
            return self.key(rest)
        if op == "stamp":
            return self.stamp()
        if op == "noise":
            return self.said()
        return "unknown-op"

    def plant(self, path, body):
        if not path:
            return "refused no-such-rung"
        if body == "-":
            with contextlib.suppress(FileNotFoundError):
                os.remove(path)
            return "ok"
        try:
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "wb") as f:
                f.write((body + "\n").encode("utf-8"))
        except OSError:
            return "refused unwritable"
        return "ok"

    def env(self, rest):
        key, sep, value = rest.partition(" ")
        name = config.ENV_VARS.get(key, "")
        if not sep or not name:
            return "invalid"
        if value == "-":
            os.environ.pop(name, None)
        else:
            os.environ[name] = "" if value == "empty" else value
        return "ok"

    def load(self):
        c = self.hearing(config.load)
        out = []
        for key in config.KEYS:
            o = c.origin(key)
            if o.rung == config.DEFAULT:
                continue
            out.append("%s=%s/%s" % (key, value(c, key), o.rung))
        return "ok " + " ".join(out) if out else "ok -"

    def key(self, name):
        if not name:
            return "invalid"
        c = self.hearing(config.load)
        return "ok value=%s from=%s" % (value(c, name), c.origin(name).rung)

    def stamp(self):
        c = self.hearing(config.load)
        s = c.stamp()
        was, first = self.last, self.first
        self.last, self.first = s, False
        if first:
            return "ok first"
        return "ok same" if s == was else "ok differs"

    def said(self):
        """Whether anything reached stderr since this was last asked. A file
        ignored silently and a file ignored loudly are the same transcript
        without this, and the difference is the whole of the rule."""
        if not self.noise:
            return "ok silent"
        self.noise = ""
        return "ok said"

    def hearing(self, f):
        """Run one call with stderr captured, so said() can report it without
        the text itself ever reaching a transcript two languages must agree
        on."""
        heard = io.StringIO()
        with contextlib.redirect_stderr(heard):
            c = f()
        self.noise += heard.getvalue()
        return c


def value(c, key):
    if key == "off":
        return ",".join(sorted(c.off))
    if key not in config.KEYS:
        return ""
    return getattr(c, key)


def main():
    if len(sys.argv) == 2 and sys.argv[1] == "--capabilities":
        sys.stdout.write(" ".join(capabilities()) + "\n")
        return
    if len(sys.argv) != 3:
        sys.exit("usage: driver.py <workdir> <scenario>")
    workdir, scenario = sys.argv[1], sys.argv[2]
    with open(scenario, "rb") as f:
        text = f.read().decode("utf-8").replace("\r\n", "\n")
    settle(workdir)
    d = Driver()
    n = 0
    for line in text.split("\n"):
        line = line.rstrip(" \t")
        if not line or line.startswith("#"):
            continue
        n += 1
        sys.stdout.write("%02d %s -> %s\n" % (n, line, d.apply(line)))


if __name__ == "__main__":
    # Windows text mode would turn every LF into CRLF, and this transcript is
    # compared with Go's byte for byte.
    sys.stdout.reconfigure(newline="\n", encoding="utf-8")
    main()

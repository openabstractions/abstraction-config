"""The Python half of the cross-language corpus.

abstraction-config/testdata/scenarios holds a scenario and the transcript an observer
should see. driver.py must print those bytes, and abstraction-config/go/corpus_test.go
demands the same of the Go driver, so the two languages agree through the file
rather than through a diff of two runs nobody recorded.
"""

import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCENARIOS = os.path.join(HERE, "..", "testdata", "scenarios")

for _sibling in ("cas", "watch"):
    sys.path.insert(0, os.path.join(HERE, "..", "..", "abstraction-" + _sibling, "python"))

import abstraction_config as config
import driver


def scenario_names():
    return sorted(n[:-4] for n in os.listdir(SCENARIOS) if n.endswith(".txt"))


def requires(body):
    for line in body.split("\n"):
        if line.strip().startswith("# requires:"):
            return line.split(":", 1)[1].split()
    return []


class Corpus(unittest.TestCase):
    def test_there_is_a_corpus_at_all(self):
        self.assertGreaterEqual(len(scenario_names()), 5,
                                "a corpus this small cannot fail on much")

    def test_every_transcript_is_what_is_recorded(self):
        declared = set(driver.capabilities())
        for name in scenario_names():
            with self.subTest(name):
                path = os.path.join(SCENARIOS, name + ".txt")
                with open(path, "rb") as f:
                    body = f.read().decode("utf-8")
                need = [c for c in requires(body) if c not in declared]
                if need:
                    # Out of reach is never a pass. unittest has no rung
                    # between the two either, so the line is said out loud.
                    sys.stderr.write(
                        "UNREACHABLE  config corpus %s -- this driver does not "
                        "declare %s on %s\n" % (name, " ".join(need), sys.platform))
                    continue
                with open(os.path.join(SCENARIOS, name + ".expected"), "rb") as f:
                    want = f.read()
                with tempfile.TemporaryDirectory() as workdir:
                    out = subprocess.run(
                        [sys.executable, os.path.join(HERE, "driver.py"), workdir, path],
                        capture_output=True, env=self.clean_env())
                self.assertEqual(0, out.returncode, out.stderr.decode("utf-8", "replace"))
                self.assertEqual(want, out.stdout,
                                 "%s: the transcript differs from the recorded one" % name)

    def clean_env(self):
        env = dict(os.environ)
        env["PYTHONDONTWRITEBYTECODE"] = "1"
        for var in config.ENV_VARS.values():
            env.pop(var, None)
        return env


if __name__ == "__main__":
    unittest.main()

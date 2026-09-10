import json
import os
import sys
import tempfile
import threading
import unittest

# cas and watch are siblings, not copies. Copying either would let the two
# drift, and this must run against the same file locking and the same
# subscription the rest of the tree uses.
for _sibling in ("cas", "watch"):
    sys.path.insert(0, os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "..", "..", _sibling, "python"))

import abstraction_config as config

BUDGET = 0.5


class Box(unittest.TestCase):
    """Every directory this layer looks in, inside a temporary one."""

    def setUp(self):
        self.box = tempfile.TemporaryDirectory()
        self.addCleanup(self.box.cleanup)
        root = self.box.name
        home = os.path.join(root, "home")
        for name, value in (("HOME", home),
                            ("USERPROFILE", home),
                            ("AppData", os.path.join(root, "appdata")),
                            ("ProgramData", os.path.join(root, "programdata")),
                            ("XDG_CONFIG_HOME", os.path.join(root, "xdg"))):
            os.makedirs(value, exist_ok=True)
            self.setenv(name, value)
        for name in ("HOMEDRIVE", "HOMEPATH"):
            self.setenv(name, None)
        for var in config.ENV_VARS.values():
            self.setenv(var, None)

    def setenv(self, name, value):
        was = os.environ.get(name)
        if value is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = value
        self.addCleanup(self._restore, name, was)

    @staticmethod
    def _restore(name, was):
        if was is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = was

    def write(self, path, obj):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            f.write((json.dumps(obj) if not isinstance(obj, str) else obj).encode("utf-8"))
        return path


class Paths(Box):
    def test_the_user_path_is_the_platforms_own_convention(self):
        p = config.user_path()
        self.assertTrue(p.endswith(os.path.join("abstraction", "config.json")), p)
        self.assertTrue(os.path.isabs(p), p)

    def test_the_machine_path_is_not_the_user_path(self):
        self.assertNotEqual(config.user_path(), config.machine_path())


class Provenance(Box):
    def test_a_key_nothing_set_came_from_the_default(self):
        c = config.load()
        self.assertEqual(config.Origin(config.DEFAULT, ""), c.origin("store"))

    def test_provenance_names_the_file_that_answered(self):
        user = self.write(config.user_path(), {"store": "A", "log_sink": "L"})
        c = config.load()
        self.assertEqual(config.Origin(config.USER, user), c.origin("store"))
        self.assertEqual(config.Origin(config.USER, user), c.origin("log_sink"))

    def test_one_variable_does_not_move_every_keys_provenance(self):
        user = self.write(config.user_path(), {"store": "A", "log_sink": "L"})
        self.setenv(config.ENV_VARS["store"], "B")
        c = config.load()
        self.assertEqual(("B", config.ENVIRONMENT), (c.store, c.origin("store").rung))
        self.assertEqual(config.Origin(config.USER, user), c.origin("log_sink"))

    def test_job_store_says_what_answered(self):
        user = self.write(config.user_path(), {"store": "A"})
        root, came_from = config.job_store()
        self.assertEqual("A", root)
        self.assertIn(user, came_from)

    def test_job_store_falls_back_to_the_default(self):
        root, came_from = config.job_store()
        self.assertTrue(root.endswith(".abstraction"), root)
        self.assertIn("default", came_from)


class Trust(Box):
    def test_a_machine_file_nobody_privileged_wrote_is_ignored(self):
        # Planted inside the box and never at machine_path(), because on Unix
        # that path is /etc: a test that wrote there would leave a machine-wide
        # file behind and would decide the answer for every other process.
        planted = self.write(os.path.join(self.box.name, "planted", "config.json"),
                             {"store": "planted"})
        try:
            config.trusted(planted)
        except config.Untrusted:
            pass
        else:
            self.skipTest("this process's files pass the ownership test, so it "
                          "is privileged and cannot plant anything")
        if sys.platform != "win32":
            return
        self.assertNotEqual("planted", config.load().store)
        self.assertTrue(os.path.exists(planted), "the file was removed rather than ignored")

    def test_the_machine_rung_is_where_the_platform_puts_it(self):
        if sys.platform == "win32":
            self.assertTrue(config.machine_path().startswith(os.environ["ProgramData"]))
        else:
            self.assertTrue(config.machine_path().startswith("/etc"))

    def test_a_malformed_file_is_ignored_and_not_silently(self):
        self.write(config.user_path(), "this is not json")
        import contextlib
        import io
        heard = io.StringIO()
        with contextlib.redirect_stderr(heard):
            c = config.load()
        self.assertEqual("", c.store)
        self.assertIn("ignoring", heard.getvalue())


class Watch(Box):
    """The rule the corpus cannot carry: a subscription is judged over wall
    time, and it names the mechanism telling it."""

    def test_it_names_its_mechanism(self):
        s = config.watch(BUDGET)
        self.addCleanup(s.close)
        self.assertTrue(s.how(), "a subscription that will not say how it is told")

    def test_a_write_by_anything_at_all_is_reported(self):
        os.makedirs(os.path.dirname(config.user_path()), exist_ok=True)
        s = config.watch(BUDGET)
        self.addCleanup(s.close)
        s.next(timeout=5)
        done = threading.Event()

        def edit():
            self.write(config.user_path(), {"store": "A"})
            done.set()

        threading.Timer(0.0, edit).start()
        for _ in range(20):
            n = s.next(timeout=5)
            if not n.quiet and n.now.store == "A":
                self.assertEqual(config.USER, n.now.origin("store").rung)
                return
        self.fail("the write was never reported; told by %s" % s.how())

    def test_quiet_arrives_when_nothing_moves(self):
        os.makedirs(os.path.dirname(config.user_path()), exist_ok=True)
        s = config.watch(BUDGET)
        self.addCleanup(s.close)
        s.next(timeout=5)
        self.assertTrue(s.next(timeout=5).quiet)


class Stamp(Box):
    def test_the_stamp_follows_the_answer_and_not_the_rung(self):
        self.write(config.user_path(), {"store": "A"})
        was = config.stamp()
        self.setenv(config.ENV_VARS["store"], "A")
        self.assertEqual(was, config.stamp())
        self.setenv(config.ENV_VARS["store"], "B")
        self.assertNotEqual(was, config.stamp())

    def test_an_empty_machine_has_an_empty_stamp(self):
        self.assertEqual("{}", config.stamp())


if __name__ == "__main__":
    unittest.main()

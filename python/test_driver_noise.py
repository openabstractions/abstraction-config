import os
import sys
import unittest
import warnings

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import driver  # noqa: E402


class DriverNoise(unittest.TestCase):
    def test_silent_load_is_not_noise(self):
        d = driver.Driver()
        d.hearing(driver.config.legacy_load)
        self.assertEqual("ok silent", d.said())

    def test_provider_warnings_and_stderr_remain_noise(self):
        d = driver.Driver()

        def noisy():
            warnings.warn("provider diagnostic", UserWarning)
            return driver.config.legacy_load()

        d.hearing(noisy)
        self.assertEqual("ok said", d.said())


if __name__ == "__main__":
    unittest.main()

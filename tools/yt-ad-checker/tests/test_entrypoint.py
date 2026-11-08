"""The entrypoint must replace a display left behind by a previous run."""

import os
import shutil
import signal
import subprocess
import unittest
from pathlib import Path

ENTRYPOINT = "/usr/local/bin/docker-entrypoint.sh"
# Not 99: the checker's own container may already own that display.
DISPLAY_NUM = 97


@unittest.skipUnless(
    os.access(ENTRYPOINT, os.X_OK) and shutil.which("Xvfb"),
    "needs the image entrypoint and Xvfb",
)
class StaleDisplayTest(unittest.TestCase):
    """`docker stop` kills Xvfb without cleanup, and /tmp is not a volume.

    The next start therefore still finds its lock and socket: Xvfb refuses to
    run ("Server is already active") while the stale socket satisfies the
    readiness check, so the app attaches to a display that does not exist.
    """

    def setUp(self):
        self.lock = Path(f"/tmp/.X{DISPLAY_NUM}-lock")
        self.socket = Path(f"/tmp/.X11-unix/X{DISPLAY_NUM}")
        self.socket.parent.mkdir(parents=True, exist_ok=True)
        # A lock naming a live PID is the reported case; the test process is
        # the easiest live PID to guarantee.
        self.lock.write_text(f"{os.getpid():10d}\n")
        self.socket.write_text("")
        self.env = dict(os.environ, HEADLESS="false", DISPLAY_NUM=str(DISPLAY_NUM))
        self.env.pop("DISPLAY", None)
        self.started = None

    def tearDown(self):
        if self.started is not None:
            try:
                os.kill(self.started, signal.SIGTERM)
            except OSError:
                pass
        for path in (self.lock, self.socket):
            try:
                path.unlink()
            except OSError:
                pass

    def test_a_stale_lock_does_not_take_the_display_with_it(self):
        result = subprocess.run(
            [ENTRYPOINT, "sh", "-c", f"test -S {self.socket}"],
            env=self.env,
            capture_output=True,
            text=True,
            timeout=60,
        )

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(self.socket.is_socket(), "the stale file must be replaced")
        # The planted PID is gone: the lock now names the Xvfb that runs.
        self.started = int(self.lock.read_text().strip())
        self.assertNotEqual(self.started, os.getpid())
        self.assertEqual(Path(f"/proc/{self.started}/comm").read_text().strip(), "Xvfb")


if __name__ == "__main__":
    unittest.main()

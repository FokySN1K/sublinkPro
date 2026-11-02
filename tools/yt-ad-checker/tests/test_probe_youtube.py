"""Unit tests for the extra-video pass of the YouTube probe."""

import time
import unittest

from playwright.sync_api import Error as PlaywrightError

from app.probe import ADS, UNKNOWN, UNREACHABLE, YouTubeProbe, connection_error, video_id

from .test_tag_sync import settings

MAIN = "https://www.youtube.com/watch?v=main"
SECOND = "https://www.youtube.com/watch?v=second"
THIRD = "https://www.youtube.com/watch?v=third"
HOME = "https://www.youtube.com/"

CLEAN_STATE = {"status": "OK", "hasPlayer": True}
AD_STATE = {"status": "OK", "hasPlayer": True, "adPlacements": 2}
NO_PLAYER_STATE = {"status": "", "hasPlayer": False}


class FakePage:
    """Answers ``evaluate`` from a queue and records the visited URLs."""

    def __init__(self, states, fail=(), errors=None) -> None:
        self._states = list(states)
        self._fail = set(fail)
        self._errors = dict(errors or {})
        self.visited: list[str] = []

    def on(self, event, handler):
        return None

    def goto(self, url, wait_until=None, timeout=None):
        self.visited.append(url)
        if url in self._fail:
            raise PlaywrightError(self._errors.get(url, "navigation failed"))

    def wait_for_selector(self, selector, timeout=None):
        return None

    def evaluate(self, script):
        return self._states.pop(0)


class FakeContext:
    def __init__(self, page) -> None:
        self._page = page

    def new_page(self):
        return self._page

    def add_cookies(self, cookies):
        return None

    def close(self):
        return None


class FakeBrowser:
    def __init__(self, page) -> None:
        self._page = page

    def new_context(self, **kwargs):
        return FakeContext(self._page)


class ExtraVideosTest(unittest.TestCase):
    def make_probe(self, urls):
        return YouTubeProbe(settings(probe_urls=urls, probe_dwell=0.0))

    def run_extra(self, page, urls, budget=30.0):
        probe = self.make_probe(urls)
        started = time.monotonic()
        return probe._probe_extra_videos(page, started + budget, started, [])

    def test_clean_extra_videos_keep_the_verdict(self):
        page = FakePage([CLEAN_STATE, CLEAN_STATE])
        self.assertIsNone(self.run_extra(page, (MAIN, SECOND, THIRD)))
        self.assertEqual(page.visited, [SECOND, THIRD])

    def test_an_ad_on_an_extra_video_wins(self):
        page = FakePage([CLEAN_STATE, AD_STATE])
        result = self.run_extra(page, (MAIN, SECOND, THIRD))
        self.assertEqual(result.status, ADS)
        # The log line names the video that carried the ad.
        self.assertIn("third", result.signals)
        self.assertIn("adPlacements=2", result.signals)
        self.assertEqual(page.visited, [SECOND, THIRD])

    def test_a_single_video_is_not_probed_twice(self):
        page = FakePage([])
        self.assertIsNone(self.run_extra(page, (MAIN,)))
        self.assertEqual(page.visited, [])

    def test_a_failed_extra_video_is_skipped(self):
        page = FakePage([CLEAN_STATE], fail=(SECOND,))
        self.assertIsNone(self.run_extra(page, (MAIN, SECOND, THIRD)))
        self.assertEqual(page.visited, [SECOND, THIRD])

    def test_no_time_left_means_no_extra_navigation(self):
        page = FakePage([])
        self.assertIsNone(self.run_extra(page, (MAIN, SECOND), budget=-1.0))
        self.assertEqual(page.visited, [])

    def test_the_video_with_ads_moves_to_the_front(self):
        # The next node starts with the video that proved monetized, so the
        # full probe runs on it instead of on a demonetized one.
        probe = self.make_probe((MAIN, SECOND, THIRD))
        started = time.monotonic()
        probe._probe_extra_videos(FakePage([CLEAN_STATE, AD_STATE]), started + 30, started, [])
        self.assertEqual(probe._ordered_urls(), (THIRD, MAIN, SECOND))

    def test_the_order_is_kept_without_a_hit(self):
        probe = self.make_probe((MAIN, SECOND, THIRD))
        started = time.monotonic()
        probe._probe_extra_videos(FakePage([CLEAN_STATE, CLEAN_STATE]), started + 30, started, [])
        self.assertEqual(probe._ordered_urls(), (MAIN, SECOND, THIRD))

    def test_a_preferred_video_outside_the_list_is_ignored(self):
        probe = self.make_probe((MAIN, SECOND))
        probe._preferred = THIRD
        self.assertEqual(probe._ordered_urls(), (MAIN, SECOND))


class ConnectionErrorTest(unittest.TestCase):
    def test_chromium_codes_mean_a_dead_node(self):
        for code in (
            "ERR_CONNECTION_CLOSED",
            "ERR_CONNECTION_TIMED_OUT",
            "ERR_CONNECTION_REFUSED",
            "ERR_TUNNEL_CONNECTION_FAILED",
            "ERR_NAME_NOT_RESOLVED",
            "ERR_TIMED_OUT",
        ):
            with self.subTest(code=code):
                message = f"Page.goto: net::{code} at https://www.youtube.com/watch?v=x"
                self.assertEqual(connection_error(message), code)

    def test_timeouts_and_tls_errors_are_not_connection_failures(self):
        self.assertIsNone(connection_error("Page.goto: Timeout 45000ms exceeded."))
        self.assertIsNone(connection_error("net::ERR_CERT_AUTHORITY_INVALID at https://youtube.com"))
        self.assertIsNone(connection_error(None))


class DeadConnectionTest(unittest.TestCase):
    """A node that cannot connect is skipped instead of timing out."""

    def probe(self, page, urls=(MAIN, SECOND)):
        probe = YouTubeProbe(settings(probe_urls=urls, probe_dwell=0.0))
        probe._browser = FakeBrowser(page)
        return probe, probe.probe()

    def test_a_dropped_connection_stops_the_probe(self):
        page = FakePage(
            [CLEAN_STATE],
            fail=(MAIN,),
            errors={MAIN: "Page.goto: net::ERR_CONNECTION_CLOSED at " + MAIN},
        )
        _probe, result = self.probe(page)
        self.assertEqual(result.status, UNREACHABLE)
        self.assertIn("ERR_CONNECTION_CLOSED", result.detail)
        # Neither the player wait nor the extra videos ran.
        self.assertEqual(page.visited, [HOME, MAIN])
        self.assertEqual(page._states, [CLEAN_STATE])

    def test_a_page_without_a_player_is_not_sampled(self):
        page = FakePage([NO_PLAYER_STATE, NO_PLAYER_STATE])
        _probe, result = self.probe(page)
        self.assertEqual(result.status, UNKNOWN)
        self.assertEqual(result.detail, "player did not load")
        self.assertEqual(page.visited, [HOME, MAIN])

    def test_a_dead_extra_video_stops_the_extra_pass(self):
        page = FakePage(
            [CLEAN_STATE],
            fail=(SECOND,),
            errors={SECOND: "Page.goto: net::ERR_CONNECTION_RESET at " + SECOND},
        )
        probe = YouTubeProbe(settings(probe_urls=(MAIN, SECOND, THIRD), probe_dwell=0.0))
        started = time.monotonic()
        self.assertIsNone(probe._probe_extra_videos(page, started + 30, started, []))
        self.assertEqual(page.visited, [SECOND])


class VideoIdTest(unittest.TestCase):
    def test_watch_url_yields_the_id(self):
        self.assertEqual(video_id("https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=5"), "dQw4w9WgXcQ")

    def test_other_urls_are_kept(self):
        self.assertEqual(video_id("https://youtu.be/abc"), "https://youtu.be/abc")


if __name__ == "__main__":
    unittest.main()

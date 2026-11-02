"""Unit tests for the Rutube play-options probe."""

import unittest

import requests

from app.probe import ADS, CLEAN, UNKNOWN, UNREACHABLE
from app.probe_rutube import CONNECT_TIMEOUT, READ_TIMEOUT, RutubeProbe, ad_slots

from .test_tag_sync import settings

MONETIZED = {"advert": [{"name": "preroll201", "count": 1}], "video_balancer": {"m3u8": "x"}, "duration": 100}
AD_FREE = {"advert": [], "video_balancer": {"m3u8": "x"}, "duration": 100}
UNAVAILABLE = {"detail": "not found"}


class FakeResponse:
    def __init__(self, payload, status=200) -> None:
        self.status_code = status
        self._payload = payload

    def json(self):
        return self._payload


class FakeSession:
    """Serves the queued responses and records every request."""

    def __init__(self, *responses) -> None:
        self._responses = list(responses)
        self.calls: list[tuple[str, tuple[float, float]]] = []

    def get(self, url, timeout=None):
        self.calls.append((url, timeout))
        item = self._responses.pop(0)
        if isinstance(item, BaseException):
            raise item
        return item


def probe_with(*responses, videos=("v1", "v2", "v3"), timeout=45.0) -> tuple[RutubeProbe, FakeSession]:
    probe = RutubeProbe(settings(rutube_videos=videos, probe_timeout=timeout, service="rutube"))
    session = FakeSession(*responses)
    probe._session = session
    return probe, session


class AdSlotsTest(unittest.TestCase):
    def test_slots_are_reported(self):
        cases = [
            ({"advert": [{"name": "preroll201", "count": 1}]}, ["preroll201"]),
            ({"advert": [{"name": "midroll70101", "count": 2}]}, ["midroll70101"]),
            ({"advert": [{"name": "preroll201", "count": 0}]}, []),
            ({"advert": [], "cuepoints": [{"time": 30}]}, ["cuepoints"]),
            ({"advert": [{"name": "preroll201"}, {"name": "preroll202", "count": 1}]}, ["preroll202"]),
            ({"advert": []}, []),
            ({}, []),
        ]
        for payload, expected in cases:
            with self.subTest(payload=payload):
                self.assertEqual(ad_slots(payload), expected)


class RutubeProbeTest(unittest.TestCase):
    def test_first_monetized_video_wins(self):
        probe, session = probe_with(FakeResponse(MONETIZED))
        result = probe.probe()
        self.assertEqual(result.status, ADS)
        self.assertEqual(result.signals, ("preroll201",))
        self.assertEqual(len(session.calls), 1)

    def test_clean_needs_two_answers(self):
        # Every video is tried even after the minimum is reached: a later video
        # may still be monetized, and a missing answer must not hide that.
        probe, session = probe_with(FakeResponse(AD_FREE), FakeResponse(AD_FREE), FakeResponse(AD_FREE))
        result = probe.probe()
        self.assertEqual(result.status, CLEAN)
        self.assertEqual(len(session.calls), 3)

    def test_single_answer_is_unknown(self):
        probe, _session = probe_with(FakeResponse(AD_FREE), FakeResponse(UNAVAILABLE), videos=("v1", "v2"))
        self.assertEqual(probe.probe().status, UNKNOWN)

    def test_unavailable_video_is_not_an_answer(self):
        probe, _session = probe_with(FakeResponse(UNAVAILABLE), FakeResponse(UNAVAILABLE), videos=("v1", "v2"))
        self.assertEqual(probe.probe().status, UNREACHABLE)

    def test_http_error_is_not_an_answer(self):
        probe, _session = probe_with(
            FakeResponse(UNAVAILABLE, status=503),
            FakeResponse(UNAVAILABLE, status=503),
            videos=("v1", "v2"),
        )
        self.assertEqual(probe.probe().status, UNREACHABLE)

    def test_first_network_error_aborts_the_probe(self):
        probe, session = probe_with(requests.ConnectionError("node is down"))
        result = probe.probe()
        self.assertEqual(result.status, UNREACHABLE)
        self.assertIn("node is down", result.detail)
        self.assertEqual(len(session.calls), 1)

    def test_network_error_after_an_answer_still_probes(self):
        probe, session = probe_with(
            FakeResponse(AD_FREE),
            requests.ReadTimeout("slow"),
            FakeResponse(AD_FREE),
        )
        self.assertEqual(probe.probe().status, CLEAN)
        self.assertEqual(len(session.calls), 3)

    def test_request_timeout_is_capped(self):
        probe, session = probe_with(FakeResponse(AD_FREE), FakeResponse(AD_FREE), timeout=45.0)
        probe.probe()
        self.assertEqual(session.calls[0][1], (CONNECT_TIMEOUT, READ_TIMEOUT))

    def test_request_timeout_follows_a_small_budget(self):
        probe, session = probe_with(FakeResponse(AD_FREE), FakeResponse(AD_FREE), timeout=4.0)
        probe.probe()
        connect, read = session.calls[0][1]
        self.assertEqual(connect, read)
        self.assertAlmostEqual(read, 4.0, places=2)

    def test_url_uses_the_public_play_options_endpoint(self):
        probe, session = probe_with(FakeResponse(MONETIZED))
        probe.probe()
        self.assertEqual(session.calls[0][0], "https://rutube.ru/api/play/options/v1/?no_404=true")


if __name__ == "__main__":
    unittest.main()

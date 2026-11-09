"""Environment parsing for the ip-checker."""

from __future__ import annotations

import os
import unittest
from unittest import mock

from app.config import Settings, parse_interval, parse_sources
from tests.helpers import make_settings


class ParseIntervalTest(unittest.TestCase):
    def test_seconds_minutes_hours_and_days(self):
        self.assertEqual(parse_interval("90"), 90)
        self.assertEqual(parse_interval("15m"), 900)
        self.assertEqual(parse_interval("24h"), 86400)
        self.assertEqual(parse_interval("1d"), 86400)
        self.assertEqual(parse_interval("1h30m"), 5400)

    def test_nonsense_is_rejected(self):
        for value in ("", "0", "-5", "abc", "15x", "1h 30m x"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    parse_interval(value)


class ParseSourcesTest(unittest.TestCase):
    def test_an_empty_value_means_both_keyless_sources(self):
        self.assertEqual(parse_sources(""), ("iplocate", "ipapi"))

    def test_aliases_are_resolved(self):
        self.assertEqual(parse_sources("ip-api, IPQS"), ("ipapi", "ipqualityscore"))

    def test_duplicates_are_collapsed(self):
        self.assertEqual(parse_sources("ipapi ip-api"), ("ipapi",))

    def test_an_unknown_name_is_rejected(self):
        with self.assertRaises(ValueError):
            parse_sources("maxmind")


class SettingsTest(unittest.TestCase):
    def test_the_api_key_is_required(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(ValueError):
                Settings.from_env()

    def test_defaults_target_the_keyless_sources(self):
        settings = make_settings()
        self.assertEqual(settings.sources, ("iplocate", "ipapi"))
        self.assertEqual(settings.clean_tag, "clean-ip")
        self.assertEqual(settings.interval, 86400)
        self.assertEqual(settings.require_clean, 1)
        self.assertEqual(settings.exit_ip_url, "https://api.ipify.org?format=json")
        self.assertEqual(settings.workers, 1)
        self.assertFalse(settings.dry_run)

    def test_keys_are_exposed_per_source(self):
        settings = make_settings(
            IPLOCATE_API_KEY="a",
            PROXYCHECK_KEY="b",
            IPQUALITYSCORE_KEY="c",
            VPNAPI_KEY="d",
        )
        self.assertEqual(
            settings.source_api_keys,
            {"iplocate": "a", "proxycheck": "b", "ipqualityscore": "c", "vpnapi": "d"},
        )

    def test_unset_keys_are_empty(self):
        self.assertEqual(make_settings().source_api_keys["vpnapi"], "")

    def test_the_filter_and_tag_knobs_are_read(self):
        settings = make_settings(
            ONLY_TAGS="airport-a airport-b",
            SKIP_TAGS="residential",
            MAX_NODES="25",
            CLEAN_TAG="clean",
            REQUIRE_CLEAN="2",
            CHECK_INTERVAL="12h",
        )
        self.assertEqual(settings.only_tags, ("airport-a", "airport-b"))
        self.assertEqual(settings.skip_tags, ("residential",))
        self.assertEqual(settings.max_nodes, 25)
        self.assertEqual(settings.clean_tag, "clean")
        self.assertEqual(settings.require_clean, 2)
        self.assertEqual(settings.interval, 43200)

    def test_invalid_numbers_are_rejected(self):
        for env in (
            {"WORKERS": "0"},
            {"WORKERS": "9"},
            {"MAX_NODES": "-1"},
            {"REQUIRE_CLEAN": "0"},
            {"PROBE_TIMEOUT": "0"},
        ):
            with self.subTest(env=env):
                with self.assertRaises(ValueError):
                    make_settings(**env)


if __name__ == "__main__":
    unittest.main()

"""Environment parsing, tag naming and interval helpers."""

from __future__ import annotations

import os
import unittest
from unittest import mock

from app.config import MAX_WORKERS, Settings, parse_interval, parse_services
from app.providers import PROVIDER_KEYS


class IntervalTest(unittest.TestCase):
    def test_plain_seconds(self):
        self.assertEqual(parse_interval("90"), 90)

    def test_units_and_compound_values(self):
        self.assertEqual(parse_interval("6h"), 6 * 3600)
        self.assertEqual(parse_interval("1h30m"), 90 * 60)
        self.assertEqual(parse_interval("1d"), 86400)

    def test_garbage_is_rejected(self):
        for value in ("", "6 hours", "h", "0", "-5m"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    parse_interval(value)


class ServicesTest(unittest.TestCase):
    def test_an_empty_list_means_every_provider(self):
        self.assertEqual(parse_services(""), PROVIDER_KEYS)

    def test_aliases_are_resolved_to_canonical_keys(self):
        self.assertEqual(parse_services("claude, openai"), ("claude", "chatgpt"))

    def test_unknown_service_is_rejected(self):
        with self.assertRaises(ValueError):
            parse_services("gpt-9")


def make_settings(**overrides) -> Settings:
    base = dict(
        sublink_url="http://127.0.0.1:8000",
        sublink_api_key="key",
        services=("chatgpt", "claude"),
        tag_prefix="ai-",
        interval=21600.0,
        run_on_start=True,
        once=False,
        only_tags=(),
        skip_tags=(),
        max_nodes=0,
        workers=1,
        ensure_tag=True,
        dry_run=False,
        probe_timeout=15.0,
        user_agent="test-agent",
        reachability_url="https://www.gstatic.com/generate_204",
        state_dir="/data",
        mihomo_bin="mihomo",
        mihomo_api="http://127.0.0.1:19090",
        mihomo_proxy_port=17890,
        log_level="INFO",
    )
    base.update(overrides)
    return Settings(**base)


class SettingsFromEnvTest(unittest.TestCase):
    def test_defaults_cover_every_service(self):
        with mock.patch.dict(os.environ, {"SUBLINK_API_KEY": "key"}, clear=True):
            settings = Settings.from_env()
        self.assertEqual(settings.services, PROVIDER_KEYS)
        self.assertEqual(settings.tag_prefix, "ai-")
        self.assertEqual(settings.interval, 6 * 3600)
        self.assertEqual(settings.workers, 1)
        self.assertEqual(settings.probe_timeout, 15.0)
        self.assertEqual(settings.available_tag("chatgpt"), "ai-chatgpt")
        self.assertEqual(settings.blocked_tag("chatgpt"), "ai-chatgpt-blocked")

    def test_the_api_key_is_required(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(ValueError):
                Settings.from_env()

    def test_worker_limit_is_enforced(self):
        with mock.patch.dict(
            os.environ, {"SUBLINK_API_KEY": "key", "WORKERS": str(MAX_WORKERS + 1)}, clear=True
        ):
            with self.assertRaises(ValueError):
                Settings.from_env()


if __name__ == "__main__":
    unittest.main()

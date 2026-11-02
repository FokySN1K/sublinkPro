"""Unit tests for choosing the YouTube probe videos."""

import os
import unittest
from unittest.mock import patch

from app.config import DEFAULT_YOUTUBE_VIDEOS, Settings, watch_url

API_KEY = {"SUBLINK_API_KEY": "key"}


def build(**env) -> Settings:
    with patch.dict(os.environ, {**API_KEY, **env}, clear=True):
        return Settings.from_env()


class WatchUrlTest(unittest.TestCase):
    def test_bare_id_becomes_a_watch_url(self):
        self.assertEqual(watch_url("dQw4w9WgXcQ"), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")

    def test_watch_prefix_gets_the_site(self):
        self.assertEqual(watch_url("watch?v=abc"), "https://www.youtube.com/watch?v=abc")

    def test_full_url_is_kept(self):
        for url in ("https://youtu.be/abc", "https://www.youtube.com/watch?v=abc&t=10"):
            with self.subTest(url=url):
                self.assertEqual(watch_url(url), url)

    def test_url_without_a_host_is_rejected(self):
        with self.assertRaises(ValueError):
            watch_url("https://")

    def test_empty_entry_is_rejected(self):
        with self.assertRaises(ValueError):
            watch_url("   ")


class ProbeVideosTest(unittest.TestCase):
    def test_built_in_list_is_used_by_default(self):
        settings = build()
        self.assertEqual(settings.probe_urls, DEFAULT_YOUTUBE_VIDEOS)
        self.assertEqual(settings.probe_url, DEFAULT_YOUTUBE_VIDEOS[0])
        self.assertGreater(len(settings.probe_urls), 1)

    def test_list_takes_ids_and_urls(self):
        settings = build(YOUTUBE_VIDEOS="abc, https://www.youtube.com/watch?v=def")
        self.assertEqual(
            settings.probe_urls,
            ("https://www.youtube.com/watch?v=abc", "https://www.youtube.com/watch?v=def"),
        )

    def test_single_probe_url_still_works(self):
        settings = build(PROBE_URL="abc")
        self.assertEqual(settings.probe_urls, ("https://www.youtube.com/watch?v=abc",))

    def test_list_wins_over_the_single_url(self):
        settings = build(YOUTUBE_VIDEOS="list", PROBE_URL="legacy")
        self.assertEqual(settings.probe_urls, ("https://www.youtube.com/watch?v=list",))

    def test_empty_list_falls_back_to_the_single_url(self):
        settings = build(YOUTUBE_VIDEOS="", PROBE_URL="legacy")
        self.assertEqual(settings.probe_urls, ("https://www.youtube.com/watch?v=legacy",))


if __name__ == "__main__":
    unittest.main()

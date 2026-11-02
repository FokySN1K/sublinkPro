"""Unit tests for tag synchronisation between the verdict and SublinkPro."""

import unittest

from app.config import Settings
from app.main import apply_tag
from app.probe import ADS, CLEAN, UNKNOWN, UNREACHABLE, ProbeResult
from app.sublink import NodeRef

NODE_ID = 7


def settings(**overrides) -> Settings:
    base = dict(
        sublink_url="http://sublink.invalid",
        sublink_api_key="key",
        service="youtube",
        clean_tag="youtube",
        ads_tag="youtube-ads",
        rutube_videos=("v1",),
        interval=3600.0,
        run_on_start=False,
        once=False,
        only_tags=(),
        skip_tags=(),
        max_nodes=0,
        workers=1,
        ensure_tag=True,
        dry_run=False,
        probe_urls=("https://www.youtube.com/watch?v=x",),
        probe_timeout=45.0,
        probe_dwell=0.0,
        headless=False,
        state_dir="/tmp",
        mihomo_bin="mihomo",
        mihomo_api="http://127.0.0.1:1",
        mihomo_proxy_port=1,
        log_level="INFO",
    )
    base.update(overrides)
    return Settings(**base)


def node(*tags: str) -> NodeRef:
    return NodeRef(id=NODE_ID, name="node", group="", protocol="ss", tags=tags, links=("ss://x",))


class FakeClient:
    def __init__(self) -> None:
        self.calls: list[tuple[str, list[int], str]] = []

    def add_tag(self, node_ids, tag):
        self.calls.append(("add", list(node_ids), tag))

    def remove_tag(self, node_ids, tag):
        self.calls.append(("remove", list(node_ids), tag))


class ApplyTagTest(unittest.TestCase):
    def sync(self, status, tags=(), **overrides):
        client = FakeClient()
        result = apply_tag(settings(**overrides), client, node(*tags), ProbeResult(status, "test"))
        return result, client.calls

    def test_clean_adds_clean_tag(self):
        sync, calls = self.sync(CLEAN)
        self.assertEqual(calls, [("add", [NODE_ID], "youtube")])
        self.assertEqual((sync.added, sync.removed), (1, 0))
        self.assertEqual(sync.note, "+youtube")

    def test_clean_replaces_ads_tag(self):
        _sync, calls = self.sync(CLEAN, ("youtube-ads",))
        self.assertEqual(calls, [("add", [NODE_ID], "youtube"), ("remove", [NODE_ID], "youtube-ads")])

    def test_clean_is_idempotent(self):
        sync, calls = self.sync(CLEAN, ("youtube",))
        self.assertEqual(calls, [])
        self.assertEqual(sync.note, "")

    def test_ads_replaces_clean_tag(self):
        _sync, calls = self.sync(ADS, ("youtube",))
        self.assertEqual(calls, [("add", [NODE_ID], "youtube-ads"), ("remove", [NODE_ID], "youtube")])

    def test_unreachable_and_unknown_keep_tags(self):
        for status in (UNREACHABLE, UNKNOWN):
            with self.subTest(status=status):
                _sync, calls = self.sync(status, ("youtube-ads",))
                self.assertEqual(calls, [])

    def test_dry_run_only_reports(self):
        sync, calls = self.sync(ADS, ("youtube",), dry_run=True)
        self.assertEqual(calls, [])
        self.assertEqual(sync.note, "would add youtube-ads, remove youtube")
        self.assertEqual((sync.added, sync.removed), (1, 1))

    def test_rutube_tag_pair_is_used(self):
        _sync, calls = self.sync(
            ADS,
            ("rutube",),
            service="rutube",
            clean_tag="rutube",
            ads_tag="rutube-ads",
        )
        self.assertEqual(calls, [("add", [NODE_ID], "rutube-ads"), ("remove", [NODE_ID], "rutube")])


if __name__ == "__main__":
    unittest.main()

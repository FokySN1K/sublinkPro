"""Tag decisions, node selection and the per-node merge."""

from __future__ import annotations

import unittest

from app.main import (
    LinkCheck,
    Target,
    apply_tags,
    build_targets,
    build_worker_settings,
    combine,
    describe,
    desired_tags,
    probe_sources,
    select_nodes,
)
from app.sources import CLEAN, FLAGGED, UNKNOWN, Source, Verdict
from app.sublink import NodeRef
from tests.helpers import make_settings

GOOD = "ss://YWVzOnB3@example.test:443#old"


def node(node_id: int = 7, tags: tuple[str, ...] = (), links: tuple[str, ...] = (GOOD,)) -> NodeRef:
    return NodeRef(
        id=node_id,
        name=f"node-{node_id}",
        group="airport",
        protocol="ss",
        tags=tags,
        links=links,
    )


class FakeClient:
    def __init__(self) -> None:
        self.added: list[tuple[tuple[int, ...], str]] = []
        self.removed: list[tuple[tuple[int, ...], str]] = []

    def add_tag(self, node_ids, tag: str) -> None:
        self.added.append((tuple(node_ids), tag))

    def remove_tag(self, node_ids, tag: str) -> None:
        self.removed.append((tuple(node_ids), tag))


class FixedSource(Source):
    """A source that returns a canned verdict without touching the network."""

    def __init__(self, key: str, verdict: Verdict) -> None:
        super().__init__()
        self.key = key
        self._verdict = verdict

    def check(self, ctx, ip: str) -> Verdict:  # noqa: ARG002 - canned answer
        return self._verdict


class SelectNodesTest(unittest.TestCase):
    def test_no_filters_keeps_everything(self):
        nodes = [node(1), node(2)]
        self.assertEqual(select_nodes(nodes, (), (), 0), nodes)

    def test_only_tags_keeps_a_match_regardless_of_case(self):
        nodes = [node(1, tags=("Airport-A",)), node(2, tags=("other",))]
        self.assertEqual([item.id for item in select_nodes(nodes, ("airport-a",), (), 0)], [1])

    def test_skip_tags_wins_over_only_tags(self):
        nodes = [node(1, tags=("airport-a", "residential"))]
        self.assertEqual(select_nodes(nodes, ("airport-a",), ("residential",), 0), [])

    def test_max_nodes_caps_the_rest(self):
        nodes = [node(index) for index in range(1, 6)]
        self.assertEqual([item.id for item in select_nodes(nodes, (), (), 2)], [1, 2])


class BuildTargetsTest(unittest.TestCase):
    def test_several_links_get_unique_proxy_names(self):
        targets, proxies, skipped = build_targets([node(7, links=(GOOD, GOOD))])
        self.assertEqual(targets[0].names, ["n7", "n7_2"])
        self.assertEqual([name for name, _link in proxies], ["n7", "n7_2"])
        self.assertTrue(proxies[0][1].endswith("#n7"))
        self.assertEqual(skipped, [])

    def test_an_unusable_link_is_reported_once(self):
        targets, _proxies, skipped = build_targets([node(3, links=("vmess://bm90LWpzb24",))])
        self.assertEqual(targets, [])
        self.assertEqual(len(skipped), 1)
        self.assertIn("node #3", skipped[0])

    def test_the_good_link_of_a_node_still_runs(self):
        targets, _proxies, skipped = build_targets(
            [node(4, links=("vmess://bm90LWpzb24", GOOD))]
        )
        self.assertEqual(targets[0].names, ["n4_2"])
        self.assertEqual(len(skipped), 1)


class WorkerLayoutTest(unittest.TestCase):
    def test_a_single_worker_keeps_the_configured_ports(self):
        settings = make_settings(WORKERS="1")
        self.assertEqual(build_worker_settings(settings, 0).mihomo_proxy_port, 17890)

    def test_every_worker_shifts_ports_and_directories(self):
        settings = make_settings(WORKERS="2")
        second = build_worker_settings(settings, 1)
        self.assertEqual(second.mihomo_proxy_port, 17891)
        self.assertEqual(second.mihomo_api, "http://127.0.0.1:19091")
        self.assertTrue(second.state_dir.endswith("worker-2"))


class CombineTest(unittest.TestCase):
    def test_nothing_checked_is_unknown(self):
        self.assertEqual(combine([]), (UNKNOWN, "no link checked"))

    def test_every_link_clean_means_clean(self):
        checks = [LinkCheck(name="n7", ip="1.1.1.1", status=CLEAN), LinkCheck(name="n7_2", ip="2.2.2.2", status=CLEAN)]
        self.assertEqual(combine(checks), (CLEAN, ""))

    def test_one_flagged_link_withholds_the_tag(self):
        checks = [
            LinkCheck(name="n7", ip="1.1.1.1", status=CLEAN),
            LinkCheck(name="n7_2", ip="2.2.2.2", status=FLAGGED, reason="ipapi: hosting"),
        ]
        status, reason = combine(checks)
        self.assertEqual(status, FLAGGED)
        self.assertIn("n7_2 (2.2.2.2): ipapi: hosting", reason)

    def test_an_unchecked_link_keeps_the_node_unknown(self):
        checks = [
            LinkCheck(name="n7", ip="1.1.1.1", status=CLEAN),
            LinkCheck(name="n7_2", status=UNKNOWN, reason="link unreachable: timeout"),
        ]
        status, reason = combine(checks)
        self.assertEqual(status, UNKNOWN)
        self.assertIn("n7_2: link unreachable", reason)

    def test_describe_uses_the_address_for_a_single_link(self):
        self.assertEqual(describe([LinkCheck(ip="1.1.1.1", reason="x")]), "1.1.1.1: x")

    def test_describe_names_each_link_of_a_multi_link_node(self):
        checks = [
            LinkCheck(name="n7", ip="1.1.1.1", reason="a"),
            LinkCheck(name="n7_2", reason="b"),
        ]
        self.assertEqual(describe(checks, name_links=True), "n7 (1.1.1.1): a; n7_2: b")


class TagsTest(unittest.TestCase):
    def test_clean_wants_the_tag(self):
        settings = make_settings()
        self.assertEqual(desired_tags(settings, CLEAN), (["clean-ip"], []))

    def test_flagged_drops_the_tag(self):
        settings = make_settings()
        self.assertEqual(desired_tags(settings, FLAGGED), ([], ["clean-ip"]))

    def test_unknown_wants_nothing(self):
        settings = make_settings()
        self.assertEqual(desired_tags(settings, UNKNOWN), ([], []))

    def test_the_tag_is_written_once(self):
        settings = make_settings()
        client = FakeClient()
        sync = apply_tags(settings, client, node(tags=()), CLEAN)
        self.assertEqual((sync.added, sync.removed), (1, 0))
        self.assertEqual(client.added, [((7,), "clean-ip")])
        self.assertEqual(client.removed, [])

    def test_an_existing_tag_is_not_added_twice(self):
        settings = make_settings()
        client = FakeClient()
        sync = apply_tags(settings, client, node(tags=("clean-ip",)), CLEAN)
        self.assertEqual((sync.added, sync.removed), (0, 0))
        self.assertEqual(client.added, [])

    def test_flagged_removes_the_tag(self):
        settings = make_settings()
        client = FakeClient()
        sync = apply_tags(settings, client, node(tags=("clean-ip", "airport-a")), FLAGGED)
        self.assertEqual((sync.added, sync.removed), (0, 1))
        self.assertEqual(client.removed, [((7,), "clean-ip")])

    def test_unknown_leaves_an_existing_tag_alone(self):
        settings = make_settings()
        client = FakeClient()
        sync = apply_tags(settings, client, node(tags=("clean-ip",)), UNKNOWN)
        self.assertEqual((sync.added, sync.removed), (0, 0))
        self.assertEqual(client.added, [])
        self.assertEqual(client.removed, [])

    def test_dry_run_only_plans(self):
        settings = make_settings(DRY_RUN="true")
        client = FakeClient()
        sync = apply_tags(settings, client, node(tags=()), CLEAN)
        self.assertEqual(sync.note, "would add clean-ip")
        self.assertEqual(client.added, [])


class ProbeSourcesTest(unittest.TestCase):
    def test_every_source_is_asked_and_the_order_is_kept(self):
        settings = make_settings()
        sources = [
            FixedSource("iplocate", Verdict(CLEAN)),
            FixedSource("ipapi", Verdict(FLAGGED, "hosting")),
        ]
        verdicts = probe_sources(settings, sources, "91.77.162.158")
        self.assertEqual(list(verdicts), ["iplocate", "ipapi"])
        self.assertEqual(verdicts["ipapi"].status, FLAGGED)

    def test_no_sources_is_an_empty_result(self):
        self.assertEqual(probe_sources(make_settings(), [], "91.77.162.158"), {})


class TargetTest(unittest.TestCase):
    def test_the_dataclass_keeps_the_link_names(self):
        target = Target(node=node(), names=["n7", "n7_2"])
        self.assertEqual(target.names, ["n7", "n7_2"])


if __name__ == "__main__":
    unittest.main()

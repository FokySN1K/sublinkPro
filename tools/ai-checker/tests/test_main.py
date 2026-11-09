"""Node selection, worker layout and log rendering."""

from __future__ import annotations

import unittest

from app.main import (
    build_targets,
    build_worker_settings,
    render_detail,
    render_verdicts,
    select_nodes,
)
from app.providers import AVAILABLE, PARTIAL, RESTRICTED, UNKNOWN, Result
from app.sublink import NodeRef

from .test_config import make_settings


def node(node_id: int, tags: tuple[str, ...] = (), links: tuple[str, ...] = ("vless://x",)) -> NodeRef:
    return NodeRef(
        id=node_id,
        name=f"node-{node_id}",
        group="g",
        protocol="vless",
        tags=tags,
        links=links,
    )


class SelectNodesTest(unittest.TestCase):
    nodes = [node(1, ("keep",)), node(2, ("skip",)), node(3, ("Keep", "Skip")), node(4)]

    def test_without_filters_everything_is_kept(self):
        self.assertEqual(len(select_nodes(self.nodes, (), (), 0)), 4)

    def test_only_tags_keeps_matching_nodes_ignoring_case(self):
        kept = select_nodes(self.nodes, ("keep",), (), 0)
        self.assertEqual([n.id for n in kept], [1, 3])

    def test_skip_tags_wins_over_only_tags(self):
        kept = select_nodes(self.nodes, ("keep",), ("skip",), 0)
        self.assertEqual([n.id for n in kept], [1])

    def test_max_nodes_caps_the_result(self):
        self.assertEqual(len(select_nodes(self.nodes, (), (), 2)), 2)


class BuildTargetsTest(unittest.TestCase):
    def test_every_link_gets_a_unique_proxy_name(self):
        targets, proxies, skipped = build_targets([node(7, links=("vless://a#x", "vless://b#x"))])
        self.assertEqual(skipped, [])
        self.assertEqual(len(targets), 1)
        self.assertEqual([name for name, _ in proxies], ["n7", "n7_2"])
        self.assertEqual(proxies[0][1], "vless://a#n7")
        self.assertEqual(proxies[1][1], "vless://b#n7_2")

    def test_a_node_without_a_usable_link_is_reported(self):
        targets, proxies, skipped = build_targets(
            [node(8, links=("vmess://not-base64",)), node(9)]
        )
        self.assertEqual([t.node.id for t in targets], [9])
        self.assertEqual(len(proxies), 1)
        self.assertEqual(len(skipped), 1)
        self.assertIn("node #8", skipped[0])


class WorkerLayoutTest(unittest.TestCase):
    def test_a_single_worker_keeps_the_configured_ports(self):
        settings = make_settings()
        self.assertIs(build_worker_settings(settings, 0), settings)

    def test_each_worker_shifts_its_ports_and_state_dir(self):
        settings = make_settings(workers=2)
        first = build_worker_settings(settings, 0)
        second = build_worker_settings(settings, 1)
        self.assertEqual(first.mihomo_proxy_port, 17890)
        self.assertEqual(second.mihomo_proxy_port, 17891)
        self.assertEqual(second.mihomo_api, "http://127.0.0.1:19091")
        self.assertTrue(second.state_dir.endswith("worker-2"))
        self.assertTrue(first.state_dir.endswith("worker-1"))


class RenderingTest(unittest.TestCase):
    def test_one_symbol_per_service_in_the_configured_order(self):
        verdicts = {
            "chatgpt": Result(AVAILABLE),
            "claude": Result(RESTRICTED),
            "gemini": Result(UNKNOWN),
        }
        self.assertEqual(render_verdicts(verdicts), "chatgpt+ claude- gemini?")

    def test_partial_has_its_own_symbol(self):
        self.assertEqual(render_verdicts({"chatgpt": Result(PARTIAL)}), "chatgpt~")

    def test_the_region_hint_is_only_printed_for_common_blocks(self):
        verdicts = {"gemini": Result(RESTRICTED, "region_blocked", "RU")}
        self.assertIn("loc=RU", render_detail(verdicts))
        verdicts = {"gemini": Result(RESTRICTED, "region_blocked", "DE")}
        self.assertNotIn("loc=DE", render_detail(verdicts))

    def test_available_verdicts_are_not_repeated_in_the_detail(self):
        self.assertEqual(render_detail({"chatgpt": Result(AVAILABLE)}), "")


if __name__ == "__main__":
    unittest.main()

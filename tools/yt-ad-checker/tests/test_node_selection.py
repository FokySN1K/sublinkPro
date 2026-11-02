"""Node selection (ONLY_TAGS/SKIP_TAGS/MAX_NODES) and the worker port layout."""

import os
import unittest
from unittest.mock import patch

from app.config import Settings
from app.main import build_worker_settings, select_nodes
from app.sublink import NodeRef

from .test_tag_sync import settings


def node(node_id, *tags, links=("ss://x",)) -> NodeRef:
    return NodeRef(
        id=node_id, name=f"node{node_id}", group="", protocol="ss", tags=tags, links=links
    )


class SelectNodesTest(unittest.TestCase):
    def test_no_filters_keeps_everything(self):
        nodes = [node(1, "youtube"), node(2)]
        self.assertEqual(select_nodes(nodes, (), (), 0), nodes)

    def test_only_tags_keeps_matching_nodes(self):
        nodes = [node(1, "youtube"), node(2, "rutube"), node(3)]
        self.assertEqual([n.id for n in select_nodes(nodes, ("youtube",), (), 0)], [1])

    def test_only_tags_is_case_insensitive(self):
        self.assertEqual([n.id for n in select_nodes([node(1, "YouTube")], ("youtube",), (), 0)], [1])

    def test_only_tags_accepts_any_tag_of_the_list(self):
        nodes = [node(1, "a"), node(2, "b"), node(3, "c")]
        self.assertEqual([n.id for n in select_nodes(nodes, ("a", "b"), (), 0)], [1, 2])

    def test_skip_tags_wins_over_only_tags(self):
        nodes = [node(1, "youtube", "bad"), node(2, "youtube")]
        self.assertEqual([n.id for n in select_nodes(nodes, ("youtube",), ("bad",), 0)], [2])

    def test_max_nodes_caps_the_list(self):
        nodes = [node(i) for i in range(5)]
        self.assertEqual([n.id for n in select_nodes(nodes, (), (), 2)], [0, 1])

    def test_max_nodes_above_the_size_changes_nothing(self):
        nodes = [node(1), node(2)]
        self.assertEqual(select_nodes(nodes, (), (), 10), nodes)


class WorkerSettingsTest(unittest.TestCase):
    def test_a_single_worker_keeps_the_configured_ports(self):
        base = settings(workers=1, mihomo_api="http://127.0.0.1:19090", mihomo_proxy_port=17890)
        self.assertIs(build_worker_settings(base, 0), base)

    def test_extra_workers_get_their_own_ports_and_state(self):
        base = settings(
            workers=3,
            mihomo_api="http://127.0.0.1:19090",
            mihomo_proxy_port=17890,
            state_dir="/data",
        )
        first = build_worker_settings(base, 0)
        second = build_worker_settings(base, 1)
        self.assertEqual(first.mihomo_api, "http://127.0.0.1:19090")
        self.assertEqual(first.mihomo_proxy_port, 17890)
        self.assertEqual(second.mihomo_api, "http://127.0.0.1:19091")
        self.assertEqual(second.mihomo_proxy_port, 17891)
        self.assertEqual(first.state_dir, os.path.join("/data", "worker-1"))
        self.assertEqual(second.state_dir, os.path.join("/data", "worker-2"))

    def test_worker_indexes_do_not_share_the_state_dir(self):
        base = settings(workers=2, state_dir="/data")
        self.assertNotEqual(
            build_worker_settings(base, 0).state_dir,
            build_worker_settings(base, 1).state_dir,
        )


class NewEnvTest(unittest.TestCase):
    API_KEY = {"SUBLINK_API_KEY": "key"}

    def build(self, **env) -> Settings:
        with patch.dict(os.environ, {**self.API_KEY, **env}, clear=True):
            return Settings.from_env()

    def test_defaults_keep_the_old_behaviour(self):
        cfg = self.build()
        self.assertFalse(cfg.once)
        self.assertEqual(cfg.only_tags, ())
        self.assertEqual(cfg.skip_tags, ())
        self.assertEqual(cfg.max_nodes, 0)
        self.assertEqual(cfg.workers, 1)

    def test_lists_accept_commas_and_spaces(self):
        cfg = self.build(ONLY_TAGS="youtube, no-ads", SKIP_TAGS="broken")
        self.assertEqual(cfg.only_tags, ("youtube", "no-ads"))
        self.assertEqual(cfg.skip_tags, ("broken",))

    def test_once_flag(self):
        self.assertTrue(self.build(ONCE="1").once)

    def test_workers_must_be_positive(self):
        with self.assertRaises(ValueError):
            self.build(WORKERS="0")

    def test_workers_have_a_ceiling(self):
        with self.assertRaises(ValueError):
            self.build(WORKERS="9")

    def test_max_nodes_must_not_be_negative(self):
        with self.assertRaises(ValueError):
            self.build(MAX_NODES="-1")


if __name__ == "__main__":
    unittest.main()

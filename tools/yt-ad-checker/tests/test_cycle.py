"""The cycle must skip a node whose connection cannot be established."""

import logging
import threading
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from app.main import Worker, run_cycle
from app.mihomo import MihomoRunner
from app.probe import CLEAN, ProbeResult
from app.sublink import NodeRef

from .test_tag_sync import settings


class FakeClient:
    def __init__(self, nodes) -> None:
        self._nodes = nodes
        self.calls: list[tuple[str, list[int], str]] = []

    def ensure_tag(self, tag):
        self.calls.append(("ensure", [], tag))

    def list_nodes(self):
        return list(self._nodes)

    def add_tag(self, node_ids, tag):
        self.calls.append(("add", list(node_ids), tag))

    def remove_tag(self, node_ids, tag):
        self.calls.append(("remove", list(node_ids), tag))

    @property
    def tag_calls(self):
        return [call for call in self.calls if call[0] != "ensure"]


class FakeRunner:
    is_running = True

    def __init__(self, failures=None) -> None:
        self.failures = dict(failures or {})
        self.selected: list[str] = []

    def set_nodes(self, proxies):
        self.proxies = list(proxies)

    def start(self):
        return None

    def apply_provider(self):
        return None

    def select(self, name):
        self.selected.append(name)

    def clear_connections(self):
        return None

    def reachable(self, url):
        return self.failures.get(self.selected[-1])

    def restart(self):
        raise AssertionError("mihomo must not be restarted")


class FakeProbe:
    def __init__(self, result) -> None:
        self._result = result
        self.calls = 0
        self.started = 0
        self.closed = 0

    def start(self):
        self.started += 1

    def close(self):
        self.closed += 1

    def probe(self, timeout=None):
        self.calls += 1
        return self._result


def node(node_id=1, links=("ss://secret",)) -> NodeRef:
    return NodeRef(id=node_id, name=f"node{node_id}", group="", protocol="ss", tags=(), links=links)


class ReachabilityGateTest(unittest.TestCase):
    def run_cycle(self, failures=None, nodes=None):
        client = FakeClient(nodes if nodes is not None else [node()])
        runner = FakeRunner(failures)
        probe = FakeProbe(ProbeResult(CLEAN, "player ok"))
        stats = run_cycle(settings(), client, [Worker(0, runner, probe)], threading.Event())
        return stats, client, runner, probe

    def test_a_dead_node_skips_the_probe_and_keeps_tags(self):
        stats, client, runner, probe = self.run_cycle({"n1": "Timeout"})
        self.assertEqual(probe.calls, 0)
        self.assertEqual(stats.unreachable, 1)
        self.assertEqual(runner.selected, ["n1"])
        self.assertEqual(client.tag_calls, [])

    def test_a_reachable_node_is_probed(self):
        stats, client, _runner, probe = self.run_cycle()
        self.assertEqual(probe.calls, 1)
        self.assertEqual(stats.clean, 1)
        self.assertEqual(client.tag_calls, [("add", [1], "youtube")])

    def test_only_the_dead_link_is_skipped(self):
        stats, _client, runner, probe = self.run_cycle(
            {"n1": "Timeout"}, nodes=[node(links=("ss://a", "ss://b"))]
        )
        self.assertEqual(runner.selected, ["n1", "n1_2"])
        self.assertEqual(probe.calls, 1)
        # One dead link is enough to keep the node out of the clean tag.
        self.assertEqual(stats.unreachable, 1)


class StoppingProbe(FakeProbe):
    """Asks the cycle to stop while the first node is being probed."""

    def __init__(self, result, stop_event) -> None:
        super().__init__(result)
        self._stop_event = stop_event

    def probe(self, timeout=None):
        result = super().probe(timeout)
        self._stop_event.set()
        return result


class CycleStatsTest(unittest.TestCase):
    def test_an_interrupted_cycle_reports_what_it_checked(self):
        stop_event = threading.Event()
        nodes = [node(node_id=i) for i in range(1, 4)]
        probe = StoppingProbe(ProbeResult(CLEAN, "player ok"), stop_event)

        stats = run_cycle(
            settings(), FakeClient(nodes), [Worker(0, FakeRunner(), probe)], stop_event
        )

        self.assertEqual(stats.nodes, 3)
        self.assertEqual(stats.checked, 1)
        self.assertTrue(stats.interrupted)
        self.assertEqual(stats.workers, 1)
        self.assertEqual(stats.seconds_per_node, round(stats.duration_seconds / stats.checked, 1))

    def test_a_full_cycle_reports_its_pace(self):
        probe = FakeProbe(ProbeResult(CLEAN, "player ok"))

        stats = run_cycle(
            settings(), FakeClient([node()]), [Worker(0, FakeRunner(), probe)], threading.Event()
        )

        self.assertEqual(stats.nodes, 1)
        self.assertEqual(stats.checked, 1)
        self.assertFalse(stats.interrupted)
        self.assertEqual(stats.workers, 1)
        self.assertEqual(stats.seconds_per_node, round(stats.duration_seconds, 1))


class ReachableTest(unittest.TestCase):
    """The mihomo dial that decides whether a node deserves a probe."""

    def runner(self):
        return MihomoRunner(settings(mihomo_api="http://api.invalid"), logging.getLogger("mihomo"))

    def test_a_delay_answer_means_the_node_connects(self):
        response = SimpleNamespace(status_code=200, json=lambda: {"delay": 217})
        with patch("app.mihomo.requests.get", return_value=response):
            self.assertIsNone(self.runner().reachable("https://www.youtube.com/"))

    def test_the_generic_mihomo_error_is_reported_readably(self):
        response = SimpleNamespace(
            status_code=503, json=lambda: {"message": "An error occurred in the delay test"}
        )
        with patch("app.mihomo.requests.get", return_value=response):
            self.assertEqual(self.runner().reachable("https://www.youtube.com/"), "no answer in 6s")

    def test_a_specific_message_is_kept(self):
        response = SimpleNamespace(status_code=408, json=lambda: {"message": "Timeout"})
        with patch("app.mihomo.requests.get", return_value=response):
            self.assertEqual(self.runner().reachable("https://www.youtube.com/"), "Timeout")


if __name__ == "__main__":
    unittest.main()

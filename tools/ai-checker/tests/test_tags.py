"""Tag decisions: only proven verdicts may rewrite a node."""

from __future__ import annotations

import unittest

from app.main import apply_tags, combine, desired_tags
from app.providers import AVAILABLE, PARTIAL, RESTRICTED, UNKNOWN, Result
from app.sublink import NodeRef

from .test_config import make_settings


class FakeClient:
    def __init__(self) -> None:
        self.added: list[tuple[tuple[int, ...], str]] = []
        self.removed: list[tuple[tuple[int, ...], str]] = []

    def add_tag(self, node_ids, tag: str) -> None:
        self.added.append((tuple(node_ids), tag))

    def remove_tag(self, node_ids, tag: str) -> None:
        self.removed.append((tuple(node_ids), tag))


def make_node(tags: tuple[str, ...] = ()) -> NodeRef:
    return NodeRef(
        id=7,
        name="node-7",
        group="g",
        protocol="vless",
        tags=tags,
        links=("vless://example",),
    )


class DesiredTagsTest(unittest.TestCase):
    def setUp(self) -> None:
        self.settings = make_settings()

    def test_available_wants_the_service_tag(self):
        wanted, unwanted = desired_tags(self.settings, {"chatgpt": Result(AVAILABLE)})
        self.assertEqual(wanted, ["ai-chatgpt"])
        self.assertEqual(unwanted, ["ai-chatgpt-blocked"])

    def test_restricted_wants_the_blocked_tag(self):
        wanted, unwanted = desired_tags(self.settings, {"chatgpt": Result(RESTRICTED)})
        self.assertEqual(wanted, ["ai-chatgpt-blocked"])
        self.assertEqual(unwanted, ["ai-chatgpt"])

    def test_unknown_wants_nothing(self):
        self.assertEqual(desired_tags(self.settings, {"chatgpt": Result(UNKNOWN)}), ([], []))

    def test_partial_wants_nothing(self):
        # Only one surface works, so neither tag would be truthful.
        verdicts = {"chatgpt": Result(PARTIAL, "web_only")}
        self.assertEqual(desired_tags(self.settings, verdicts), ([], []))


class ApplyTagsTest(unittest.TestCase):
    def setUp(self) -> None:
        self.settings = make_settings()

    def test_a_new_verdict_adds_the_tag(self):
        client = FakeClient()
        apply_tags(self.settings, client, make_node(), {"claude": Result(AVAILABLE)})
        self.assertEqual(client.added, [((7,), "ai-claude")])
        self.assertEqual(client.removed, [])

    def test_the_opposite_tag_is_removed(self):
        client = FakeClient()
        node = make_node(("ai-claude-blocked",))
        apply_tags(self.settings, client, node, {"claude": Result(AVAILABLE)})
        self.assertEqual(client.added, [((7,), "ai-claude")])
        self.assertEqual(client.removed, [((7,), "ai-claude-blocked")])

    def test_an_unknown_verdict_changes_nothing(self):
        client = FakeClient()
        node = make_node(("ai-claude",))
        sync = apply_tags(self.settings, client, node, {"claude": Result(UNKNOWN, "timeout")})
        self.assertEqual((client.added, client.removed), ([], []))
        self.assertEqual(sync.added + sync.removed, 0)

    def test_dry_run_only_reports_the_plan(self):
        client = FakeClient()
        settings = make_settings(dry_run=True)
        sync = apply_tags(settings, client, make_node(), {"claude": Result(RESTRICTED)})
        self.assertEqual((client.added, client.removed), ([], []))
        self.assertTrue(sync.note.startswith("would "))

    def test_an_already_correct_node_is_left_alone(self):
        client = FakeClient()
        node = make_node(("ai-gemini",))
        sync = apply_tags(self.settings, client, node, {"gemini": Result(AVAILABLE)})
        self.assertEqual((client.added, client.removed), ([], []))
        self.assertEqual(sync.note, "")


class CombineTest(unittest.TestCase):
    def test_one_working_link_is_enough(self):
        merged = combine([Result(RESTRICTED, "x"), Result(AVAILABLE)])
        self.assertEqual(merged.status, AVAILABLE)

    def test_restricted_wins_when_nothing_works(self):
        merged = combine([Result(UNKNOWN, "timeout"), Result(RESTRICTED, "x")])
        self.assertEqual(merged.status, RESTRICTED)

    def test_no_results_is_unknown(self):
        self.assertEqual(combine([]).status, UNKNOWN)


if __name__ == "__main__":
    unittest.main()

"""Exit address parsing and the fallback chain."""

from __future__ import annotations

import unittest

from app.exitip import DEFAULT_URL, discover_exit_ip, extract_ip, is_public_ip
from tests.helpers import FakeResponse, FakeSession

IPIFY = "https://api.ipify.org?format=json"
ICANHAZIP = "https://icanhazip.com"


class IsPublicIpTest(unittest.TestCase):
    def test_routable_addresses_pass(self):
        self.assertTrue(is_public_ip("91.77.162.158"))
        self.assertTrue(is_public_ip("2001:4860:4860::8888"))

    def test_every_non_routable_range_is_rejected(self):
        for value in (
            "10.1.2.3",
            "172.16.5.4",
            "192.168.1.1",
            "127.0.0.1",
            "169.254.10.10",
            "224.0.0.1",
            "0.0.0.0",
            "fd00::1",
            "::1",
            "",
            "garbage",
        ):
            with self.subTest(value=value):
                self.assertFalse(is_public_ip(value))


class ExtractIpTest(unittest.TestCase):
    def test_a_json_answer_is_read(self):
        self.assertEqual(extract_ip('{"ip": "91.77.162.158"}'), "91.77.162.158")

    def test_a_plain_text_answer_is_read(self):
        self.assertEqual(extract_ip("91.77.162.158\r\n"), "91.77.162.158")

    def test_an_error_page_is_not_an_address(self):
        self.assertFalse(is_public_ip(extract_ip("<html>blocked</html>")))
        self.assertEqual(extract_ip('{"error": true}'), "")
        self.assertEqual(extract_ip("{oops"), "")
        self.assertEqual(extract_ip(""), "")


class DiscoverExitIpTest(unittest.TestCase):
    def test_the_json_endpoint_answers_first(self):
        session = FakeSession({IPIFY: FakeResponse(200, '{"ip": "91.77.162.158"}')})
        self.assertEqual(discover_exit_ip(session, 5), ("91.77.162.158", ""))
        self.assertEqual(session.calls, [IPIFY])

    def test_a_broken_primary_falls_back_to_plain_text(self):
        session = FakeSession({ICANHAZIP: FakeResponse(200, "91.77.162.158\n")})
        self.assertEqual(discover_exit_ip(session, 5), ("91.77.162.158", ""))
        self.assertEqual(session.calls[:2], [IPIFY, ICANHAZIP])

    def test_a_private_answer_is_not_accepted(self):
        session = FakeSession(
            {
                IPIFY: FakeResponse(200, '{"ip": "10.0.0.5"}'),
                ICANHAZIP: FakeResponse(200, "192.168.1.10"),
            }
        )
        address, reason = discover_exit_ip(session, 5)
        self.assertEqual(address, "")
        self.assertIn("api.ipify.org", reason)
        self.assertIn("icanhazip.com", reason)

    def test_every_endpoint_failing_lists_them(self):
        address, reason = discover_exit_ip(FakeSession(), 5)
        self.assertEqual(address, "")
        for host in ("api.ipify.org", "icanhazip.com", "ifconfig.me"):
            self.assertIn(host, reason)

    def test_an_http_error_does_not_stop_the_chain(self):
        session = FakeSession(
            {
                IPIFY: FakeResponse(503, ""),
                ICANHAZIP: FakeResponse(200, "91.77.162.158"),
            }
        )
        self.assertEqual(discover_exit_ip(session, 5)[0], "91.77.162.158")

    def test_the_configured_endpoint_is_tried_first(self):
        custom = "https://example.test/ip"
        session = FakeSession({custom: FakeResponse(200, '{"ip": "91.77.162.158"}')})
        self.assertEqual(discover_exit_ip(session, 5, custom)[0], "91.77.162.158")
        self.assertEqual(session.calls, [custom])

    def test_the_default_is_ipify(self):
        self.assertEqual(DEFAULT_URL, IPIFY)


if __name__ == "__main__":
    unittest.main()

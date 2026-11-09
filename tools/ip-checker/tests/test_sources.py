"""Verdicts of every IP reputation source, plus how they are folded together."""

from __future__ import annotations

import json
import unittest

from app.sources import (
    CLEAN,
    FLAGGED,
    UNKNOWN,
    IpApi,
    Iplocate,
    IpContext,
    IpQualityScore,
    ProxyCheck,
    VpnApi,
    Verdict,
    aggregate,
    select_sources,
    source_by_key,
)
from tests.helpers import FakeResponse, FakeSession

IP = "91.77.162.158"


def context_for(url: str, body: str, status: int = 200) -> IpContext:
    return IpContext(FakeSession({url: FakeResponse(status, body)}), 5)


def payload(**values: object) -> str:
    return json.dumps(values)


class IplocateTest(unittest.TestCase):
    def lookup(self, privacy: object, status: int = 200) -> Verdict:
        url = Iplocate.url.format(ip=IP)
        body = payload(ip=IP, privacy=privacy) if privacy is not None else payload(ip=IP)
        return Iplocate().check(context_for(url, body, status), IP)

    def test_a_residential_address_is_clean(self):
        verdict = self.lookup({"is_vpn": False, "is_proxy": False, "is_hosting": False})
        self.assertEqual(verdict.status, CLEAN)

    def test_vpn_flag_is_reported_by_name(self):
        verdict = self.lookup({"is_vpn": True, "is_proxy": False})
        self.assertEqual(verdict.status, FLAGGED)
        self.assertEqual(verdict.reason, "vpn")

    def test_every_set_flag_is_listed(self):
        verdict = self.lookup({"is_tor": True, "is_hosting": True})
        self.assertEqual(verdict.status, FLAGGED)
        self.assertEqual(verdict.reason, "tor+hosting")

    def test_a_missing_privacy_block_proves_nothing(self):
        # The free tier used to omit the block for some ranges; guessing would
        # be worse than admitting the answer says nothing.
        self.assertEqual(self.lookup(None).status, UNKNOWN)

    def test_a_quota_answer_is_unknown(self):
        verdict = self.lookup({"is_vpn": False}, status=429)
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "rate_limited")

    def test_an_html_error_page_is_not_a_verdict(self):
        url = Iplocate.url.format(ip=IP)
        verdict = Iplocate().check(context_for(url, "<html>502</html>"), IP)
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "no_json")

    def test_the_key_is_sent_when_configured(self):
        url = Iplocate.url.format(ip=IP) + "?apikey=secret"
        context = context_for(url, payload(privacy={"is_vpn": False}))
        verdict = Iplocate("secret").check(context, IP)
        self.assertEqual(verdict.status, CLEAN)


class IpApiTest(unittest.TestCase):
    def lookup(self, body: dict, status: int = 200) -> Verdict:
        url = IpApi.url.format(ip=IP)
        return IpApi().check(context_for(url, json.dumps(body), status), IP)

    def test_a_residential_address_is_clean(self):
        verdict = self.lookup({"status": "success", "hosting": False, "proxy": False})
        self.assertEqual(verdict.status, CLEAN)

    def test_hosting_and_proxy_are_flags(self):
        self.assertEqual(
            self.lookup({"status": "success", "hosting": True, "proxy": False}).reason,
            "hosting",
        )
        self.assertEqual(
            self.lookup({"status": "success", "hosting": False, "proxy": True}).reason,
            "proxy",
        )

    def test_a_failed_lookup_keeps_its_message(self):
        verdict = self.lookup({"status": "fail", "message": "quota exceeded"})
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "quota exceeded")

    def test_an_empty_body_is_not_parsed_as_clean(self):
        url = IpApi.url.format(ip=IP)
        verdict = IpApi().check(context_for(url, "", status=200), IP)
        self.assertEqual(verdict.status, UNKNOWN)


class ProxyCheckTest(unittest.TestCase):
    url = ProxyCheck.url.format(ip=IP)

    def lookup(self, body: dict, status: int = 200) -> Verdict:
        return ProxyCheck().check(context_for(self.url, json.dumps(body), status), IP)

    def test_a_proxy_yes_is_flagged(self):
        verdict = self.lookup({"status": "ok", IP: {"proxy": "yes", "type": "VPN"}})
        self.assertEqual(verdict.status, FLAGGED)
        self.assertIn("proxy=yes", verdict.reason)

    def test_a_vpn_type_is_flagged_even_without_the_flag(self):
        verdict = self.lookup({"status": "ok", IP: {"proxy": "no", "type": "VPN"}})
        self.assertEqual(verdict.status, FLAGGED)

    def test_a_clean_business_network_passes(self):
        verdict = self.lookup({"status": "ok", IP: {"proxy": "no", "type": "Business"}})
        self.assertEqual(verdict.status, CLEAN)

    def test_a_denied_quota_is_unknown(self):
        verdict = self.lookup({"status": "denied", "message": "quota reached"})
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "quota reached")

    def test_the_optional_key_is_appended(self):
        url = f"{self.url}&key=secret"
        verdict = ProxyCheck("secret").check(
            context_for(url, json.dumps({"status": "ok", IP: {"proxy": "no"}})), IP
        )
        self.assertEqual(verdict.status, CLEAN)


class IpQualityScoreTest(unittest.TestCase):
    def lookup(self, body: dict) -> Verdict:
        url = IpQualityScore.url.format(key="secret", ip=IP)
        return IpQualityScore("secret").check(context_for(url, json.dumps(body)), IP)

    def test_vpn_and_proxy_flags_are_reported(self):
        verdict = self.lookup({"success": True, "vpn": True, "proxy": False})
        self.assertEqual(verdict.status, FLAGGED)
        self.assertEqual(verdict.reason, "vpn")

    def test_a_very_high_fraud_score_is_flagged(self):
        verdict = self.lookup({"success": True, "vpn": False, "fraud_score": 95})
        self.assertEqual(verdict.status, FLAGGED)
        self.assertEqual(verdict.reason, "fraud_score=95")

    def test_a_low_fraud_score_is_clean(self):
        verdict = self.lookup({"success": True, "fraud_score": 4})
        self.assertEqual(verdict.status, CLEAN)

    def test_a_rejected_query_is_unknown(self):
        verdict = self.lookup({"success": False, "message": "invalid key"})
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "invalid key")

    def test_without_a_key_it_says_so(self):
        verdict = IpQualityScore().check(IpContext(FakeSession(), 5), IP)
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "no_api_key")


class VpnApiTest(unittest.TestCase):
    def lookup(self, body: dict) -> Verdict:
        url = VpnApi.url.format(ip=IP, key="secret")
        return VpnApi("secret").check(context_for(url, json.dumps(body)), IP)

    def test_a_relay_is_flagged(self):
        verdict = self.lookup({"security": {"vpn": False, "relay": True}})
        self.assertEqual(verdict.status, FLAGGED)
        self.assertEqual(verdict.reason, "relay")

    def test_an_empty_security_block_is_clean(self):
        verdict = self.lookup({"security": {"vpn": False, "proxy": False, "tor": False}})
        self.assertEqual(verdict.status, CLEAN)

    def test_an_error_envelope_is_unknown(self):
        verdict = self.lookup({"message": "invalid key"})
        self.assertEqual(verdict.status, UNKNOWN)
        self.assertEqual(verdict.reason, "invalid key")


class RegistryTest(unittest.TestCase):
    def test_aliases_resolve_to_the_same_source(self):
        self.assertIs(source_by_key("ip-api"), IpApi)
        self.assertIs(source_by_key("IPQS"), IpQualityScore)
        self.assertIsNone(source_by_key("nope"))

    def test_the_default_pair_is_keyless(self):
        sources = select_sources(("iplocate", "ipapi"), {})
        self.assertEqual([source.key for source in sources], ["iplocate", "ipapi"])

    def test_a_keyed_source_without_a_key_is_a_configuration_error(self):
        with self.assertRaises(ValueError) as caught:
            select_sources(("ipqualityscore",), {})
        self.assertIn("IPQUALITYSCORE_KEY", str(caught.exception))

    def test_an_unknown_key_is_a_configuration_error(self):
        with self.assertRaises(ValueError):
            select_sources(("nope",), {})

    def test_duplicates_are_collapsed(self):
        sources = select_sources(("ipapi", "ip-api"), {})
        self.assertEqual([source.key for source in sources], ["ipapi"])


class AggregateTest(unittest.TestCase):
    def test_one_flag_beats_any_number_of_clean_answers(self):
        status, reason = aggregate(
            {"iplocate": Verdict(CLEAN), "ipapi": Verdict(FLAGGED, "hosting")}, 1
        )
        self.assertEqual(status, FLAGGED)
        self.assertEqual(reason, "ipapi: hosting")

    def test_a_single_clean_answer_is_enough_by_default(self):
        status, _ = aggregate({"iplocate": Verdict(CLEAN), "ipapi": Verdict(UNKNOWN, "no answer")}, 1)
        self.assertEqual(status, CLEAN)

    def test_strict_mode_waits_for_every_required_answer(self):
        status, reason = aggregate(
            {"iplocate": Verdict(CLEAN), "ipapi": Verdict(UNKNOWN, "no answer")}, 2
        )
        self.assertEqual(status, UNKNOWN)
        self.assertIn("ipapi: no answer", reason)

    def test_strict_mode_passes_when_both_agree(self):
        status, _ = aggregate({"iplocate": Verdict(CLEAN), "ipapi": Verdict(CLEAN)}, 2)
        self.assertEqual(status, CLEAN)

    def test_nothing_answered_is_unknown(self):
        status, reason = aggregate({"ipapi": Verdict(UNKNOWN, "rate_limited")}, 1)
        self.assertEqual(status, UNKNOWN)
        self.assertEqual(reason, "ipapi: rate_limited")


if __name__ == "__main__":
    unittest.main()

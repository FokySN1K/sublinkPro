"""Verdict rules for every provider, built from synthetic HTTP answers."""

from __future__ import annotations

import unittest

from app.providers import (
    AVAILABLE,
    PARTIAL,
    RESTRICTED,
    UNKNOWN,
    ChatGPT,
    Claude,
    Gemini,
    Grok,
    Perplexity,
    ProbeContext,
    Response,
    provider_by_key,
    select_providers,
)


def response(status: int, body: str = "", final_url: str = "", headers: dict | None = None) -> Response:
    return Response(
        status=status,
        final_url=final_url or "https://example.com/",
        headers=headers or {},
        body=body,
    )


CHALLENGE_HEADERS = {"cf-mitigated": "challenge"}


class FakeSession:
    """Stands in for ``requests.Session`` inside ``ProbeContext``."""

    def __init__(self, answers: dict[str, Response]) -> None:
        self._answers = answers
        self.calls: list[str] = []

    def get(self, url: str, **_kwargs: object) -> "_FakeResponse":
        self.calls.append(url)
        answer = self._answers.get(url)
        if answer is None:
            raise AssertionError(f"unexpected request to {url}")
        return _FakeResponse(answer)


class _FakeResponse:
    """The slice of ``requests.Response`` that ``ProbeContext`` reads."""

    def __init__(self, answer: Response) -> None:
        self.status_code = answer.status
        self.url = answer.final_url
        self.headers = answer.headers
        self.text = answer.body


def probe_context(session: FakeSession) -> ProbeContext:
    return ProbeContext(session, 5.0, "test-agent")  # type: ignore[arg-type]


class ChatGPTTest(unittest.TestCase):
    provider = ChatGPT()

    def test_unsupported_country_marks_the_exit_restricted(self):
        result = self.provider.evaluate(
            response(200, '{"unsupported_country": true}'), "RU"
        )
        self.assertEqual(result.status, RESTRICTED)
        self.assertEqual(result.reason, "unsupported_country")
        self.assertEqual(result.region, "RU")

    def test_a_regular_compliance_answer_means_available(self):
        result = self.provider.evaluate(
            response(200, '{"cookie_consent_required": true}'), "DE"
        )
        self.assertEqual(result.status, AVAILABLE)

    def test_a_cloudflare_challenge_is_not_a_block(self):
        result = self.provider.evaluate(
            response(403, "Just a moment...", headers=CHALLENGE_HEADERS), "DE"
        )
        self.assertEqual(result.status, UNKNOWN)
        self.assertEqual(result.reason, "cloudflare_challenge")

    def test_a_plain_403_is_a_block(self):
        self.assertEqual(self.provider.evaluate(response(403)).status, RESTRICTED)

    def test_no_answer_is_unknown(self):
        self.assertEqual(self.provider.evaluate(None).status, UNKNOWN)


class ChatGPTSurfacesTest(unittest.TestCase):
    """The web and the iOS app are gated separately."""

    provider = ChatGPT()

    def test_both_surfaces_blocked_stays_restricted(self):
        verdict = self.provider.evaluate(response(200, '{"unsupported_country": true}'))
        refined = self.provider.refine(verdict, response(403, '{"vpn": true}'))
        self.assertEqual(refined.status, RESTRICTED)

    def test_a_working_app_downgrades_the_web_block_to_partial(self):
        verdict = self.provider.evaluate(response(200, '{"unsupported_country": true}'))
        refined = self.provider.refine(verdict, response(200, '<html>ok</html>'))
        self.assertEqual(refined.status, PARTIAL)
        self.assertEqual(refined.reason, "mobile_app_only")

    def test_a_blocked_app_downgrades_a_working_web_to_partial(self):
        verdict = self.provider.evaluate(response(200, '{"cookie_consent_required": true}'))
        refined = self.provider.refine(verdict, response(403, '{"vpn": true}'))
        self.assertEqual(refined.status, PARTIAL)
        self.assertEqual(refined.reason, "web_only")

    def test_an_empty_app_answer_proves_nothing(self):
        # ios.chat.openai.com answers 403 with an empty body to healthy exits.
        verdict = self.provider.evaluate(response(200, '{"cookie_consent_required": true}'))
        self.assertEqual(
            self.provider.refine(verdict, response(403, "")).status, AVAILABLE
        )

    def test_a_challenged_app_endpoint_keeps_the_compliance_verdict(self):
        verdict = self.provider.evaluate(response(200, '{"unsupported_country": true}'))
        refined = self.provider.refine(
            verdict, response(403, "Just a moment...", headers=CHALLENGE_HEADERS)
        )
        self.assertEqual(refined.status, RESTRICTED)


class ClaudeTest(unittest.TestCase):
    provider = Claude()

    def test_region_redirect_is_restricted(self):
        result = self.provider.evaluate(
            response(200, final_url="https://www.anthropic.com/app-unavailable-in-region")
        )
        self.assertEqual(result.status, RESTRICTED)
        self.assertEqual(result.reason, "app_unavailable_in_region")

    def test_a_normal_page_is_available(self):
        result = self.provider.evaluate(response(200, final_url="https://claude.ai/login"))
        self.assertEqual(result.status, AVAILABLE)

    def test_plain_403_is_restricted(self):
        self.assertEqual(self.provider.evaluate(response(403)).status, RESTRICTED)

    def test_rate_limit_is_unknown(self):
        self.assertEqual(self.provider.evaluate(response(429)).reason, "rate_limited")


class GeminiTest(unittest.TestCase):
    provider = Gemini()

    def test_availability_flag_marks_available(self):
        result = self.provider.evaluate(response(200, 'var cfg={"45631641":true};'))
        self.assertEqual(result.status, AVAILABLE)

    def test_the_escaped_flag_is_matched_too(self):
        # The production page embeds the flag inside an escaped JSON blob:
        #    \"45631641\":true
        result = self.provider.evaluate(
            response(200, 'DS=\'{\\"45631641\\":true,\\"45841483\\":true}\';')
        )
        self.assertEqual(result.status, AVAILABLE)

    def test_region_phrase_marks_restricted(self):
        result = self.provider.evaluate(
            response(200, "Gemini isn't available in your country")
        )
        self.assertEqual(result.status, RESTRICTED)
        self.assertEqual(result.reason, "region_blocked")

    def test_a_missing_marker_is_unknown_not_restricted(self):
        # The page markup changes often; guessing "restricted" would drop
        # working nodes out of every filtered subscription.
        result = self.provider.evaluate(response(200, "<html>new markup</html>"))
        self.assertEqual(result.status, UNKNOWN)
        self.assertEqual(result.reason, "gemini_marker_missing")

    def test_plain_403_is_unknown(self):
        self.assertEqual(self.provider.evaluate(response(403)).status, UNKNOWN)


class SimpleProviderTest(unittest.TestCase):
    provider = Grok()

    def test_200_without_markers_is_available(self):
        self.assertEqual(self.provider.evaluate(response(200, "<html>ok</html>")).status, AVAILABLE)

    def test_region_phrase_is_restricted(self):
        result = self.provider.evaluate(response(200, "Grok is not available in your region"))
        self.assertEqual(result.status, RESTRICTED)

    def test_403_is_unknown(self):
        self.assertEqual(self.provider.evaluate(response(403)).status, UNKNOWN)

    def test_server_error_is_unknown(self):
        self.assertEqual(self.provider.evaluate(response(503)).status, UNKNOWN)

    def test_timeout_is_unknown(self):
        self.assertEqual(self.provider.evaluate(None).status, UNKNOWN)


class PerplexityTest(unittest.TestCase):
    provider = Perplexity()

    def test_the_json_session_endpoint_is_enough(self):
        session = FakeSession(
            {
                Perplexity.trace_url: response(200, "loc=DE"),
                Perplexity.session_url: response(200, "{}"),
            }
        )
        result = self.provider.check(probe_context(session))
        self.assertEqual(result.status, AVAILABLE)
        self.assertEqual(result.region, "DE")
        self.assertEqual(session.calls, [Perplexity.trace_url, Perplexity.session_url])

    def test_a_challenged_json_endpoint_falls_back_to_the_page(self):
        session = FakeSession(
            {
                Perplexity.trace_url: response(200, "loc=DE"),
                Perplexity.session_url: response(
                    403, "Just a moment...", headers=CHALLENGE_HEADERS
                ),
                Perplexity.home: response(200, "<html>Perplexity</html>"),
            }
        )
        result = self.provider.check(probe_context(session))
        self.assertEqual(result.status, AVAILABLE)
        self.assertEqual(
            session.calls, [Perplexity.trace_url, Perplexity.session_url, Perplexity.home]
        )

    def test_both_endpoints_challenged_is_unknown(self):
        session = FakeSession(
            {
                Perplexity.trace_url: response(200, "loc=DE"),
                Perplexity.session_url: response(
                    403, "Just a moment...", headers=CHALLENGE_HEADERS
                ),
                Perplexity.home: response(403, "Just a moment...", headers=CHALLENGE_HEADERS),
            }
        )
        self.assertEqual(self.provider.check(probe_context(session)).status, UNKNOWN)

    def test_a_region_phrase_on_the_page_is_restricted(self):
        session = FakeSession(
            {
                Perplexity.trace_url: response(200, "loc=RU"),
                Perplexity.session_url: response(403),
                Perplexity.home: response(200, "Perplexity is not available in your country"),
            }
        )
        result = self.provider.check(probe_context(session))
        self.assertEqual(result.status, RESTRICTED)
        self.assertEqual(result.region, "RU")


class RegistryTest(unittest.TestCase):
    def test_the_openai_alias_resolves_to_chatgpt(self):
        provider = provider_by_key("openai")
        self.assertIsNotNone(provider)
        self.assertEqual(provider.key, "chatgpt")

    def test_selection_keeps_the_requested_order_and_drops_duplicates(self):
        providers = select_providers(["claude", "chatgpt", "openai", "qwen"])
        self.assertEqual([p.key for p in providers], ["claude", "chatgpt", "qwen"])

    def test_unknown_keys_are_ignored(self):
        self.assertEqual(select_providers(["nope"]), [])


if __name__ == "__main__":
    unittest.main()

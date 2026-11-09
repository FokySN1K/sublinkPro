"""HTTP probes that decide whether an AI service accepts the node's exit IP.

Every provider returns one of four verdicts:

``available``
    The service answers normally from this IP.
``restricted``
    The service itself says the region is not supported.
``partial``
    Only some surfaces work, e.g. ChatGPT answers the web but not the mobile
    app.  Kept in the log for diagnosis, never written to a tag.
``unknown``
    The answer proves nothing: a Cloudflare challenge, 429, 5xx, a timeout.

Only ``available`` and ``restricted`` move tags.  A captcha, a rate limit or a
connection reset leaves the node untouched, so a bot check can never mark a
working node as blocked (and vice versa).
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, replace
from typing import Mapping, Optional, Sequence

import requests

AVAILABLE = "available"
RESTRICTED = "restricted"
PARTIAL = "partial"
UNKNOWN = "unknown"

DEFAULT_BODY_LIMIT = 256 * 1024
GEMINI_BODY_LIMIT = 1024 * 1024
TRACE_BODY_LIMIT = 8 * 1024

# Phrases services put on the page they serve to a blocked region.  The list is
# deliberately short and specific: a long list of generic words would match
# marketing copy and turn every node into "restricted".
REGION_MARKERS = (
    "not available in your country",
    "not available in your region",
    "isn't available in your country",
    "isn't available in your region",
    "is not available in your country",
    "is not available in your region",
    "unavailable in your country",
    "unavailable in your region",
    "unsupported_country",
    "app-unavailable-in-region",
)

# A solved-by-a-browser page, not a regional block.  Treating it as restricted
# is the most common way such checkers produce false negatives.
CHALLENGE_MARKERS = (
    "just a moment",
    "cf-chl",
    "__cf_chl",
    "enable javascript and cookies to continue",
    "attention required! | cloudflare",
)

# Regions blocked by most of the large AI vendors.  Only used for the hint in
# the log line, never for the verdict: the service itself is the authority.
COMMON_BLOCKED_REGIONS = frozenset({"CN", "HK", "MO", "RU", "BY", "IR", "KP", "SY", "CU", "VE"})


@dataclass(frozen=True)
class Result:
    """The verdict for one provider on one node."""

    status: str
    reason: str = ""
    region: str = ""

    def with_region(self, region: str) -> "Result":
        return replace(self, region=region) if region else self


@dataclass(frozen=True)
class Response:
    """The parts of an HTTP answer the verdicts are built from."""

    status: int
    final_url: str
    headers: Mapping[str, str]
    body: str

    @property
    def text(self) -> str:
        return self.body.lower()


class ProbeContext:
    """A proxy-bound HTTP session plus the knobs every probe shares."""

    def __init__(
        self,
        session: requests.Session,
        timeout: float,
        user_agent: str,
        logger: Optional[logging.Logger] = None,
    ) -> None:
        self._session = session
        self._timeout = timeout
        self._user_agent = user_agent
        self._log = logger or logging.getLogger("probe")

    def get(
        self,
        url: str,
        headers: Optional[Mapping[str, str]] = None,
        allow_redirects: bool = True,
        limit: int = DEFAULT_BODY_LIMIT,
    ) -> Optional[Response]:
        merged = {
            "User-Agent": self._user_agent,
            "Accept-Language": "en-US,en;q=0.9",
            "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        }
        if headers:
            merged.update(headers)
        try:
            response = self._session.get(
                url,
                headers=merged,
                timeout=self._timeout,
                allow_redirects=allow_redirects,
                stream=False,
            )
        except requests.RequestException as exc:
            self._log.debug("%s: %s", url, exc)
            return None
        body = response.text[:limit] if response.text else ""
        return Response(
            status=response.status_code,
            final_url=str(response.url),
            headers=response.headers,
            body=body,
        )


def cloudflare_region(ctx: ProbeContext, trace_url: str) -> str:
    """The Cloudflare edge country for this exit, e.g. ``DE``; empty if unknown."""
    response = ctx.get(trace_url, limit=TRACE_BODY_LIMIT)
    if response is None or response.status != 200:
        return ""
    for line in response.body.splitlines():
        if line.startswith("loc="):
            return line[len("loc=") :].strip().upper()
    return ""


def _challenge(response: Response) -> bool:
    if str(response.headers.get("cf-mitigated", "")).lower() == "challenge":
        return True
    return any(marker in response.text for marker in CHALLENGE_MARKERS)


def _region_marker(response: Response) -> str:
    for marker in REGION_MARKERS:
        if marker in response.text:
            return marker
    return ""


def _neutral(response: Optional[Response]) -> Optional[Result]:
    """A verdict no HTTP answer can improve on: challenge, throttle, outage."""
    if response is None:
        return Result(UNKNOWN, "no answer")
    if _challenge(response):
        return Result(UNKNOWN, "cloudflare_challenge")
    if response.status == 429:
        return Result(UNKNOWN, "rate_limited")
    if response.status >= 500:
        return Result(UNKNOWN, f"http_{response.status}")
    return None


class Provider:
    """One AI service. Subclasses implement ``check`` (and ``evaluate``)."""

    key = ""
    label = ""
    home = ""
    aliases: Sequence[str] = ()

    def check(self, ctx: ProbeContext) -> Result:  # pragma: no cover - overridden
        raise NotImplementedError

    def __repr__(self) -> str:  # pragma: no cover - debugging helper
        return f"<{self.__class__.__name__} {self.key}>"


class ChatGPT(Provider):
    """OpenAI answers ``unsupported_country`` for blocked exits on its API.

    The web and the iOS app are gated separately, so both endpoints are probed:
    when they disagree the node is ``partial`` (one surface only) instead of
    being rounded to available or blocked.
    """

    key = "chatgpt"
    label = "ChatGPT / OpenAI"
    home = "https://chatgpt.com/"
    aliases = ("openai",)

    compliance_url = "https://api.openai.com/compliance/cookie_requirements"
    compliance_headers = {
        "Accept": "*/*",
        "Authorization": "Bearer null",
        "Content-Type": "application/json",
        "Origin": "https://platform.openai.com",
        "Referer": "https://platform.openai.com/",
    }
    ios_url = "https://ios.chat.openai.com/"
    ios_headers = {"Accept": "*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"}

    def check(self, ctx: ProbeContext) -> Result:
        region = cloudflare_region(ctx, "https://chatgpt.com/cdn-cgi/trace")
        response = ctx.get(self.compliance_url, self.compliance_headers)
        verdict = self.evaluate(response, region)
        if verdict.status not in (AVAILABLE, RESTRICTED):
            return verdict
        return self.refine(verdict, ctx.get(self.ios_url, self.ios_headers))

    def evaluate(self, response: Optional[Response], region: str = "") -> Result:
        neutral = _neutral(response)
        if neutral is not None:
            return neutral.with_region(region)
        assert response is not None  # _neutral covers None
        if "unsupported_country" in response.text:
            return Result(RESTRICTED, "unsupported_country", region)
        if response.status == 403:
            return Result(RESTRICTED, "http_403", region)
        if 200 <= response.status < 400:
            return Result(AVAILABLE, "", region)
        return Result(UNKNOWN, f"http_{response.status}", region)

    def refine(self, verdict: Result, ios: Optional[Response]) -> Result:
        """Cross-check the compliance verdict with the iOS app endpoint.

        The app answers the word ``vpn`` to an exit it refuses.  A challenge or
        an empty answer proves nothing, so the compliance verdict stands.
        """
        if _neutral(ios) is not None:
            return verdict
        assert ios is not None
        app_blocked = "vpn" in ios.text
        web_blocked = verdict.status == RESTRICTED
        if app_blocked == web_blocked:
            return verdict
        surface = "mobile_app_only" if web_blocked else "web_only"
        return Result(PARTIAL, surface, verdict.region)


class Claude(Provider):
    """Anthropic redirects blocked exits to ``app-unavailable-in-region``."""

    key = "claude"
    label = "Claude"
    home = "https://claude.ai/"

    def check(self, ctx: ProbeContext) -> Result:
        region = cloudflare_region(ctx, "https://claude.ai/cdn-cgi/trace")
        return self.evaluate(ctx.get("https://claude.ai/"), region)

    def evaluate(self, response: Optional[Response], region: str = "") -> Result:
        neutral = _neutral(response)
        if neutral is not None:
            return neutral.with_region(region)
        assert response is not None
        final_url = response.final_url.lower()
        if "app-unavailable-in-region" in final_url:
            return Result(RESTRICTED, "app_unavailable_in_region", region)
        marker = _region_marker(response)
        if marker:
            return Result(RESTRICTED, marker, region)
        if response.status == 403:
            return Result(RESTRICTED, "http_403", region)
        if 200 <= response.status < 400:
            return Result(AVAILABLE, "", region)
        return Result(UNKNOWN, f"http_{response.status}", region)


class Gemini(Provider):
    """Google embeds an availability flag; a missing flag is not a block."""

    key = "gemini"
    label = "Gemini"
    home = "https://gemini.google.com/"

    # Google serves the flag inside an escaped JSON blob, so both the raw and
    # the backslash-escaped spelling have to be matched.
    availability_markers = (
        '"45631641":true',
        '"45631641": true',
        '\\"45631641\\":true',
        '\\"45631641\\": true',
    )
    restricted_markers = (
        "gemini isn't available in your country",
        "gemini is not available in your country",
        "gemini isn't available in your region",
        "gemini is not available in your region",
    )

    def check(self, ctx: ProbeContext) -> Result:
        return self.evaluate(ctx.get("https://gemini.google.com/", limit=GEMINI_BODY_LIMIT))

    def evaluate(self, response: Optional[Response], region: str = "") -> Result:
        neutral = _neutral(response)
        if neutral is not None:
            return neutral.with_region(region)
        assert response is not None
        if 200 <= response.status < 400:
            if any(marker in response.text for marker in self.availability_markers):
                return Result(AVAILABLE, "", region)
            if any(marker in response.text for marker in self.restricted_markers):
                return Result(RESTRICTED, "region_blocked", region)
            # The page changed: say "unknown" instead of guessing a block.
            return Result(UNKNOWN, "gemini_marker_missing", region)
        return Result(UNKNOWN, f"http_{response.status}", region)


class SimpleProvider(Provider):
    """A 200 page means available; a region marker or a challenge overrides it.

    ``403`` stays ``unknown``: without a regional phrase in the body it is far
    more often a bot rule than a country block.
    """

    trace_url = ""

    def check(self, ctx: ProbeContext) -> Result:
        region = cloudflare_region(ctx, self.trace_url) if self.trace_url else ""
        return self.evaluate(ctx.get(self.home), region)

    def evaluate(self, response: Optional[Response], region: str = "") -> Result:
        neutral = _neutral(response)
        if neutral is not None:
            return neutral.with_region(region)
        assert response is not None
        marker = _region_marker(response)
        if marker:
            return Result(RESTRICTED, marker, region)
        if 200 <= response.status < 400:
            return Result(AVAILABLE, "", region)
        return Result(UNKNOWN, f"http_{response.status}", region)


class Copilot(SimpleProvider):
    key = "copilot"
    label = "Microsoft Copilot"
    home = "https://copilot.microsoft.com/"


class Perplexity(SimpleProvider):
    """The landing page is challenged for most exits; the JSON session
    endpoint answers an anonymous visitor directly."""

    key = "perplexity"
    label = "Perplexity"
    home = "https://www.perplexity.ai/"
    trace_url = "https://www.perplexity.ai/cdn-cgi/trace"
    session_url = "https://www.perplexity.ai/api/auth/session"

    def check(self, ctx: ProbeContext) -> Result:
        region = cloudflare_region(ctx, self.trace_url)
        verdict = self.evaluate(
            ctx.get(
                self.session_url,
                headers={"Accept": "application/json"},
                limit=TRACE_BODY_LIMIT,
            ),
            region,
        )
        if verdict.status != UNKNOWN:
            return verdict
        return self.evaluate(ctx.get(self.home), region)


class Grok(SimpleProvider):
    key = "grok"
    label = "Grok (xAI)"
    home = "https://grok.com/"
    trace_url = "https://grok.com/cdn-cgi/trace"


class Mistral(SimpleProvider):
    key = "mistral"
    label = "Mistral Le Chat"
    home = "https://chat.mistral.ai/"


class DeepSeek(SimpleProvider):
    key = "deepseek"
    label = "DeepSeek"
    home = "https://chat.deepseek.com/"


class Qwen(SimpleProvider):
    key = "qwen"
    label = "Qwen Chat"
    home = "https://chat.qwen.ai/"


class Poe(SimpleProvider):
    key = "poe"
    label = "Poe"
    home = "https://poe.com/"
    trace_url = "https://poe.com/cdn-cgi/trace"


ALL_PROVIDERS = (
    ChatGPT(),
    Claude(),
    Gemini(),
    Copilot(),
    Perplexity(),
    Grok(),
    Mistral(),
    DeepSeek(),
    Qwen(),
    Poe(),
)
PROVIDER_KEYS = tuple(provider.key for provider in ALL_PROVIDERS)

_BY_KEY: dict[str, Provider] = {}
for _provider in ALL_PROVIDERS:
    _BY_KEY[_provider.key] = _provider
    for _alias in _provider.aliases:
        _BY_KEY[_alias] = _provider


def provider_by_key(key: str) -> Optional[Provider]:
    return _BY_KEY.get(key.strip().lower())


def select_providers(keys: Sequence[str]) -> list[Provider]:
    """Resolve provider keys (aliases included), preserving the given order."""
    selected: list[Provider] = []
    for key in keys:
        provider = provider_by_key(key)
        if provider is not None and provider not in selected:
            selected.append(provider)
    return selected

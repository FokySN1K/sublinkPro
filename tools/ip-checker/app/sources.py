"""IP reputation sources: is the node's exit address a VPN, a proxy or a datacenter?

Each source answers the same question about one address and is asked separately,
so a provider outage or a single false positive never decides the verdict alone.
Every source returns one of three verdicts:

``clean``
    Nothing in the answer says VPN, proxy, Tor or hosting.
``flagged``
    The source recognised the address as VPN/proxy/Tor/hosting/abusive; the
    reason names the flag that fired.
``unknown``
    The answer proves nothing: a quota, an HTTP error, a missing field, a
    timeout.  ``unknown`` never rewrites a tag.

Only the keyless sources (``iplocate``, ``ipapi``) are enabled by default; the
rest are switched on through ``SOURCES`` once their key is present.
"""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from typing import Any, Mapping, Optional, Sequence
from urllib.parse import quote

import requests

CLEAN = "clean"
FLAGGED = "flagged"
UNKNOWN = "unknown"

DEFAULT_BODY_LIMIT = 64 * 1024

# These endpoints answer small JSON documents; a proxy that injects an HTML
# error page must not be parsed as a verdict, hence the strict parsers below.


@dataclass(frozen=True)
class Response:
    """The parts of an HTTP answer the verdicts are built from."""

    status: int
    body: str

    def json(self) -> Optional[dict[str, Any]]:
        try:
            payload = json.loads(self.body)
        except ValueError:
            return None
        return payload if isinstance(payload, dict) else None


@dataclass(frozen=True)
class Verdict:
    """What one source said about one address."""

    status: str
    reason: str = ""


class IpContext:
    """A proxy-bound HTTP session plus the knobs every lookup shares."""

    def __init__(
        self,
        session: requests.Session,
        timeout: float,
        user_agent: str = "ip-checker/1.0",
        logger: Optional[logging.Logger] = None,
    ) -> None:
        self._session = session
        self._timeout = timeout
        self._user_agent = user_agent
        self._log = logger or logging.getLogger("source")

    def get(
        self,
        url: str,
        headers: Optional[Mapping[str, str]] = None,
        limit: int = DEFAULT_BODY_LIMIT,
    ) -> Optional[Response]:
        merged = {"User-Agent": self._user_agent, "Accept": "application/json"}
        if headers:
            merged.update(headers)
        try:
            response = self._session.get(url, headers=merged, timeout=self._timeout)
        except requests.RequestException as exc:
            self._log.debug("%s: %s", url, exc)
            return None
        body = response.text[:limit] if response.text else ""
        return Response(status=response.status_code, body=body)


def _neutral(response: Optional[Response]) -> Optional[Verdict]:
    """A verdict no payload can improve on: no answer, throttle, outage."""
    if response is None:
        return Verdict(UNKNOWN, "no answer")
    if response.status == 429:
        return Verdict(UNKNOWN, "rate_limited")
    if response.status >= 400:
        return Verdict(UNKNOWN, f"http_{response.status}")
    return None


class Source:
    """One IP reputation provider. Subclasses implement ``check``."""

    key = ""
    label = ""
    aliases: Sequence[str] = ()
    key_env = ""
    requires_key = False

    def __init__(self, api_key: str = "", logger: Optional[logging.Logger] = None) -> None:
        self._api_key = api_key
        self._log = logger or logging.getLogger("source")

    def check(self, ctx: IpContext, ip: str) -> Verdict:  # pragma: no cover - overridden
        raise NotImplementedError

    def __repr__(self) -> str:  # pragma: no cover - debugging helper
        return f"<{self.__class__.__name__} {self.key}>"


class Iplocate(Source):
    """iplocate.io exposes the whole ``privacy`` block on its free tier.

    A clean answer looks like ``{"asn": {"type": "isp"}, "privacy":
    {"is_vpn": false, "is_proxy": false, "is_tor": false, "is_hosting": false,
    ...}}``; any ``true`` flag is a reason not to call the address clean.
    """

    key = "iplocate"
    label = "iplocate.io"
    key_env = "IPLOCATE_API_KEY"
    url = "https://www.iplocate.io/api/lookup/{ip}"
    flags = (
        ("is_vpn", "vpn"),
        ("is_proxy", "proxy"),
        ("is_tor", "tor"),
        ("is_hosting", "hosting"),
        ("is_abuser", "abuser"),
        ("is_icloud_relay", "icloud_relay"),
        ("is_bogon", "bogon"),
    )

    def check(self, ctx: IpContext, ip: str) -> Verdict:
        url = self.url.format(ip=quote(ip))
        if self._api_key:
            url = f"{url}?apikey={quote(self._api_key)}"
        response = ctx.get(url)
        neutral = _neutral(response)
        if neutral is not None:
            return neutral
        payload = response.json()  # type: ignore[union-attr]
        if payload is None:
            return Verdict(UNKNOWN, "no_json")
        privacy = payload.get("privacy")
        if not isinstance(privacy, dict) or not privacy:
            # The free tier used to omit this block for some ranges; without it
            # the answer says nothing about VPN/DC usage.
            return Verdict(UNKNOWN, "no_privacy_block")
        hits = [name for field, name in self.flags if privacy.get(field) is True]
        if hits:
            return Verdict(FLAGGED, "+".join(hits))
        return Verdict(CLEAN)


class IpApi(Source):
    """ip-api.com free tier: ``hosting``/``proxy`` flags, plain HTTP only."""

    key = "ipapi"
    label = "ip-api.com"
    aliases = ("ip-api",)
    url = (
        "http://ip-api.com/json/{ip}"
        "?fields=status,message,query,hosting,proxy,mobile,as,country"
    )

    def check(self, ctx: IpContext, ip: str) -> Verdict:
        response = ctx.get(self.url.format(ip=quote(ip)))
        neutral = _neutral(response)
        if neutral is not None:
            return neutral
        payload = response.json()  # type: ignore[union-attr]
        if payload is None:
            return Verdict(UNKNOWN, "no_json")
        if payload.get("status") != "success":
            message = str(payload.get("message") or "lookup failed")
            return Verdict(UNKNOWN, message[:60])
        hits = [
            name
            for field, name in (("proxy", "proxy"), ("hosting", "hosting"))
            if payload.get(field) is True
        ]
        if hits:
            return Verdict(FLAGGED, "+".join(hits))
        return Verdict(CLEAN)


class ProxyCheck(Source):
    """proxycheck.io: ``proxy: yes/no`` plus the address ``type``.

    The key is optional (100 queries a day without it), so it is not required
    here, but the quota is small enough that it is off by default.
    """

    key = "proxycheck"
    label = "proxycheck.io"
    key_env = "PROXYCHECK_KEY"
    url = "https://proxycheck.io/v2/{ip}?vpn=1&asn=1&risk=1"

    def check(self, ctx: IpContext, ip: str) -> Verdict:
        url = self.url.format(ip=quote(ip))
        if self._api_key:
            url = f"{url}&key={quote(self._api_key)}"
        response = ctx.get(url)
        neutral = _neutral(response)
        if neutral is not None:
            return neutral
        payload = response.json()  # type: ignore[union-attr]
        if payload is None:
            return Verdict(UNKNOWN, "no_json")
        if payload.get("status") != "ok":
            message = str(payload.get("message") or payload.get("status") or "error")
            return Verdict(UNKNOWN, message[:60])
        data = payload.get(ip)
        if not isinstance(data, dict):
            data = next((value for value in payload.values() if isinstance(value, dict)), {})
        kind = str(data.get("type") or "")
        if str(data.get("proxy") or "").lower() == "yes" or "vpn" in kind.lower():
            reason = f"proxy={data.get('proxy') or '?'}" + (f" type={kind}" if kind else "")
            return Verdict(FLAGGED, reason)
        return Verdict(CLEAN)


class IpQualityScore(Source):
    """IPQualityScore: ``vpn``/``proxy``/``tor`` plus a general ``fraud_score``."""

    key = "ipqualityscore"
    label = "ipqualityscore.com"
    aliases = ("ipqs",)
    key_env = "IPQUALITYSCORE_KEY"
    requires_key = True
    url = "https://www.ipqualityscore.com/api/json/ip/{key}/{ip}"
    fraud_score_limit = 90

    def check(self, ctx: IpContext, ip: str) -> Verdict:
        if not self._api_key:
            return Verdict(UNKNOWN, "no_api_key")
        url = self.url.format(key=quote(self._api_key), ip=quote(ip))
        response = ctx.get(url)
        neutral = _neutral(response)
        if neutral is not None:
            return neutral
        payload = response.json()  # type: ignore[union-attr]
        if payload is None:
            return Verdict(UNKNOWN, "no_json")
        if payload.get("success") is False:
            message = str(payload.get("message") or "query failed")
            return Verdict(UNKNOWN, message[:60])
        hits = [
            name
            for field, name in (("vpn", "vpn"), ("proxy", "proxy"), ("tor", "tor"))
            if payload.get(field) is True
        ]
        if hits:
            return Verdict(FLAGGED, "+".join(hits))
        score = payload.get("fraud_score")
        if isinstance(score, (int, float)) and score >= self.fraud_score_limit:
            return Verdict(FLAGGED, f"fraud_score={score:g}")
        return Verdict(CLEAN)


class VpnApi(Source):
    """vpnapi.io: the ``security`` block carries ``vpn``/``proxy``/``tor``/``relay``."""

    key = "vpnapi"
    label = "vpnapi.io"
    key_env = "VPNAPI_KEY"
    requires_key = True
    url = "https://vpnapi.io/api/{ip}?key={key}"

    def check(self, ctx: IpContext, ip: str) -> Verdict:
        if not self._api_key:
            return Verdict(UNKNOWN, "no_api_key")
        response = ctx.get(self.url.format(ip=quote(ip), key=quote(self._api_key)))
        neutral = _neutral(response)
        if neutral is not None:
            return neutral
        payload = response.json()  # type: ignore[union-attr]
        if payload is None:
            return Verdict(UNKNOWN, "no_json")
        security = payload.get("security")
        if not isinstance(security, dict) or not security:
            return Verdict(UNKNOWN, str(payload.get("message") or "no_security_block")[:60])
        hits = [
            name
            for field, name in (("vpn", "vpn"), ("proxy", "proxy"), ("tor", "tor"), ("relay", "relay"))
            if security.get(field) is True
        ]
        if hits:
            return Verdict(FLAGGED, "+".join(hits))
        return Verdict(CLEAN)


# Order matters: the keyless sources run first, so a log line always shows what
# the free tier said before the paid confirmations.
SOURCE_CLASSES: tuple[type[Source], ...] = (
    Iplocate,
    IpApi,
    ProxyCheck,
    IpQualityScore,
    VpnApi,
)

# What runs when SOURCES is empty: both keyless sources, no account needed.
DEFAULT_SOURCE_KEYS = ("iplocate", "ipapi")

SOURCE_KEYS = tuple(source.key for source in SOURCE_CLASSES)


def source_by_key(key: str) -> Optional[type[Source]]:
    """Resolve a ``SOURCES`` entry, aliases included."""
    wanted = key.strip().lower()
    for source in SOURCE_CLASSES:
        if wanted == source.key or wanted in source.aliases:
            return source
    return None


def select_sources(keys: Sequence[str], api_keys: Mapping[str, str]) -> list[Source]:
    """Build the ordered source list for the configured keys.

    Raises ``ValueError`` when a keyed source is enabled without its key: that
    misconfiguration would otherwise show up as a permanent ``unknown``.
    """
    chosen: list[Source] = []
    seen: set[str] = set()
    for key in keys:
        source = source_by_key(key)
        if source is None:
            raise ValueError(f"unknown source {key!r}; known: {', '.join(SOURCE_KEYS)}")
        if source.key in seen:
            continue
        api_key = api_keys.get(source.key, "")
        if source.requires_key and not api_key:
            raise ValueError(f"{source.key} needs {source.key_env}")
        seen.add(source.key)
        chosen.append(source(api_key))
    return chosen


def aggregate(verdicts: Mapping[str, Verdict], require_clean: int = 1) -> tuple[str, str]:
    """Fold every source verdict into one status and a human readable reason.

    A single ``flagged`` wins, because one provider recognising a VPN is enough
    to withhold the tag. ``clean`` needs ``require_clean`` confirmations, so a
    strict deployment can demand that every enabled source agrees.
    """
    flagged = [
        f"{key}: {verdict.reason or 'flagged'}"
        for key, verdict in verdicts.items()
        if verdict.status == FLAGGED
    ]
    if flagged:
        return FLAGGED, "; ".join(flagged)
    clean = [key for key, verdict in verdicts.items() if verdict.status == CLEAN]
    if len(clean) >= max(1, require_clean):
        return CLEAN, ""
    unknown = [
        f"{key}: {verdict.reason or 'unknown'}"
        for key, verdict in verdicts.items()
        if verdict.status == UNKNOWN
    ]
    if not unknown:
        # Every source answered clean, just not enough of them.
        unknown = [f"only {len(clean)} clean answer(s), {require_clean} required"]
    return UNKNOWN, "; ".join(unknown)

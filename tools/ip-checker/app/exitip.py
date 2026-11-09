"""Discovery of the node's exit address, i.e. the IP the services actually see.

The address is read through the node itself: a probe to a "what is my IP"
endpoint is the only way to learn what the other side sees, because a node may
rewrite, NAT or round-robin its egress.  Three independent endpoints are tried
so one blocked domain does not turn every node into ``unknown``.
"""

from __future__ import annotations

import ipaddress
import json
from typing import Sequence
from urllib.parse import urlsplit

import requests

DEFAULT_URL = "https://api.ipify.org?format=json"
FALLBACK_URLS = ("https://icanhazip.com", "https://ifconfig.me/ip")

DEFAULT_BODY_LIMIT = 8 * 1024


def is_public_ip(value: str) -> bool:
    """True for a routable unicast address, i.e. one a VPN vendor could judge."""
    try:
        address = ipaddress.ip_address((value or "").strip())
    except ValueError:
        return False
    return not (
        address.is_private
        or address.is_loopback
        or address.is_link_local
        or address.is_multicast
        or address.is_reserved
        or address.is_unspecified
    )


def extract_ip(body: str) -> str:
    """Pull the address out of a JSON (``{"ip": ...}``) or plain text answer."""
    text = (body or "").strip()
    if not text:
        return ""
    if text.startswith("{"):
        try:
            payload = json.loads(text)
        except ValueError:
            return ""
        if not isinstance(payload, dict):
            return ""
        return str(payload.get("ip") or "").strip()
    parts = text.split()
    return parts[0].strip() if parts else ""


def _host(url: str) -> str:
    return urlsplit(url).hostname or url


def discover_exit_ip(
    session: requests.Session,
    timeout: float,
    primary: str = DEFAULT_URL,
    limit: int = DEFAULT_BODY_LIMIT,
) -> tuple[str, str]:
    """Return ``(address, reason)``; the address is empty when every endpoint failed.

    ``reason`` explains the failure and is shown in the log line, so a node whose
    egress cannot answer any endpoint is distinguishable from a node that is
    simply down (that one is dropped by the mihomo reachability check first).
    """
    urls: Sequence[str] = (primary,) if primary in FALLBACK_URLS else (primary, *FALLBACK_URLS)
    reasons: list[str] = []
    for url in urls:
        try:
            response = session.get(
                url,
                timeout=timeout,
                headers={"Accept": "text/plain,application/json", "User-Agent": "ip-checker/1.0"},
            )
        except requests.RequestException as exc:
            message = str(exc).splitlines()[0][:60] if str(exc) else exc.__class__.__name__
            reasons.append(f"{_host(url)}: {message}")
            continue
        if response.status_code >= 400:
            reasons.append(f"{_host(url)}: http_{response.status_code}")
            continue
        candidate = extract_ip((response.text or "")[:limit])
        if candidate and is_public_ip(candidate):
            return candidate, ""
        reasons.append(f"{_host(url)}: not an address ({candidate[:45]!r})")
    return "", "; ".join(reasons) if reasons else "no answer"

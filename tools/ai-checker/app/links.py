"""Helpers for working with proxy URI links.

SublinkPro stores one URI per node (a record may hold several comma separated
links).  mihomo names a proxy after the ``#fragment`` of the URI, so two nodes
sharing a display name would collide inside a provider.  Every link is therefore
re-stamped with a unique, stable name (``n<ID>``) before it reaches mihomo.
"""

from __future__ import annotations

import base64
import json
from urllib.parse import parse_qsl, quote, urlencode


def _b64decode_loose(value: str) -> bytes:
    """Decode base64 that may be urlsafe and/or missing padding."""
    cleaned = value.strip().replace("\n", "").replace("\r", "")
    padded = cleaned + "=" * (-len(cleaned) % 4)
    try:
        return base64.urlsafe_b64decode(padded)
    except Exception:
        return base64.b64decode(padded)


def _b64encode_loose(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def _rewrite_vmess(payload: str, name: str) -> str:
    data = json.loads(_b64decode_loose(payload).decode("utf-8"))
    if not isinstance(data, dict):
        raise ValueError("unexpected vmess payload")
    data["ps"] = name
    encoded = base64.b64encode(json.dumps(data, ensure_ascii=False).encode("utf-8")).decode()
    return "vmess://" + encoded


def _rewrite_ssr(payload: str, name: str) -> str:
    decoded = _b64decode_loose(payload).decode("utf-8")
    body, separator, query = decoded.partition("/?")
    if not separator:
        raise ValueError("unexpected ssr payload")
    params = dict(parse_qsl(query, keep_blank_values=True))
    params["remarks"] = _b64encode_loose(name.encode("utf-8"))
    rebuilt = f"{body}/?{urlencode(params)}"
    return "ssr://" + _b64encode_loose(rebuilt.encode("utf-8"))


def rewrite_link_name(link: str, name: str) -> str:
    """Return ``link`` with its display name replaced by ``name``."""
    link = link.strip()
    lowered = link.lower()
    if lowered.startswith("vmess://"):
        return _rewrite_vmess(link[len("vmess://") :], name)
    if lowered.startswith("ssr://"):
        return _rewrite_ssr(link[len("ssr://") :], name)
    base = link.split("#", 1)[0]
    return f"{base}#{quote(name, safe='')}"


def split_links(raw: str) -> list[str]:
    """Split the comma separated ``Link`` column of a node into separate URIs."""
    return [part.strip() for part in (raw or "").split(",") if part.strip()]

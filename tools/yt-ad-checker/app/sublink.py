"""Minimal client for the SublinkPro REST API (``X-API-Key`` authentication)."""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any, Iterable, Optional

import requests

from .links import split_links

LOG = logging.getLogger("sublink")


class SublinkError(RuntimeError):
    """Raised when SublinkPro answers with an error envelope."""


@dataclass(frozen=True)
class NodeRef:
    """A node as reported by ``GET /api/v1/nodes/get``."""

    id: int
    name: str
    group: str
    protocol: str
    tags: tuple[str, ...]
    links: tuple[str, ...]

    def has_tag(self, tag: str) -> bool:
        return tag in self.tags


class SublinkClient:
    def __init__(self, base_url: str, api_key: str, timeout: float = 30.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._timeout = timeout
        self._session = requests.Session()
        self._session.headers.update(
            {
                "X-API-Key": api_key,
                "Accept": "application/json",
                "User-Agent": "yt-ad-checker/1.0",
            }
        )

    def close(self) -> None:
        self._session.close()

    # ------------------------------------------------------------------ core

    def _call(self, method: str, path: str, payload: Optional[dict[str, Any]] = None) -> Any:
        url = f"{self._base_url}{path}"
        try:
            response = self._session.request(method, url, json=payload, timeout=self._timeout)
        except requests.RequestException as exc:
            raise SublinkError(f"{method} {path} failed: {exc}") from exc

        if response.status_code in (401, 403):
            raise SublinkError(f"{method} {path} rejected the API key (HTTP {response.status_code})")
        try:
            body = response.json()
        except ValueError as exc:
            raise SublinkError(
                f"{method} {path} returned non-JSON (HTTP {response.status_code})"
            ) from exc

        # SublinkPro reports failures with HTTP 200 and code 500 in the envelope.
        if body.get("code") != 200:
            raise SublinkError(f"{method} {path} failed: {body.get('msg') or body.get('code')}")
        return body.get("data")

    # ----------------------------------------------------------------- nodes

    def list_nodes(self) -> list[NodeRef]:
        data = self._call("GET", "/api/v1/nodes/get") or []
        nodes: list[NodeRef] = []
        for raw in data:
            node_id = raw.get("ID")
            links = split_links(raw.get("Link") or "")
            if node_id is None or not links:
                continue
            tags = tuple(t.strip() for t in (raw.get("Tags") or "").split(",") if t.strip())
            nodes.append(
                NodeRef(
                    id=int(node_id),
                    name=raw.get("EffectiveName") or raw.get("Name") or raw.get("LinkName") or f"#{node_id}",
                    group=raw.get("Group") or "",
                    protocol=raw.get("Protocol") or "",
                    tags=tags,
                    links=tuple(links),
                )
            )
        return nodes

    # ------------------------------------------------------------------ tags

    def tag_exists(self, name: str) -> bool:
        tags = self._call("GET", "/api/v1/tags/list") or []
        return any((tag or {}).get("name") == name for tag in tags)

    def ensure_tag(self, name: str, color: str = "#1976d2") -> bool:
        """Create the tag when missing. Returns True when it had to be created."""
        if self.tag_exists(name):
            return False
        try:
            self._call("POST", "/api/v1/tags/add", {"name": name, "color": color})
        except SublinkError as exc:
            # A concurrent run may have created it in the meantime.
            if self.tag_exists(name):
                return False
            raise exc
        LOG.info("created tag %r", name)
        return True

    def add_tag(self, node_ids: Iterable[int], tag: str) -> None:
        payload = {"nodeIds": list(node_ids), "tagName": tag}
        self._call("POST", "/api/v1/tags/node/batch-add", payload)

    def remove_tag(self, node_ids: Iterable[int], tag: str) -> None:
        # Only the given tag is stripped; every other tag on the node survives.
        payload = {"nodeIds": list(node_ids), "tagNames": [tag]}
        self._call("POST", "/api/v1/tags/node/batch-remove", payload)

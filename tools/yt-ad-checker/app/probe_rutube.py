"""Detect whether Rutube schedules video ads for the currently selected node.

Rutube decides the ad schedule server-side and ships it inside the play
options of every video: ``advert`` lists the preroll and midroll slots that the
requesting IP would receive, and ``cuepoints`` marks midroll insert points. A
node whose exit looks ad-free to Rutube gets an empty list, so one JSON request
per video is enough - no browser and no DRM playback.

A single video can be demonetized for everyone, so several videos are checked:
``ads`` as soon as one of them schedules a slot, ``clean`` only when at least
two answered and none scheduled anything, ``unknown`` when fewer answered.
"""

from __future__ import annotations

import logging
import time
from typing import Any, Mapping, Optional

import requests

from .config import Settings
from .probe import ADS, CLEAN, UNKNOWN, UNREACHABLE, ProbeResult

PLAY_OPTIONS_URL = "https://rutube.ru/api/play/options/{video_id}/?no_404=true"
HEADERS = {
    "Accept": "application/json",
    "Accept-Language": "ru-RU,ru;q=0.9",
    "Referer": "https://rutube.ru/",
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
    ),
}
# A clean verdict needs at least this many answered videos, otherwise a node
# that silently drops requests would look ad-free.
MIN_CLEAN_ANSWERS = 2
# Rutube answers in about a second; anything slower is a sick node. Without
# these caps a dead node burns the whole PROBE_TIMEOUT budget (45s) and one
# cycle over a hundred nodes takes half an hour.
CONNECT_TIMEOUT = 6.0
READ_TIMEOUT = 10.0


def ad_slots(payload: Mapping[str, Any]) -> list[str]:
    """Names of the ad slots Rutube would play for this client."""
    slots: list[str] = []
    for entry in payload.get("advert") or []:
        if not isinstance(entry, dict):
            continue
        try:
            count = int(entry.get("count") or 0)
        except (TypeError, ValueError):
            count = 0
        if count <= 0:
            continue
        slots.append(str(entry.get("name") or "ad"))
    if payload.get("cuepoints"):
        slots.append("cuepoints")
    return slots


class RutubeProbe:
    """HTTP-only probe; ``start``/``close`` mirror the YouTube probe API."""

    def __init__(self, settings: Settings, logger: Optional[logging.Logger] = None) -> None:
        self._settings = settings
        self._log = logger or logging.getLogger("probe")
        self._session: Optional[requests.Session] = None

    # ------------------------------------------------------------- lifecycle

    def start(self) -> None:
        session = requests.Session()
        session.headers.update(HEADERS)
        proxy = self._settings.mihomo_proxy_url
        # trust_env would let an ambient HTTP(S)_PROXY bypass mihomo.
        session.trust_env = False
        session.proxies.update({"http": proxy, "https": proxy})
        self._session = session

    def close(self) -> None:
        if self._session is not None:
            self._session.close()
            self._session = None

    # ----------------------------------------------------------------- probe

    def probe(self, timeout: Optional[float] = None) -> ProbeResult:
        if self._session is None:
            raise RuntimeError("RutubeProbe.start() must be called first")

        budget = float(timeout or self._settings.probe_timeout)
        deadline = time.monotonic() + budget
        started = time.monotonic()
        answered = 0
        errors: list[str] = []

        for video_id in self._settings.rutube_videos:
            remaining = deadline - time.monotonic()
            if remaining <= 1.0:
                errors.append("probe timeout")
                break
            try:
                payload = self._fetch(video_id, remaining)
            except requests.RequestException as exc:
                errors.append(self._short(exc))
                self._log.debug("rutube video %s failed: %s", video_id[:8], exc)
                if answered == 0:
                    # The node cannot reach Rutube at all: the remaining videos
                    # would fail the same way and only waste the budget.
                    break
                continue
            except Exception as exc:  # noqa: BLE001 - one bad video must not stop the probe
                errors.append(self._short(exc))
                self._log.debug("rutube video %s failed: %s", video_id[:8], exc)
                continue

            answered += 1
            slots = ad_slots(payload)
            if slots:
                result = ProbeResult(
                    ADS,
                    detail=f"{video_id[:8]} {', '.join(slots[:4])}",
                    signals=tuple(slots[:4]),
                    elapsed=time.monotonic() - started,
                )
                self._log.debug("probe finished: %s", result)
                return result

        elapsed = time.monotonic() - started
        if answered == 0:
            return ProbeResult(UNREACHABLE, errors[0] if errors else "no response", elapsed=elapsed)
        required = min(MIN_CLEAN_ANSWERS, len(self._settings.rutube_videos))
        if answered < required:
            return ProbeResult(
                UNKNOWN, f"only {answered} video(s) answered, {errors[0] if errors else ''}".strip(), elapsed=elapsed
            )
        return ProbeResult(CLEAN, f"{answered} video(s) without ad slots", elapsed=elapsed)

    # -------------------------------------------------------------- internals

    def _fetch(self, video_id: str, timeout: float) -> Mapping[str, Any]:
        url = PLAY_OPTIONS_URL.format(video_id=video_id)
        request_timeout = (min(CONNECT_TIMEOUT, timeout), min(READ_TIMEOUT, timeout))
        response = self._session.get(url, timeout=request_timeout)
        if response.status_code >= 400:
            raise RuntimeError(f"HTTP {response.status_code}")
        try:
            payload = response.json()
        except ValueError as exc:
            raise RuntimeError("non-JSON response") from exc
        if not isinstance(payload, dict):
            raise RuntimeError("unexpected response")
        # Geo-blocked or removed videos answer without playback data; counting
        # them as "answered" would turn a blocked node into a clean one.
        if not payload.get("video_balancer") and not payload.get("duration"):
            raise RuntimeError("video unavailable")
        return payload

    @staticmethod
    def _short(exc: BaseException) -> str:
        message = str(exc).splitlines()[0] if str(exc) else exc.__class__.__name__
        return message[:120]

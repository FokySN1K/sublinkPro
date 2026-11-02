"""Environment driven configuration for the YouTube ad checker."""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from urllib.parse import urlsplit

_INTERVAL_UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400}
_INTERVAL_PART = re.compile(r"(\d+(?:\.\d+)?)([smhd])")

# A single video can be demonetized (for one country or for everyone), which
# would report every node as ad-free. The first entry is probed in full, the
# rest are only checked for an ad schedule, so extra videos stay cheap.
DEFAULT_YOUTUBE_VIDEOS = (
    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
    "https://www.youtube.com/watch?v=9bZkp7q19f0",
    "https://www.youtube.com/watch?v=kJQP7kiw5Fk",
)

SERVICE_YOUTUBE = "youtube"
SERVICE_RUTUBE = "rutube"
SERVICES = (SERVICE_YOUTUBE, SERVICE_RUTUBE)

# Every worker owns a mihomo process and a Chromium instance, so the ceiling is
# about RAM and ports rather than about the API.
MAX_WORKERS = 8

# Rutube answers /api/play/options with the ad schedule the requesting IP would
# get. These videos are monetized, so an empty schedule means "no ads here".
DEFAULT_RUTUBE_VIDEOS = (
    "eab0fc4ce26d9ca7f60a6515d27bf029",
    "7f7bb1bce64c534f1343226ac10d7806",
    "8886deb27b3566a6331e7041e3e0c0a2",
    "1a138b949ab8b57d16799b4c79356844",
    "762096980c26ff20c86434e254f9825e",
)


def parse_interval(raw: str) -> float:
    """Parse ``90``, ``90s``, ``15m``, ``6h``, ``1h30m`` or ``1d`` into seconds."""
    text = raw.strip().lower()
    if not text:
        raise ValueError("interval is empty")
    if re.fullmatch(r"\d+(?:\.\d+)?", text):
        seconds = float(text)
        if seconds <= 0:
            raise ValueError(f"interval must be positive: {raw!r}")
        return seconds

    compact = re.sub(r"\s+", "", text)
    total = 0.0
    position = 0
    for match in _INTERVAL_PART.finditer(compact):
        if match.start() != position:
            raise ValueError(f"cannot parse interval: {raw!r}")
        total += float(match.group(1)) * _INTERVAL_UNITS[match.group(2)]
        position = match.end()
    if position != len(compact) or total <= 0:
        raise ValueError(f"cannot parse interval: {raw!r}")
    return total


def _env_str(name: str, default: str) -> str:
    raw = os.getenv(name)
    if raw is None or raw.strip() == "":
        return default
    return raw.strip()


def _env_bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None or raw.strip() == "":
        return default
    return raw.strip().lower() in {"1", "true", "yes", "y", "on"}


def _env_float(name: str, default: float) -> float:
    raw = os.getenv(name)
    if raw is None or raw.strip() == "":
        return default
    return float(raw.strip())


def _env_int(name: str, default: int) -> int:
    raw = os.getenv(name)
    if raw is None or raw.strip() == "":
        return default
    return int(raw.strip())


def _env_list(name: str) -> tuple[str, ...]:
    """A comma or whitespace separated list; empty when the variable is unset."""
    return tuple(part for part in re.split(r"[,\s]+", _env_str(name, "")) if part)


def watch_url(entry: str) -> str:
    """Accept a bare video id, ``watch?v=…`` or a full URL."""
    text = entry.strip()
    if not text:
        raise ValueError("empty video entry")
    if text.startswith(("http://", "https://")):
        if not urlsplit(text).netloc:
            raise ValueError(f"not a video URL: {entry!r}")
        return text
    if text.startswith("watch?"):
        return f"https://www.youtube.com/{text}"
    return f"https://www.youtube.com/watch?v={text}"


def _env_videos(name: str, default: tuple[str, ...], convert=watch_url) -> tuple[str, ...]:
    raw = _env_str(name, "")
    entries = [part for part in re.split(r"[,\s]+", raw) if part]
    if not entries:
        return default
    return tuple(convert(entry) for entry in entries)


@dataclass(frozen=True)
class Settings:
    """All knobs exposed to the container, resolved once at start-up."""

    sublink_url: str
    sublink_api_key: str
    service: str
    clean_tag: str
    ads_tag: str
    rutube_videos: tuple[str, ...]
    interval: float
    run_on_start: bool
    once: bool
    only_tags: tuple[str, ...]
    skip_tags: tuple[str, ...]
    max_nodes: int
    workers: int
    ensure_tag: bool
    dry_run: bool
    probe_urls: tuple[str, ...]
    probe_timeout: float
    probe_dwell: float
    headless: bool
    state_dir: str
    mihomo_bin: str
    mihomo_api: str
    mihomo_proxy_port: int
    log_level: str

    @classmethod
    def from_env(cls) -> "Settings":
        api_key = _env_str("SUBLINK_API_KEY", "")
        if not api_key:
            raise ValueError("SUBLINK_API_KEY is required")

        service = _env_str("SERVICE", SERVICE_YOUTUBE).lower()
        if service not in SERVICES:
            raise ValueError(f"SERVICE must be one of {', '.join(SERVICES)}")

        # The two tags are mutually exclusive per node, so sharing a name would
        # make every write cancel itself out.
        if service == SERVICE_RUTUBE:
            clean_tag = _env_str("RUTUBE_TAG", "rutube")
            ads_tag = _env_str("RUTUBE_ADS_TAG", "rutube-ads")
        else:
            clean_tag = _env_str("YOUTUBE_TAG", "youtube")
            # ADS_TAG predates the per-service split and still works.
            ads_tag = _env_str("YOUTUBE_ADS_TAG", _env_str("ADS_TAG", "youtube-ads"))
        if clean_tag == ads_tag:
            raise ValueError(f"{service} tags must differ, got {clean_tag!r}")

        raw_videos = _env_str("RUTUBE_VIDEOS", "")
        rutube_videos = tuple(v.strip() for v in raw_videos.split(",") if v.strip())
        if not rutube_videos:
            rutube_videos = DEFAULT_RUTUBE_VIDEOS

        workers = _env_int("WORKERS", 1)
        if workers < 1:
            raise ValueError("WORKERS must be at least 1")
        if workers > MAX_WORKERS:
            raise ValueError(f"WORKERS must not exceed {MAX_WORKERS}")

        max_nodes = _env_int("MAX_NODES", 0)
        if max_nodes < 0:
            raise ValueError("MAX_NODES must not be negative")

        return cls(
            sublink_url=_env_str("SUBLINK_URL", "http://host.docker.internal:8000").rstrip("/"),
            sublink_api_key=api_key,
            service=service,
            clean_tag=clean_tag,
            ads_tag=ads_tag,
            rutube_videos=rutube_videos,
            interval=parse_interval(_env_str("CHECK_INTERVAL", "6h")),
            run_on_start=_env_bool("RUN_ON_START", True),
            once=_env_bool("ONCE", False),
            only_tags=_env_list("ONLY_TAGS"),
            skip_tags=_env_list("SKIP_TAGS"),
            max_nodes=max_nodes,
            workers=workers,
            ensure_tag=_env_bool("ENSURE_TAG", True),
            dry_run=_env_bool("DRY_RUN", False),
            probe_urls=_env_videos("YOUTUBE_VIDEOS", _env_videos("PROBE_URL", DEFAULT_YOUTUBE_VIDEOS)),
            probe_timeout=_env_float("PROBE_TIMEOUT", 45.0),
            probe_dwell=_env_float("PROBE_DWELL", 10.0),
            headless=_env_bool("HEADLESS", False),
            state_dir=_env_str("STATE_DIR", "/data"),
            mihomo_bin=_env_str("MIHOMO_BIN", "mihomo"),
            mihomo_api=_env_str("MIHOMO_API", "http://127.0.0.1:19090").rstrip("/"),
            mihomo_proxy_port=_env_int("MIHOMO_PROXY_PORT", 17890),
            log_level=_env_str("LOG_LEVEL", "info").upper(),
        )

    @property
    def mihomo_proxy_url(self) -> str:
        return f"http://127.0.0.1:{self.mihomo_proxy_port}"

    @property
    def probe_url(self) -> str:
        """The video that gets the full probe; the rest are quick checks."""
        return self.probe_urls[0]

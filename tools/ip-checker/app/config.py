"""Environment driven configuration for the IP reputation checker."""

from __future__ import annotations

import os
import re
from dataclasses import dataclass

from .sources import DEFAULT_SOURCE_KEYS, SOURCE_KEYS, source_by_key

_INTERVAL_UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400}
_INTERVAL_PART = re.compile(r"(\d+(?:\.\d+)?)([smhd])")

# Every worker owns a mihomo process, so the ceiling is about RAM and ports.
MAX_WORKERS = 8

USER_AGENT = "ip-checker/1.0"
TAG_COLOR = "#2e7d32"


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


def parse_sources(raw: str) -> tuple[str, ...]:
    """Resolve the ``SOURCES`` list; an empty value means both keyless sources."""
    entries = [part for part in re.split(r"[,\s]+", raw.strip()) if part]
    if not entries:
        return tuple(DEFAULT_SOURCE_KEYS)
    resolved: list[str] = []
    for entry in entries:
        source = source_by_key(entry)
        if source is None:
            raise ValueError(f"unknown source {entry!r}; known: {', '.join(SOURCE_KEYS)}")
        if source.key not in resolved:
            resolved.append(source.key)
    return tuple(resolved)


@dataclass(frozen=True)
class Settings:
    """All knobs exposed to the container, resolved once at start-up."""

    sublink_url: str
    sublink_api_key: str
    sources: tuple[str, ...]
    iplocate_api_key: str
    proxycheck_key: str
    ipqualityscore_key: str
    vpnapi_key: str
    clean_tag: str
    interval: float
    run_on_start: bool
    once: bool
    only_tags: tuple[str, ...]
    skip_tags: tuple[str, ...]
    max_nodes: int
    workers: int
    ensure_tag: bool
    dry_run: bool
    require_clean: int
    probe_timeout: float
    exit_ip_url: str
    reachability_url: str
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

        workers = _env_int("WORKERS", 1)
        if workers < 1:
            raise ValueError("WORKERS must be at least 1")
        if workers > MAX_WORKERS:
            raise ValueError(f"WORKERS must not exceed {MAX_WORKERS}")

        max_nodes = _env_int("MAX_NODES", 0)
        if max_nodes < 0:
            raise ValueError("MAX_NODES must not be negative")

        require_clean = _env_int("REQUIRE_CLEAN", 1)
        if require_clean < 1:
            raise ValueError("REQUIRE_CLEAN must be at least 1")

        probe_timeout = _env_float("PROBE_TIMEOUT", 15.0)
        if probe_timeout <= 0:
            raise ValueError("PROBE_TIMEOUT must be positive")

        return cls(
            sublink_url=_env_str("SUBLINK_URL", "http://host.docker.internal:8000").rstrip("/"),
            sublink_api_key=api_key,
            sources=parse_sources(_env_str("SOURCES", "")),
            iplocate_api_key=_env_str("IPLOCATE_API_KEY", ""),
            proxycheck_key=_env_str("PROXYCHECK_KEY", ""),
            ipqualityscore_key=_env_str("IPQUALITYSCORE_KEY", ""),
            vpnapi_key=_env_str("VPNAPI_KEY", ""),
            clean_tag=_env_str("CLEAN_TAG", "clean-ip"),
            # Reputation changes far slower than AI availability, and the keyless
            # sources have daily quotas, so one pass a day is plenty.
            interval=parse_interval(_env_str("CHECK_INTERVAL", "24h")),
            run_on_start=_env_bool("RUN_ON_START", True),
            once=_env_bool("ONCE", False),
            only_tags=_env_list("ONLY_TAGS"),
            skip_tags=_env_list("SKIP_TAGS"),
            max_nodes=max_nodes,
            workers=workers,
            ensure_tag=_env_bool("ENSURE_TAG", True),
            dry_run=_env_bool("DRY_RUN", False),
            require_clean=require_clean,
            probe_timeout=probe_timeout,
            exit_ip_url=_env_str("EXIT_IP_URL", "https://api.ipify.org?format=json"),
            # A request that proves the node can carry traffic at all: mihomo
            # dials it directly, so a dead node costs seconds instead of one
            # PROBE_TIMEOUT per request.
            reachability_url=_env_str("REACHABILITY_URL", "https://www.gstatic.com/generate_204"),
            state_dir=_env_str("STATE_DIR", "/data"),
            mihomo_bin=_env_str("MIHOMO_BIN", "mihomo"),
            mihomo_api=_env_str("MIHOMO_API", "http://127.0.0.1:19090").rstrip("/"),
            mihomo_proxy_port=_env_int("MIHOMO_PROXY_PORT", 17890),
            log_level=_env_str("LOG_LEVEL", "info").upper(),
        )

    # ------------------------------------------------------------------- keys

    @property
    def source_api_keys(self) -> dict[str, str]:
        return {
            "iplocate": self.iplocate_api_key,
            "proxycheck": self.proxycheck_key,
            "ipqualityscore": self.ipqualityscore_key,
            "vpnapi": self.vpnapi_key,
        }

    @property
    def mihomo_proxy_url(self) -> str:
        return f"http://127.0.0.1:{self.mihomo_proxy_port}"

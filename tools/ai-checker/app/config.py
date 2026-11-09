"""Environment driven configuration for the AI availability checker."""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from typing import Optional

from .providers import ALL_PROVIDERS, PROVIDER_KEYS, provider_by_key

_INTERVAL_UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400}
_INTERVAL_PART = re.compile(r"(\d+(?:\.\d+)?)([smhd])")

# Every worker owns a mihomo process, so the ceiling is about RAM and ports.
MAX_WORKERS = 8

# Headless HTTP probes still need a browser-ish UA: several AI services answer
# a bare ``python-requests`` UA with a Cloudflare challenge or a 403.
DEFAULT_USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

TAG_OK_COLOR = "#2e7d32"
TAG_BLOCKED_COLOR = "#c62828"


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


def parse_services(raw: str) -> tuple[str, ...]:
    """Resolve the ``SERVICES`` list; an empty value means every provider."""
    entries = [part for part in re.split(r"[,\s]+", raw.strip()) if part]
    if not entries:
        return tuple(PROVIDER_KEYS)
    resolved: list[str] = []
    for entry in entries:
        key = entry.strip().lower()
        provider = provider_by_key(key)
        if provider is None:
            raise ValueError(
                f"unknown AI service {entry!r}; known: {', '.join(p.key for p in ALL_PROVIDERS)}"
            )
        if provider.key not in resolved:
            resolved.append(provider.key)
    return tuple(resolved)


@dataclass(frozen=True)
class Settings:
    """All knobs exposed to the container, resolved once at start-up."""

    sublink_url: str
    sublink_api_key: str
    services: tuple[str, ...]
    tag_prefix: str
    interval: float
    run_on_start: bool
    once: bool
    only_tags: tuple[str, ...]
    skip_tags: tuple[str, ...]
    max_nodes: int
    workers: int
    ensure_tag: bool
    dry_run: bool
    probe_timeout: float
    user_agent: str
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

        probe_timeout = _env_float("PROBE_TIMEOUT", 15.0)
        if probe_timeout <= 0:
            raise ValueError("PROBE_TIMEOUT must be positive")

        return cls(
            sublink_url=_env_str("SUBLINK_URL", "http://host.docker.internal:8000").rstrip("/"),
            sublink_api_key=api_key,
            services=parse_services(_env_str("SERVICES", "")),
            tag_prefix=_env_str("TAG_PREFIX", "ai-"),
            interval=parse_interval(_env_str("CHECK_INTERVAL", "6h")),
            run_on_start=_env_bool("RUN_ON_START", True),
            once=_env_bool("ONCE", False),
            only_tags=_env_list("ONLY_TAGS"),
            skip_tags=_env_list("SKIP_TAGS"),
            max_nodes=max_nodes,
            workers=workers,
            ensure_tag=_env_bool("ENSURE_TAG", True),
            dry_run=_env_bool("DRY_RUN", False),
            probe_timeout=probe_timeout,
            user_agent=_env_str("PROBE_USER_AGENT", DEFAULT_USER_AGENT),
            # A request that proves the node can carry traffic at all: mihomo
            # dials it directly, so a dead node costs seconds instead of one
            # PROBE_TIMEOUT per service.
            reachability_url=_env_str("REACHABILITY_URL", "https://www.gstatic.com/generate_204"),
            state_dir=_env_str("STATE_DIR", "/data"),
            mihomo_bin=_env_str("MIHOMO_BIN", "mihomo"),
            mihomo_api=_env_str("MIHOMO_API", "http://127.0.0.1:19090").rstrip("/"),
            mihomo_proxy_port=_env_int("MIHOMO_PROXY_PORT", 17890),
            log_level=_env_str("LOG_LEVEL", "info").upper(),
        )

    # ------------------------------------------------------------------- tags

    def available_tag(self, service: str) -> str:
        return f"{self.tag_prefix}{service}"

    def blocked_tag(self, service: str) -> str:
        return f"{self.tag_prefix}{service}-blocked"

    def service_label(self, service: str) -> str:
        provider = provider_by_key(service)
        return provider.label if provider else service

    @property
    def mihomo_proxy_url(self) -> str:
        return f"http://127.0.0.1:{self.mihomo_proxy_port}"

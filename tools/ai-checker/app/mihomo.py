"""A private mihomo instance used to route probes through a single node at a time."""

from __future__ import annotations

import collections
import logging
import os
import subprocess
import threading
import time
from typing import Any, Optional, Sequence
from urllib.parse import urlencode, urlsplit

import requests

from .config import Settings

CONFIG_TEMPLATE = """\
mixed-port: {proxy_port}
allow-lan: false
mode: rule
log-level: warning
ipv6: false
external-controller: 127.0.0.1:{api_port}
unified-delay: true
tcp-concurrent: true

dns:
  enable: true
  ipv6: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  default-nameserver:
    - 223.5.5.5
    - 1.1.1.1
  nameserver:
    - 223.5.5.5
    - 1.1.1.1

proxy-providers:
  nodes:
    type: file
    path: ./source/nodes.txt
    health-check:
      enable: false

proxy-groups:
  - name: PROBE
    type: select
    use:
      - nodes

rules:
  - MATCH,PROBE
"""

PROVIDER_NAME = "nodes"
GROUP_NAME = "PROBE"
# mihomo dials the node itself, so a dead node costs these seconds here instead
# of the whole PROBE_TIMEOUT inside the HTTP probe.
REACHABILITY_TIMEOUT_MS = 6000


class MihomoError(RuntimeError):
    """Raised when the mihomo process or its API misbehaves."""


def _short(exc: BaseException) -> str:
    message = str(exc).splitlines()[0] if str(exc) else exc.__class__.__name__
    return message[:120]


class MihomoRunner:
    """Owns config generation, the mihomo process and node switching."""

    def __init__(self, settings: Settings, logger: Optional[logging.Logger] = None) -> None:
        self._settings = settings
        self._log = logger or logging.getLogger("mihomo")
        self._home = os.path.join(settings.state_dir, "mihomo")
        self._source_dir = os.path.join(self._home, "source")
        self._proxies: list[tuple[str, str]] = []
        self._process: Optional[subprocess.Popen[str]] = None
        self._log_tail: collections.deque[str] = collections.deque(maxlen=40)

    # ------------------------------------------------------------- lifecycle

    def set_nodes(self, proxies: Sequence[tuple[str, str]]) -> None:
        """Publish the proxy list that mihomo should expose through its provider."""
        if not proxies:
            raise MihomoError("refusing to start mihomo without proxies")
        self._proxies = list(proxies)
        os.makedirs(self._source_dir, exist_ok=True)
        target = os.path.join(self._source_dir, "nodes.txt")
        tmp = target + ".tmp"
        with open(tmp, "w", encoding="utf-8") as handle:
            for _name, link in proxies:
                handle.write(link.strip() + "\n")
        os.replace(tmp, target)

    @property
    def is_running(self) -> bool:
        return self._process is not None and self._process.poll() is None

    def start(self) -> None:
        self._write_config()
        self._spawn()
        self._wait_api()

    def stop(self) -> None:
        process, self._process = self._process, None
        if process is None or process.poll() is not None:
            return
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self._log.warning("mihomo did not stop in time, killing it")
            process.kill()
            process.wait(timeout=5)

    def restart(self) -> None:
        self._log.warning("restarting mihomo")
        self.stop()
        self._spawn()
        self._wait_api()

    def close(self) -> None:
        self.stop()

    # ---------------------------------------------------------------- config

    def _write_config(self) -> None:
        os.makedirs(self._home, exist_ok=True)
        os.makedirs(self._source_dir, exist_ok=True)
        api_port = urlsplit(self._settings.mihomo_api).port or 19090
        rendered = CONFIG_TEMPLATE.format(
            proxy_port=self._settings.mihomo_proxy_port,
            api_port=api_port,
        )
        with open(os.path.join(self._home, "config.yaml"), "w", encoding="utf-8") as handle:
            handle.write(rendered)

    # --------------------------------------------------------------- process

    def _spawn(self) -> None:
        os.makedirs(self._home, exist_ok=True)
        self._log_tail.clear()
        self._process = subprocess.Popen(
            [self._settings.mihomo_bin, "-d", self._home],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        threading.Thread(target=self._pump_logs, name="mihomo-log", daemon=True).start()

    def _pump_logs(self) -> None:
        process = self._process
        stream = process.stdout if process is not None else None
        if stream is None:
            return
        for line in iter(stream.readline, ""):
            stripped = line.rstrip()
            if not stripped:
                continue
            self._log_tail.append(stripped)
            self._log.debug("mihomo: %s", stripped)

    def _wait_api(self, timeout: float = 30.0) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            process = self._process
            if process is not None and process.poll() is not None:
                raise MihomoError("mihomo exited during start-up:\n" + "\n".join(self._log_tail))
            try:
                response = requests.get(f"{self._settings.mihomo_api}/version", timeout=2)
                if response.status_code == 200:
                    self._log.debug("mihomo API is ready: %s", response.text[:120])
                    return
            except requests.RequestException:
                pass
            time.sleep(0.3)
        raise MihomoError("mihomo API did not become ready:\n" + "\n".join(self._log_tail))

    # -------------------------------------------------------------------- api

    def _api(
        self,
        method: str,
        path: str,
        payload: Optional[dict[str, Any]] = None,
        timeout: float = 15.0,
    ) -> Any:
        url = f"{self._settings.mihomo_api}{path}"
        try:
            response = requests.request(method, url, json=payload, timeout=timeout)
        except requests.RequestException as exc:
            raise MihomoError(f"{method} {path} failed: {exc}") from exc
        if response.status_code >= 400:
            raise MihomoError(f"{method} {path} -> HTTP {response.status_code}: {response.text[:200]}")
        if not response.content:
            return None
        try:
            return response.json()
        except ValueError:
            return None

    def provider_names(self) -> set[str]:
        data = self._api("GET", f"/providers/proxies/{PROVIDER_NAME}") or {}
        names: set[str] = set()
        for item in data.get("proxies") or []:
            if isinstance(item, dict):
                name = item.get("name")
                if name:
                    names.add(str(name))
            elif isinstance(item, str):
                names.add(item)
        return names

    def apply_provider(self, timeout: float = 45.0) -> None:
        """Re-download the provider and verify every expected proxy is present."""
        expected = {name for name, _link in self._proxies}
        for attempt in range(1, 4):
            try:
                self._api("PUT", f"/providers/proxies/{PROVIDER_NAME}", timeout=30)
            except MihomoError as exc:
                # The local provider server occasionally answers the very first
                # request with EOF, so a single failure is not fatal.
                self._log.warning("provider refresh failed (attempt %d): %s", attempt, exc)
                time.sleep(1.0)
                continue
            if self._wait_provider(expected, min(timeout, 20.0)):
                self._log.debug("provider refreshed with %d proxies", len(expected))
                return
            self._log.warning(
                "provider returned %d of %d proxies (attempt %d)",
                len(self.provider_names()),
                len(expected),
                attempt,
            )
            time.sleep(1.0)
        self.restart()
        if not self._wait_provider(expected, timeout):
            missing = expected - self.provider_names()
            preview = ", ".join(sorted(missing)[:5])
            raise MihomoError(
                f"provider is missing {len(missing)} proxies after restart (e.g. {preview})"
            )
        self._log.debug("provider loaded with %d proxies after restart", len(expected))

    def _wait_provider(self, expected: set[str], timeout: float) -> bool:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                if self.provider_names() == expected:
                    return True
            except MihomoError:
                pass
            time.sleep(0.4)
        return False

    def select(self, name: str) -> None:
        self._api("PUT", f"/proxies/{GROUP_NAME}", payload={"name": name}, timeout=10)
        data = self._api("GET", f"/proxies/{GROUP_NAME}", timeout=10) or {}
        current = data.get("now")
        if current != name:
            # An empty selector silently falls back to DIRECT, which would make
            # every probe measure the host's own connection instead of the node.
            raise MihomoError(f"selector stayed on {current!r} instead of {name!r}")

    def clear_connections(self) -> None:
        """Drop lingering sockets so the next probe cannot reuse the previous node."""
        try:
            self._api("DELETE", "/connections", timeout=10)
        except MihomoError as exc:
            self._log.debug("could not clear connections: %s", exc)

    def reachable(self, url: str, timeout_ms: int = REACHABILITY_TIMEOUT_MS) -> Optional[str]:
        """Request ``url`` through the selected node; a failure reason or ``None``.

        Run right after :meth:`select`: mihomo dials the node itself, so a node
        that cannot establish a connection is skipped in seconds instead of
        burning the whole probe budget. Any answer proves the connection works.
        Provider nodes are reachable only through the selector, hence its name.
        """
        query = urlencode({"url": url, "timeout": timeout_ms})
        path = f"/proxies/{GROUP_NAME}/delay?{query}"
        try:
            response = requests.get(f"{self._settings.mihomo_api}{path}", timeout=timeout_ms / 1000 + 5)
        except requests.RequestException as exc:
            return _short(exc)
        try:
            payload: Any = response.json()
        except ValueError:
            payload = {}
        delay = payload.get("delay") if isinstance(payload, dict) else None
        if response.status_code == 200 and delay:
            return None
        message = payload.get("message") if isinstance(payload, dict) else None
        if not message or "error occurred" in str(message).lower():
            # mihomo hides the real dial error (timeout, reset, refused) behind
            # this generic 503 text.
            return f"no answer in {timeout_ms / 1000:g}s"
        return str(message)

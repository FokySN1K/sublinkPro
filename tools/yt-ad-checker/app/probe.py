"""Detect whether YouTube serves ads through the currently selected node.

Four independent signals are collected on every probe:

* ``adPlacements`` / ``playerAds`` inside the player responses (initial and live),
* the ``.ad-showing`` class and ad overlay nodes on the player element,
* requests to real ad creative hosts (never the ``req=9`` detection pixels),
* ``playabilityStatus`` so a broken player is reported as ``unknown`` instead of
  being mistaken for an ad-free node.
"""

from __future__ import annotations

import logging
import re
import time
from dataclasses import dataclass, field
from typing import Any, Optional
from urllib.parse import parse_qs, urlsplit

from playwright.sync_api import Error as PlaywrightError
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError

from .config import Settings

CLEAN = "clean"
ADS = "ads"
UNREACHABLE = "unreachable"
UNKNOWN = "unknown"

# Only real creative deliveries count. YouTube also beacons
# ``googleads.g.doubleclick.net/pagead/id`` on ad-free playback (ad-block
# detection), so matching the whole host produces false "ads" verdicts.
AD_CREATIVE_RE = re.compile(
    r"googleads\.g\.doubleclick\.net/pagead/(?:adview|interaction|lvz)\b"
    r"|(?:s\d+\.)?2mdn\.net/"
    r"|\bgooglevideo\.com/videoplayback\?[^\s]*\badformat=",
    re.IGNORECASE,
)

PLAYER_STATE_JS = """() => {
  const player = document.getElementById('movie_player');
  const pick = (pr) => {
    if (!pr) return { status: '', reason: '', placements: 0, playerAds: false };
    const ps = pr.playabilityStatus || {};
    return {
      status: ps.status || '',
      reason: ps.reason || '',
      placements: Array.isArray(pr.adPlacements) ? pr.adPlacements.length : 0,
      playerAds: !!pr.playerAds,
    };
  };
  let initial = null;
  let live = null;
  try { initial = window.ytInitialPlayerResponse || null; } catch (e) { initial = null; }
  try { live = (player && player.getPlayerResponse) ? player.getPlayerResponse() : null; } catch (e) { live = null; }
  const a = pick(initial);
  const b = pick(live);
  let gl = '';
  try { gl = (window.ytcfg && window.ytcfg.get) ? (window.ytcfg.get('GL') || '') : ''; } catch (e) { gl = ''; }
  return {
    status: a.status || b.status || '',
    reason: a.reason || b.reason || '',
    hasPlayer: !!player,
    adShowing: !!(player && player.classList.contains('ad-showing')),
    adModule: player
      ? player.querySelectorAll('.ytp-ad-module > *, .ytp-ad-player-overlay, .ytp-ad-image-overlay').length
      : 0,
    adPlacements: Math.max(a.placements, b.placements),
    playerAds: a.playerAds || b.playerAds,
    gl: gl,
  };
}"""

CONSENT_COOKIES = [
    {"name": "CONSENT", "value": "YES+cb.20210328-17-p0.en+FX+000", "domain": ".youtube.com", "path": "/"},
    {"name": "SOCS", "value": "CAI", "domain": ".youtube.com", "path": "/"},
]

# Chromium codes that mean the page never connected. Such a node cannot be
# checked at all, so the probe stops before the player wait and PROBE_DWELL.
NET_ERROR_RE = re.compile(r"net::(ERR_[A-Z_]+)")
CONNECTION_ERRORS = (
    "ERR_CONNECTION_",
    "ERR_TIMED_OUT",
    "ERR_TUNNEL_CONNECTION_FAILED",
    "ERR_PROXY_CONNECTION_FAILED",
    "ERR_NAME_NOT_RESOLVED",
    "ERR_NAME_RESOLUTION_FAILED",
    "ERR_ADDRESS_UNREACHABLE",
    "ERR_INTERNET_DISCONNECTED",
    "ERR_SOCKET_NOT_CONNECTED",
    "ERR_EMPTY_RESPONSE",
    "ERR_NETWORK_CHANGED",
    "ERR_NETWORK_ACCESS_DENIED",
)


def connection_error(message: Optional[str]) -> Optional[str]:
    """The Chromium code of a failed connection inside an error message."""
    if not message:
        return None
    found = NET_ERROR_RE.search(message)
    if found and found.group(1).startswith(CONNECTION_ERRORS):
        return found.group(1)
    return None


def video_id(url: str) -> str:
    """Short label for a probe video: its ``v`` parameter, or the URL itself."""
    found = parse_qs(urlsplit(url).query).get("v")
    return found[0] if found else url


@dataclass(frozen=True)
class ProbeResult:
    status: str
    detail: str = ""
    geo: str = ""
    elapsed: float = 0.0
    signals: tuple[str, ...] = field(default_factory=tuple)

    @property
    def decisive(self) -> bool:
        """True when the result may change tags (clean or ads)."""
        return self.status in (CLEAN, ADS)


class YouTubeProbe:
    def __init__(self, settings: Settings, logger: Optional[logging.Logger] = None) -> None:
        self._settings = settings
        self._log = logger or logging.getLogger("probe")
        self._playwright: Any = None
        self._browser: Any = None
        self._user_agent: Optional[str] = None
        # The video that showed ads last time; it moves to the front so the
        # full probe runs on a monetized video instead of a demonetized one.
        self._preferred: Optional[str] = None

    # ------------------------------------------------------------- lifecycle

    def start(self) -> None:
        from playwright.sync_api import sync_playwright

        self._playwright = sync_playwright().start()
        self._browser = self._playwright.chromium.launch(
            headless=self._settings.headless,
            proxy={"server": self._settings.mihomo_proxy_url},
            args=[
                "--autoplay-policy=no-user-gesture-required",
                "--mute-audio",
                "--disable-quic",
                "--disable-dev-shm-usage",
                "--no-first-run",
                "--no-default-browser-check",
                "--disable-background-timer-throttling",
                "--disable-renderer-backgrounding",
                "--disable-features=Translate,MediaRouter",
            ],
        )
        if self._settings.headless:
            # Headless Chromium advertises itself, which YouTube sometimes treats
            # as a bot; reuse the real string with the marker stripped away.
            self._user_agent = self._detect_user_agent()

    def close(self) -> None:
        if self._browser is not None:
            self._browser.close()
            self._browser = None
        if self._playwright is not None:
            self._playwright.stop()
            self._playwright = None

    def _detect_user_agent(self) -> Optional[str]:
        try:
            context = self._browser.new_context()
            try:
                page = context.new_page()
                return str(page.evaluate("navigator.userAgent")).replace("HeadlessChrome", "Chrome")
            finally:
                context.close()
        except PlaywrightError as exc:
            self._log.debug("could not detect user agent: %s", exc)
            return None

    # ----------------------------------------------------------------- probe

    def probe(self, timeout: Optional[float] = None) -> ProbeResult:
        if self._browser is None:
            raise RuntimeError("YouTubeProbe.start() must be called first")

        budget = float(timeout or self._settings.probe_timeout)
        deadline = time.monotonic() + budget
        started = time.monotonic()
        primary = self._ordered_urls()[0]

        context = self._browser.new_context(**self._context_options())
        try:
            page = context.new_page()
            creative_hits: list[str] = []
            page.on("request", lambda request: self._on_request(request, creative_hits))
            context.add_cookies(CONSENT_COOKIES)

            warm_error = self._warm_up(page, deadline, primary)
            failed = connection_error(warm_error)
            if failed:
                return ProbeResult(
                    UNREACHABLE, f"connection failed: {failed}", elapsed=time.monotonic() - started
                )

            navigation_error = self._navigate(page, deadline, primary)
            failed = connection_error(navigation_error)
            if failed:
                # The node dropped the connection: a player cannot appear, and
                # the extra videos would fail the same way.
                return ProbeResult(
                    UNREACHABLE, f"connection failed: {failed}", elapsed=time.monotonic() - started
                )
            state = self._read_state(page)
            if state is None:
                return ProbeResult(
                    UNREACHABLE, navigation_error or "page did not load", elapsed=time.monotonic() - started
                )

            self._wait_for_player(page, deadline)
            state = self._read_state(page) or state
            if not state.get("hasPlayer") and not state.get("status") and not self._has_ad_signal(state):
                # Nothing arrived: a slow node or a blocked page. Sampling would
                # only sit until the deadline and end as unknown anyway.
                status = UNREACHABLE if navigation_error else UNKNOWN
                return ProbeResult(
                    status,
                    navigation_error or "player did not load",
                    str(state.get("gl") or ""),
                    time.monotonic() - started,
                )

            self._start_playback(page)
            state = self._sample_until_decided(page, deadline) or self._read_state(page) or state

            result = self._classify(state, creative_hits, navigation_error, time.monotonic() - started)
            self._log.debug(
                "probe finished: status=%s geo=%s signals=%s detail=%s",
                result.status,
                result.geo or "?",
                result.signals,
                result.detail,
            )
            if result.status == ADS:
                self._remember(primary)
                return result
            if result.status == UNREACHABLE:
                return result
            # The watched video can be demonetized for this exit (or blocked
            # there), so the other videos get a quick look: their ad schedule
            # alone can still prove that the node is not ad-free.
            return self._probe_extra_videos(page, deadline, started, creative_hits) or result
        finally:
            try:
                context.close()
            except PlaywrightError:
                pass

    # -------------------------------------------------------------- internals

    def _ordered_urls(self) -> tuple[str, ...]:
        """The video that last showed ads first, then the configured order."""
        preferred = self._preferred
        if not preferred or preferred not in self._settings.probe_urls:
            return self._settings.probe_urls
        rest = tuple(url for url in self._settings.probe_urls if url != preferred)
        return (preferred,) + rest

    def _remember(self, url: str) -> None:
        """Promote a monetized video so later nodes start with it."""
        if url == self._preferred:
            return
        self._preferred = url
        self._log.info("probe video %s shows ads, it now goes first", video_id(url))

    def _context_options(self) -> dict[str, Any]:
        options: dict[str, Any] = {
            "ignore_https_errors": True,
            "locale": "en-US",
            "timezone_id": "UTC",
            "viewport": {"width": 1280, "height": 800},
        }
        if self._user_agent:
            options["user_agent"] = self._user_agent
        return options

    @staticmethod
    def _on_request(request: Any, hits: list[str]) -> None:
        try:
            url = request.url
        except Exception:  # pragma: no cover - defensive against closed contexts
            return
        if AD_CREATIVE_RE.search(url):
            hits.append(url)

    def _warm_up(self, page: Any, deadline: float, url: str) -> Optional[str]:
        """Visit the YouTube home page before the watched video.

        A brand-new browser context has no VISITOR_INFO1_LIVE cookie; without it
        YouTube withholds both ads and the media stream, so every node would
        look ad-free and every player would stall at the first frame.

        Returns the failure message when the home page did not open, ``None``
        otherwise.
        """
        origin = urlsplit(url)
        if not origin.scheme or not origin.netloc:
            return None
        remaining = min(12.0, max(3.0, deadline - time.monotonic()))
        try:
            page.goto(
                f"{origin.scheme}://{origin.netloc}/",
                wait_until="domcontentloaded",
                timeout=remaining * 1000,
            )
            return None
        except PlaywrightTimeoutError:
            return "home page did not load in time"
        except PlaywrightError as exc:
            message = str(exc).splitlines()[0] if str(exc) else exc.__class__.__name__
            self._log.debug("warm-up failed: %s", message)
            return message

    def _navigate(self, page: Any, deadline: float, url: str) -> Optional[str]:
        remaining = max(5.0, deadline - time.monotonic())
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=remaining * 1000)
            return None
        except PlaywrightTimeoutError:
            # A slow node may still render the player afterwards; keep the page.
            return "navigation timeout"
        except PlaywrightError as exc:
            message = str(exc).splitlines()[0] if str(exc) else exc.__class__.__name__
            return message

    def _wait_for_player(self, page: Any, deadline: float, cap: float = 15.0) -> None:
        remaining = deadline - time.monotonic()
        if remaining <= 1:
            return
        try:
            page.wait_for_selector("#movie_player", timeout=min(cap, remaining) * 1000)
        except PlaywrightTimeoutError:
            pass
        except PlaywrightError:
            pass

    def _start_playback(self, page: Any) -> None:
        """Start the video so a pre-roll ad has a chance to be requested.

        YouTube asks for the ad when playback begins, so a paused player can
        look ad-free even on a monetized node.
        """
        try:
            page.evaluate(
                """() => {
                  const player = document.getElementById('movie_player');
                  if (!player) return;
                  if (player.playVideo) player.playVideo();
                  const button = player.querySelector('.ytp-large-play-button');
                  if (button && button.click) button.click();
                }"""
            )
        except PlaywrightError as exc:
            self._log.debug("could not start playback: %s", exc)

    def _sample_until_decided(self, page: Any, deadline: float) -> Optional[dict[str, Any]]:
        # A healthy player that stays ad-free for PROBE_DWELL is accepted right
        # away; burning the whole timeout on every good node makes a cycle take
        # PROBE_TIMEOUT per node.
        dwell = max(0.0, self._settings.probe_dwell)
        settled_at: Optional[float] = None
        state: Optional[dict[str, Any]] = None
        while True:
            current = self._read_state(page)
            if current is None:
                return state
            state = current
            if self._has_ad_signal(current):
                return current
            now = time.monotonic()
            if current.get("status") == "OK" and current.get("hasPlayer"):
                if settled_at is None:
                    settled_at = now
                elif now - settled_at >= dwell:
                    return current
            else:
                settled_at = None
            if now >= deadline:
                return current
            try:
                page.wait_for_timeout(500)
            except PlaywrightError:
                return current

    def _probe_extra_videos(
        self,
        page: Any,
        deadline: float,
        started: float,
        creative_hits: list[str],
    ) -> Optional[ProbeResult]:
        """Quick look at the remaining videos when the main probe found no ads.

        Only the ad schedule of the page is inspected: no playback and no
        ``PROBE_DWELL``, so an extra video costs a couple of seconds. A hit can
        only turn a clean verdict into ``ads`` - it never produces a clean one.
        """
        for url in self._ordered_urls()[1:]:
            remaining = deadline - time.monotonic()
            if remaining < 4.0:
                break
            try:
                page.goto(url, wait_until="domcontentloaded", timeout=min(8.0, remaining) * 1000)
            except PlaywrightError as exc:
                message = str(exc).splitlines()[0] if str(exc) else exc.__class__.__name__
                self._log.debug("extra video %s failed: %s", url, message)
                if connection_error(message):
                    # The node is gone mid-check; the other videos would fail too.
                    break
                continue
            self._wait_for_player(page, deadline, cap=8.0)
            state = self._read_state(page)
            if state is None:
                continue
            if not creative_hits and not self._has_ad_signal(state):
                continue
            result = self._classify(state, creative_hits, None, time.monotonic() - started)
            if result.status == ADS:
                self._remember(url)
            # Name the video in the log: it tells which entry of YOUTUBE_VIDEOS
            # is still monetized and which one is worth keeping first.
            return ProbeResult(
                result.status,
                result.detail,
                result.geo,
                result.elapsed,
                (video_id(url),) + result.signals,
            )
        return None

    def _read_state(self, page: Any) -> Optional[dict[str, Any]]:
        try:
            return page.evaluate(PLAYER_STATE_JS)
        except PlaywrightError:
            return None

    @staticmethod
    def _has_ad_signal(state: dict[str, Any]) -> bool:
        return bool(
            state.get("adPlacements")
            or state.get("playerAds")
            or state.get("adShowing")
            or state.get("adModule")
        )

    def _classify(
        self,
        state: dict[str, Any],
        creative_hits: list[str],
        navigation_error: Optional[str],
        elapsed: float,
    ) -> ProbeResult:
        geo = str(state.get("gl") or "")
        status = str(state.get("status") or "")
        signals: list[str] = []

        if state.get("adPlacements"):
            signals.append(f"adPlacements={state['adPlacements']}")
        if state.get("playerAds"):
            signals.append("playerAds")
        if state.get("adShowing"):
            signals.append("dom:ad-showing")
        if state.get("adModule"):
            signals.append(f"dom:ad-module={state['adModule']}")
        if creative_hits:
            signals.append(f"network:ad-creative={len(creative_hits)}")

        if signals:
            return ProbeResult(ADS, "ads detected", geo, elapsed, tuple(signals))

        if status == "OK" and state.get("hasPlayer"):
            return ProbeResult(CLEAN, "player ok, no ad signals", geo, elapsed)

        if status:
            reason = str(state.get("reason") or "").strip()
            detail = f"playability={status}" + (f" ({reason})" if reason else "")
            return ProbeResult(UNKNOWN, detail, geo, elapsed)

        if navigation_error:
            return ProbeResult(UNREACHABLE, navigation_error, geo, elapsed)

        return ProbeResult(UNKNOWN, "player did not load", geo, elapsed)

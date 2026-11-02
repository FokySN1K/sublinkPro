"""Entry point: periodically check every SublinkPro node for YouTube ads."""

from __future__ import annotations

import fcntl
import itertools
import json
import logging
import os
import queue
import signal
import sys
import threading
import time
from dataclasses import asdict, dataclass, field, replace
from datetime import datetime, timezone
from typing import IO, Optional, Protocol, Sequence
from urllib.parse import urlsplit

from .config import SERVICE_RUTUBE, SERVICE_YOUTUBE, Settings
from .links import rewrite_link_name
from .mihomo import MihomoError, MihomoRunner
from .probe import ADS, CLEAN, UNKNOWN, UNREACHABLE, ProbeResult, YouTubeProbe
from .probe_rutube import RutubeProbe
from .sublink import NodeRef, SublinkClient, SublinkError

LOG = logging.getLogger("main")


class Probe(Protocol):
    """A service-specific ad probe; see ``probe.py`` and ``probe_rutube.py``."""

    def start(self) -> None: ...

    def probe(self, timeout: Optional[float] = None) -> ProbeResult: ...

    def close(self) -> None: ...


# --------------------------------------------------------------------- helpers


# A request that proves the node can reach the service at all. Mihomo dials it
# directly, so an unreachable node is skipped in seconds instead of burning
# PROBE_TIMEOUT inside the browser.
REACHABILITY_URLS = {
    SERVICE_YOUTUBE: "https://www.youtube.com/",
    SERVICE_RUTUBE: "https://rutube.ru/",
}


def configure_logging(level: str) -> None:
    logging.basicConfig(
        level=getattr(logging, level, logging.INFO),
        format="%(asctime)s %(levelname)-7s %(name)-8s %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
        stream=sys.stdout,
        force=True,
    )
    for noisy in ("urllib3", "playwright"):
        logging.getLogger(noisy).setLevel(logging.WARNING)


def format_duration(seconds: float) -> str:
    seconds = int(round(seconds))
    if seconds >= 86400 and seconds % 86400 == 0:
        return f"{seconds // 86400}d"
    if seconds >= 3600:
        hours, rest = divmod(seconds, 3600)
        return f"{hours}h{rest // 60}m" if rest else f"{hours}h"
    if seconds >= 60:
        minutes, rest = divmod(seconds, 60)
        return f"{minutes}m{rest}s" if rest else f"{minutes}m"
    return f"{seconds}s"


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def truncate(value: str, width: int) -> str:
    return value if len(value) <= width else value[: width - 1] + "…"


def acquire_lock(state_dir: str) -> IO[str]:
    os.makedirs(state_dir, exist_ok=True)
    handle = open(os.path.join(state_dir, "scheduler.lock"), "w", encoding="utf-8")
    try:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        handle.close()
        raise SystemExit("another yt-ad-checker instance is already running on this state dir")
    return handle


# --------------------------------------------------------------------- targets


@dataclass
class Target:
    """A SublinkPro node and the mihomo proxy names built from its links."""

    node: NodeRef
    names: list[str] = field(default_factory=list)


def build_targets(nodes: Sequence[NodeRef]) -> tuple[list[Target], list[tuple[str, str]], list[str]]:
    targets: list[Target] = []
    proxies: list[tuple[str, str]] = []
    skipped: list[str] = []
    for node in nodes:
        names: list[str] = []
        for index, link in enumerate(node.links, start=1):
            name = f"n{node.id}" if index == 1 else f"n{node.id}_{index}"
            try:
                proxies.append((name, rewrite_link_name(link, name)))
            except Exception as exc:  # noqa: BLE001 - a single bad link must not stop the cycle
                skipped.append(f"node #{node.id}: unusable link ({exc})")
                continue
            names.append(name)
        if names:
            targets.append(Target(node=node, names=names))
        else:
            skipped.append(f"node #{node.id}: no usable link")
    return targets, proxies, skipped


@dataclass
class Worker:
    """One mihomo instance plus the browser that probes through it.

    Playwright's sync API is bound to the thread that started it, so a worker's
    probe is started and closed inside its own thread (see :func:`run_cycle`).
    """

    index: int
    runner: MihomoRunner
    probe: Probe


def select_nodes(
    nodes: Sequence[NodeRef],
    only_tags: Sequence[str],
    skip_tags: Sequence[str],
    max_nodes: int,
) -> list[NodeRef]:
    """Apply ONLY_TAGS / SKIP_TAGS / MAX_NODES to the fetched node list.

    ``ONLY_TAGS`` keeps a node that carries at least one of the tags,
    ``SKIP_TAGS`` drops a node that carries any of them, and ``MAX_NODES`` caps
    what is left. Tag matching ignores case, the same way the GUI shows tags.
    """
    only = {tag.casefold() for tag in only_tags}
    skip = {tag.casefold() for tag in skip_tags}
    kept: list[NodeRef] = []
    for node in nodes:
        tags = {tag.casefold() for tag in node.tags}
        if only and not (tags & only):
            continue
        if tags & skip:
            continue
        kept.append(node)
    if max_nodes and len(kept) > max_nodes:
        kept = kept[:max_nodes]
    return kept


def build_worker_settings(settings: Settings, index: int) -> Settings:
    """Ports and state directory for worker ``index`` (0 based).

    A single worker keeps the configured ports and directories untouched, so an
    existing deployment behaves exactly as before.
    """
    if settings.workers == 1:
        return settings
    parts = urlsplit(settings.mihomo_api)
    api_port = (parts.port or 19090) + index
    api = f"{parts.scheme or 'http'}://{parts.hostname or '127.0.0.1'}:{api_port}"
    return replace(
        settings,
        state_dir=os.path.join(settings.state_dir, f"worker-{index + 1}"),
        mihomo_api=api,
        mihomo_proxy_port=settings.mihomo_proxy_port + index,
    )


def build_probe(settings: Settings) -> Probe:
    if settings.service == SERVICE_RUTUBE:
        return RutubeProbe(settings, logging.getLogger("probe"))
    return YouTubeProbe(settings, logging.getLogger("probe"))


def build_workers(settings: Settings) -> list[Worker]:
    workers: list[Worker] = []
    for index in range(settings.workers):
        worker_settings = build_worker_settings(settings, index)
        workers.append(
            Worker(
                index=index,
                runner=MihomoRunner(worker_settings, logging.getLogger("mihomo")),
                probe=build_probe(worker_settings),
            )
        )
    return workers


def combine(results: Sequence[ProbeResult]) -> ProbeResult:
    """A node with several links counts as clean only when every link is clean."""
    if not results:
        return ProbeResult(UNKNOWN, "no probe result")
    for result in results:
        if result.status == ADS:
            return result
    if all(result.status == CLEAN for result in results):
        return results[0]
    for result in results:
        if result.status == UNKNOWN:
            return result
    return results[0]


# ----------------------------------------------------------------------- cycle


@dataclass
class CycleStats:
    """What one cycle did.

    ``nodes`` is how many nodes the cycle planned to check, ``checked`` how many
    it actually finished: when a run is stopped early the difference is exactly
    what was left, so a partial run can never look like a complete one.
    """

    started_at: str = ""
    finished_at: str = ""
    duration_seconds: float = 0.0
    workers: int = 0
    nodes: int = 0
    checked: int = 0
    skipped_nodes: int = 0
    interrupted: bool = False
    seconds_per_node: float = 0.0
    clean: int = 0
    ads: int = 0
    unreachable: int = 0
    unknown: int = 0
    tags_added: int = 0
    tags_removed: int = 0
    tag_errors: int = 0


@dataclass
class TagSync:
    """Result of a single node tag sync."""

    note: str = ""
    added: int = 0
    removed: int = 0


def apply_tag(
    settings: Settings,
    client: SublinkClient,
    node: NodeRef,
    result: ProbeResult,
) -> TagSync:
    """Mirror the verdict on both tags. Returns a short note and op counts.

    ``clean`` nodes get the clean tag and lose the ads tag; ``ads`` nodes get the
    ads tag and lose the clean tag. The pair is service specific and mutually
    exclusive, so a node never carries both. ``unreachable`` and ``unknown``
    nodes are left as they are.
    """
    if result.status == CLEAN:
        wanted, unwanted = settings.clean_tag, settings.ads_tag
    elif result.status == ADS:
        wanted, unwanted = settings.ads_tag, settings.clean_tag
    else:
        return TagSync()

    to_add = [] if node.has_tag(wanted) else [wanted]
    to_remove = [unwanted] if node.has_tag(unwanted) else []
    if not to_add and not to_remove:
        return TagSync()

    sync = TagSync(added=len(to_add), removed=len(to_remove))
    if settings.dry_run:
        planned = [f"add {tag}" for tag in to_add] + [f"remove {tag}" for tag in to_remove]
        sync.note = "would " + ", ".join(planned)
        return sync

    for tag in to_add:
        client.add_tag([node.id], tag)
    for tag in to_remove:
        client.remove_tag([node.id], tag)
    sync.note = " ".join([f"+{tag}" for tag in to_add] + [f"-{tag}" for tag in to_remove])
    return sync


@dataclass
class TargetOutcome:
    """What one node produced: the verdict plus the tag diff it caused."""

    result: ProbeResult
    sync: TagSync
    tag_failed: bool = False


def check_target(
    settings: Settings,
    client: SublinkClient,
    worker: Worker,
    target: Target,
    reachability_url: Optional[str],
    tag_lock: threading.Lock,
) -> TargetOutcome:
    """Probe every link of one node and mirror the verdict on its tags."""
    results: list[ProbeResult] = []
    for name in target.names:
        try:
            worker.runner.select(name)
            worker.runner.clear_connections()
            failure = worker.runner.reachable(reachability_url) if reachability_url else None
            if failure:
                # No connection at all: the probe would only wait out its
                # timeout, so the link is skipped instead.
                LOG.debug("node #%d: %s cannot connect: %s", target.node.id, name, failure)
                results.append(ProbeResult(UNREACHABLE, f"connection failed: {failure}"))
                continue
            results.append(worker.probe.probe())
        except MihomoError as exc:
            LOG.error("node #%d: mihomo error: %s", target.node.id, exc)
            try:
                worker.runner.restart()
            except MihomoError as restart_exc:
                LOG.error("mihomo restart failed: %s", restart_exc)
                raise
            results.append(ProbeResult(UNKNOWN, "mihomo error"))
        except Exception as exc:  # noqa: BLE001 - a browser crash must not abort the cycle
            LOG.error("node #%d: probe error: %s", target.node.id, exc)
            results.append(ProbeResult(UNKNOWN, f"probe error: {exc}"))
        if results and results[-1].status == ADS:
            break

    result = combine(results)
    try:
        # The HTTP session is shared by every worker, so tag writes are serialised.
        with tag_lock:
            sync = apply_tag(settings, client, target.node, result)
    except SublinkError as exc:
        LOG.error("node #%d: tag update failed: %s", target.node.id, exc)
        return TargetOutcome(result=result, sync=TagSync(note="tag error"), tag_failed=True)
    return TargetOutcome(result=result, sync=sync)


def run_cycle(
    settings: Settings,
    client: SublinkClient,
    workers: Sequence[Worker],
    stop_event: threading.Event,
) -> CycleStats:
    stats = CycleStats(started_at=utc_now(), workers=len(workers))
    started = time.monotonic()

    if settings.ensure_tag and not settings.dry_run:
        for tag in (settings.clean_tag, settings.ads_tag):
            client.ensure_tag(tag)

    nodes = client.list_nodes()
    LOG.info("fetched %d node(s) from %s", len(nodes), settings.sublink_url)
    kept = select_nodes(nodes, settings.only_tags, settings.skip_tags, settings.max_nodes)
    if len(kept) != len(nodes):
        LOG.info(
            "filters ONLY_TAGS=%s SKIP_TAGS=%s MAX_NODES=%d kept %d of %d node(s)",
            ",".join(settings.only_tags) or "-",
            ",".join(settings.skip_tags) or "-",
            settings.max_nodes,
            len(kept),
            len(nodes),
        )
    if not kept:
        stats.finished_at = utc_now()
        stats.duration_seconds = time.monotonic() - started
        return stats

    targets, proxies, skipped = build_targets(kept)
    stats.skipped_nodes = len(kept) - len(targets)
    for reason in skipped[:10]:
        LOG.warning("skipped %s", reason)
    if not targets:
        stats.finished_at = utc_now()
        stats.duration_seconds = time.monotonic() - started
        return stats

    # Every worker publishes the full node list: it doubles as the selector
    # entries, so any worker can pick any node.
    for worker in workers:
        worker.runner.set_nodes(proxies)
        if not worker.runner.is_running:
            worker.runner.start()
        worker.runner.apply_provider()

    total = len(targets)
    stats.nodes = total
    reachability_url = REACHABILITY_URLS.get(settings.service)

    pending: "queue.Queue[Target]" = queue.Queue()
    for target in targets:
        pending.put(target)

    tag_lock = threading.Lock()
    log_lock = threading.Lock()
    counter = itertools.count(1)
    abort = threading.Event()
    fatal: list[BaseException] = []
    partials: list[CycleStats] = []

    def loop(worker: Worker) -> None:
        local = CycleStats()
        try:
            # Playwright's sync API belongs to the thread that started it, so the
            # browser is opened here instead of in main().
            worker.probe.start()
            while not stop_event.is_set() and not abort.is_set():
                try:
                    target = pending.get_nowait()
                except queue.Empty:
                    break
                with log_lock:
                    index = next(counter)

                outcome = check_target(
                    settings, client, worker, target, reachability_url, tag_lock
                )
                if outcome.result.status == CLEAN:
                    local.clean += 1
                elif outcome.result.status == ADS:
                    local.ads += 1
                elif outcome.result.status == UNREACHABLE:
                    local.unreachable += 1
                else:
                    local.unknown += 1
                local.tags_added += outcome.sync.added
                local.tags_removed += outcome.sync.removed
                if outcome.tag_failed:
                    local.tag_errors += 1

                detail = (
                    " ".join(outcome.result.signals)
                    if outcome.result.signals
                    else outcome.result.detail
                )
                # Only the YouTube probe learns the exit country; Rutube leaves it empty.
                geo = f" GL={outcome.result.geo}" if outcome.result.geo else ""
                prefix = f"W{worker.index + 1} " if settings.workers > 1 else ""
                with log_lock:
                    LOG.info(
                        "[%3d/%3d] %s#%-5d %-42s %-11s%s %5.1fs %s%s",
                        index,
                        total,
                        prefix,
                        target.node.id,
                        truncate(target.node.name, 42),
                        outcome.result.status,
                        geo,
                        outcome.result.elapsed,
                        detail,
                        f" -> {outcome.sync.note}" if outcome.sync.note else "",
                    )
        except BaseException as exc:  # noqa: BLE001 - reported after the join below
            fatal.append(exc)
            abort.set()
        finally:
            try:
                worker.probe.close()
            except Exception as exc:  # noqa: BLE001 - a stuck browser must not hide the cycle
                LOG.warning("worker %d: closing the browser failed: %s", worker.index + 1, exc)
            partials.append(local)

    threads = [
        threading.Thread(target=loop, args=(worker,), name=f"worker-{worker.index + 1}")
        for worker in workers
    ]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    left = pending.qsize()
    if left and (stop_event.is_set() or abort.is_set()):
        LOG.warning("check cycle stopped early, %d node(s) left unchecked", left)

    for local in partials:
        stats.clean += local.clean
        stats.ads += local.ads
        stats.unreachable += local.unreachable
        stats.unknown += local.unknown
        stats.tags_added += local.tags_added
        stats.tags_removed += local.tags_removed
        stats.tag_errors += local.tag_errors

    if fatal:
        raise fatal[0]

    stats.checked = stats.clean + stats.ads + stats.unreachable + stats.unknown
    stats.interrupted = stats.checked < stats.nodes
    stats.finished_at = utc_now()
    stats.duration_seconds = round(time.monotonic() - started, 1)
    if stats.checked:
        stats.seconds_per_node = round(stats.duration_seconds / stats.checked, 1)
    return stats


def write_state(settings: Settings, stats: CycleStats) -> None:
    path = os.path.join(settings.state_dir, "state.json")
    tmp = path + ".tmp"
    try:
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(asdict(stats), handle, indent=2, ensure_ascii=False)
        os.replace(tmp, path)
    except OSError as exc:
        LOG.warning("could not write %s: %s", path, exc)


def log_summary(stats: CycleStats, settings: Settings) -> None:
    pace = f", {stats.seconds_per_node:.1f}s/node" if stats.seconds_per_node else ""
    LOG.info(
        "cycle done in %s: checked %d/%d with %d worker(s)%s: "
        "clean=%d ads=%d unreachable=%d unknown=%d | "
        "%r/%r added=%d removed=%d errors=%d",
        format_duration(stats.duration_seconds),
        stats.checked,
        stats.nodes,
        stats.workers,
        pace,
        stats.clean,
        stats.ads,
        stats.unreachable,
        stats.unknown,
        settings.clean_tag,
        settings.ads_tag,
        stats.tags_added,
        stats.tags_removed,
        stats.tag_errors,
    )
    if stats.interrupted:
        LOG.warning(
            "cycle was interrupted: %d of %d node(s) left unchecked, their tags are unchanged",
            stats.nodes - stats.checked,
            stats.nodes,
        )
    if stats.ads == 0 and stats.clean >= 3:
        # The usual reason for a spotless cycle is a probe video that YouTube
        # no longer monetizes: every node then answers "no ads".
        videos = "YOUTUBE_VIDEOS/PROBE_URL" if settings.service == SERVICE_YOUTUBE else "RUTUBE_VIDEOS"
        LOG.warning(
            "no node reported ads: check that the videos in %s are still monetized for your exits",
            videos,
        )


# ------------------------------------------------------------------- scheduler


def run_forever(
    settings: Settings,
    client: SublinkClient,
    workers: Sequence[Worker],
    stop_event: threading.Event,
) -> None:
    interval = settings.interval
    due = time.monotonic() + (0 if settings.run_on_start else interval)
    if not settings.run_on_start:
        LOG.info("RUN_ON_START is disabled, first cycle in %s", format_duration(interval))

    while not stop_event.is_set():
        wait = due - time.monotonic()
        if wait > 0:
            stop_event.wait(wait)
            continue
        if stop_event.is_set():
            break

        started = time.monotonic()
        LOG.info("=== check cycle started ===")
        try:
            stats = run_cycle(settings, client, workers, stop_event)
            write_state(settings, stats)
            log_summary(stats, settings)
        except (SublinkError, MihomoError) as exc:
            LOG.error("check cycle aborted: %s", exc)
        except Exception:  # noqa: BLE001 - keep the daemon alive on unexpected errors
            LOG.exception("check cycle crashed")

        finished = time.monotonic()
        due = started + interval
        if finished > due:
            # The cycle outlived its own period: drop the runs that were missed
            # instead of firing them back to back.
            missed = int((finished - due) // interval) + 1
            LOG.warning(
                "cycle took %s, longer than the %s period; %d scheduled run(s) skipped",
                format_duration(finished - started),
                format_duration(interval),
                missed,
            )
            due = finished + interval
        LOG.info("next cycle at ~%s", format_duration(max(0.0, due - time.monotonic())) + " from now")


def main() -> int:
    try:
        settings = Settings.from_env()
    except ValueError as exc:
        print(f"configuration error: {exc}", file=sys.stderr)
        return 2

    configure_logging(settings.log_level)
    LOG.info(
        "yt-ad-checker starting: service=%s sublink=%s tags=%r/%r interval=%s dry_run=%s",
        settings.service,
        settings.sublink_url,
        settings.clean_tag,
        settings.ads_tag,
        format_duration(settings.interval),
        settings.dry_run,
    )
    if settings.dry_run:
        LOG.warning("DRY_RUN is enabled: node tags will not be modified")
    if settings.service == SERVICE_YOUTUBE:
        LOG.info(
            "youtube probes (%d): %s; the video that shows ads moves to the front",
            len(settings.probe_urls),
            ", ".join(settings.probe_urls),
        )
    if settings.workers > 1:
        LOG.info("workers: %d (each one runs its own mihomo and browser)", settings.workers)
    if settings.only_tags or settings.skip_tags or settings.max_nodes:
        LOG.info(
            "node filters: ONLY_TAGS=%s SKIP_TAGS=%s MAX_NODES=%d",
            ",".join(settings.only_tags) or "-",
            ",".join(settings.skip_tags) or "-",
            settings.max_nodes,
        )

    try:
        lock = acquire_lock(settings.state_dir)
    except SystemExit as exc:
        LOG.error("%s", exc)
        return 3
    except OSError as exc:
        LOG.error("cannot lock %s: %s", settings.state_dir, exc)
        return 3

    stop_event = threading.Event()

    def request_stop(signum: int, _frame: object) -> None:
        LOG.warning("received signal %s, shutting down", signum)
        stop_event.set()

    signal.signal(signal.SIGTERM, request_stop)
    signal.signal(signal.SIGINT, request_stop)

    client = SublinkClient(settings.sublink_url, settings.sublink_api_key)
    workers = build_workers(settings)

    exit_code = 0
    try:
        if settings.once:
            # A manual run: no scheduler, no idle wait, just one pass and exit.
            LOG.info("ONCE is enabled: running a single cycle and exiting")
            stats = run_cycle(settings, client, workers, stop_event)
            write_state(settings, stats)
            log_summary(stats, settings)
        else:
            run_forever(settings, client, workers, stop_event)
    except KeyboardInterrupt:
        LOG.warning("interrupted")
    except Exception:  # noqa: BLE001 - report and exit non-zero for the supervisor
        LOG.exception("fatal error")
        exit_code = 1
    finally:
        for worker in workers:
            worker.runner.close()
        client.close()
        lock.close()
        LOG.info("stopped")
    return exit_code


if __name__ == "__main__":
    sys.exit(main())

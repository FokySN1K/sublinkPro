"""Entry point: periodically check every SublinkPro node for AI service access."""

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
from concurrent.futures import ThreadPoolExecutor
from dataclasses import asdict, dataclass, field, replace
from datetime import datetime, timezone
from typing import IO, Optional, Sequence
from urllib.parse import urlsplit

import requests

from .config import TAG_BLOCKED_COLOR, TAG_OK_COLOR, Settings
from .links import rewrite_link_name
from .mihomo import MihomoError, MihomoRunner
from .providers import (
    AVAILABLE,
    COMMON_BLOCKED_REGIONS,
    PARTIAL,
    RESTRICTED,
    UNKNOWN,
    ProbeContext,
    Provider,
    Result,
    select_providers,
)
from .sublink import NodeRef, SublinkClient, SublinkError

LOG = logging.getLogger("main")

# One short symbol per verdict keeps a node line readable with ten services.
STATUS_SYMBOLS = {AVAILABLE: "+", RESTRICTED: "-", PARTIAL: "~", UNKNOWN: "?"}
# Providers are independent HTTP calls, so a small pool already hides most of
# the latency; going wider only multiplies sockets without gaining much.
PROVIDER_THREADS = 5


# --------------------------------------------------------------------- helpers


def configure_logging(level: str) -> None:
    logging.basicConfig(
        level=getattr(logging, level, logging.INFO),
        format="%(asctime)s %(levelname)-7s %(name)-8s %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
        stream=sys.stdout,
        force=True,
    )
    for noisy in ("urllib3",):
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
        raise SystemExit("another ai-checker instance is already running on this state dir")
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
        reported = False
        for index, link in enumerate(node.links, start=1):
            name = f"n{node.id}" if index == 1 else f"n{node.id}_{index}"
            try:
                proxies.append((name, rewrite_link_name(link, name)))
            except Exception as exc:  # noqa: BLE001 - a single bad link must not stop the cycle
                skipped.append(f"node #{node.id}: unusable link ({exc})")
                reported = True
                continue
            names.append(name)
        if names:
            targets.append(Target(node=node, names=names))
        elif not reported:
            skipped.append(f"node #{node.id}: no usable link")
    return targets, proxies, skipped


@dataclass
class Worker:
    """One mihomo instance; the probes themselves are stateless HTTP calls."""

    index: int
    settings: Settings
    runner: MihomoRunner


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


def build_workers(settings: Settings) -> list[Worker]:
    workers: list[Worker] = []
    for index in range(settings.workers):
        worker_settings = build_worker_settings(settings, index)
        workers.append(
            Worker(
                index=index,
                settings=worker_settings,
                runner=MihomoRunner(worker_settings, logging.getLogger("mihomo")),
            )
        )
    return workers


# ---------------------------------------------------------------------- probe


def new_probe_session(settings: Settings) -> requests.Session:
    """A session that sends every request through the worker's mihomo port."""
    session = requests.Session()
    session.trust_env = False
    session.proxies.update({"http": settings.mihomo_proxy_url, "https": settings.mihomo_proxy_url})
    return session


def run_provider(settings: Settings, provider: Provider) -> Result:
    """Probe one service from the currently selected node.

    A session per provider keeps the pool thread-safe and guarantees that a
    failed call cannot poison the next service.
    """
    session = new_probe_session(settings)
    try:
        context = ProbeContext(
            session,
            settings.probe_timeout,
            settings.user_agent,
            logging.getLogger("probe"),
        )
        return provider.check(context)
    except Exception as exc:  # noqa: BLE001 - one service must not abort the node
        return Result(UNKNOWN, f"probe error: {exc}")
    finally:
        session.close()


def combine(results: Sequence[Result]) -> Result:
    """Merge verdicts from several links of the same node.

    One working link is enough for "available", while "restricted" is only
    reported when no link worked; anything else stays unknown.
    """
    if not results:
        return Result(UNKNOWN, "no probe result")
    for result in results:
        if result.status == AVAILABLE:
            return result
    for result in results:
        if result.status == RESTRICTED:
            return result
    return results[0]


@dataclass
class TagSync:
    """Result of a single node tag sync."""

    note: str = ""
    added: int = 0
    removed: int = 0


def desired_tags(settings: Settings, verdicts: dict[str, Result]) -> tuple[list[str], list[str]]:
    """Tag names this verdict wants on the node and the ones it must drop."""
    wanted: list[str] = []
    unwanted: list[str] = []
    for service, result in verdicts.items():
        if result.status == AVAILABLE:
            wanted.append(settings.available_tag(service))
            unwanted.append(settings.blocked_tag(service))
        elif result.status == RESTRICTED:
            wanted.append(settings.blocked_tag(service))
            unwanted.append(settings.available_tag(service))
    return wanted, unwanted


def apply_tags(
    settings: Settings,
    client: SublinkClient,
    node: NodeRef,
    verdicts: dict[str, Result],
) -> TagSync:
    """Mirror every verdict on the pair of tags that belongs to the service.

    ``unknown``/``partial`` verdicts leave the node untouched: a captcha or a
    timeout must never rewrite yesterday's result.
    """
    wanted, unwanted = desired_tags(settings, verdicts)
    to_add = [tag for tag in wanted if not node.has_tag(tag)]
    to_remove = [tag for tag in unwanted if node.has_tag(tag)]
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
    """What one node produced: the verdicts plus the tag diff they caused."""

    verdicts: dict[str, Result]
    sync: TagSync
    elapsed: float = 0.0
    tag_failed: bool = False


def check_target(
    settings: Settings,
    client: SublinkClient,
    worker: Worker,
    providers: Sequence[Provider],
    target: Target,
    tag_lock: threading.Lock,
) -> TargetOutcome:
    """Probe every link of one node and mirror the verdicts on its tags."""
    started = time.monotonic()
    per_service: dict[str, list[Result]] = {provider.key: [] for provider in providers}
    for name in target.names:
        try:
            worker.runner.select(name)
            worker.runner.clear_connections()
            failure = worker.runner.reachable(settings.reachability_url)
            if failure:
                # No connection at all: probing would only burn PROBE_TIMEOUT
                # per service, so the link is recorded as unchecked instead.
                LOG.debug("node #%d: %s cannot connect: %s", target.node.id, name, failure)
                for provider in providers:
                    per_service[provider.key].append(Result(UNKNOWN, f"link unreachable: {failure}"))
                continue
            with ThreadPoolExecutor(max_workers=min(PROVIDER_THREADS, len(providers))) as pool:
                futures = {
                    pool.submit(run_provider, worker.settings, provider): provider
                    for provider in providers
                }
                for future, provider in futures.items():
                    per_service[provider.key].append(future.result())
        except MihomoError as exc:
            LOG.error("node #%d: mihomo error: %s", target.node.id, exc)
            try:
                worker.runner.restart()
            except MihomoError as restart_exc:
                LOG.error("mihomo restart failed: %s", restart_exc)
                raise
            for provider in providers:
                per_service[provider.key].append(Result(UNKNOWN, "mihomo error"))

    verdicts = {key: combine(results) for key, results in per_service.items()}
    try:
        # The HTTP session is shared by every worker, so tag writes are serialised.
        with tag_lock:
            sync = apply_tags(settings, client, target.node, verdicts)
    except SublinkError as exc:
        LOG.error("node #%d: tag update failed: %s", target.node.id, exc)
        return TargetOutcome(
            verdicts=verdicts,
            sync=TagSync(note="tag error"),
            elapsed=time.monotonic() - started,
            tag_failed=True,
        )
    return TargetOutcome(
        verdicts=verdicts, sync=sync, elapsed=time.monotonic() - started
    )


# ----------------------------------------------------------------------- cycle


@dataclass
class CycleStats:
    """What one cycle did; ``checked`` can differ from ``nodes`` on a stop."""

    started_at: str = ""
    finished_at: str = ""
    duration_seconds: float = 0.0
    workers: int = 0
    nodes: int = 0
    checked: int = 0
    skipped_nodes: int = 0
    interrupted: bool = False
    seconds_per_node: float = 0.0
    # service -> status -> how many nodes ended up with that verdict
    per_service: dict[str, dict[str, int]] = field(default_factory=dict)
    tags_added: int = 0
    tags_removed: int = 0
    tag_errors: int = 0


def empty_counts(services: Sequence[str]) -> dict[str, dict[str, int]]:
    return {
        service: {AVAILABLE: 0, RESTRICTED: 0, PARTIAL: 0, UNKNOWN: 0} for service in services
    }


def render_verdicts(verdicts: dict[str, Result]) -> str:
    parts = []
    for service, result in verdicts.items():
        symbol = STATUS_SYMBOLS.get(result.status, "?")
        parts.append(f"{service}{symbol}")
    return " ".join(parts)


def render_detail(verdicts: dict[str, Result]) -> str:
    """Reasons and Cloudflare regions, only for services that said something."""
    details = []
    for service, result in verdicts.items():
        reason = result.reason
        if result.region and result.region in COMMON_BLOCKED_REGIONS:
            reason = f"{reason} loc={result.region}".strip()
        if reason and result.status != AVAILABLE:
            details.append(f"{service}: {reason}")
    return "; ".join(details)


def run_cycle(
    settings: Settings,
    client: SublinkClient,
    workers: Sequence[Worker],
    stop_event: threading.Event,
) -> CycleStats:
    stats = CycleStats(started_at=utc_now(), workers=len(workers))
    providers = select_providers(settings.services)
    stats.per_service = empty_counts([provider.key for provider in providers])
    started = time.monotonic()

    if settings.ensure_tag and not settings.dry_run:
        for provider in providers:
            client.ensure_tag(settings.available_tag(provider.key), TAG_OK_COLOR)
            client.ensure_tag(settings.blocked_tag(provider.key), TAG_BLOCKED_COLOR)

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
        local = CycleStats(per_service=empty_counts([provider.key for provider in providers]))
        try:
            while not stop_event.is_set() and not abort.is_set():
                try:
                    target = pending.get_nowait()
                except queue.Empty:
                    break
                with log_lock:
                    index = next(counter)

                outcome = check_target(
                    settings, client, worker, providers, target, tag_lock
                )
                for service, result in outcome.verdicts.items():
                    local.per_service[service][result.status] += 1
                local.tags_added += outcome.sync.added
                local.tags_removed += outcome.sync.removed
                if outcome.tag_failed:
                    local.tag_errors += 1

                prefix = f"W{worker.index + 1} " if settings.workers > 1 else ""
                detail = render_detail(outcome.verdicts)
                with log_lock:
                    LOG.info(
                        "[%3d/%3d] %s#%-5d %-38s %-48s %5.1fs %s%s",
                        index,
                        total,
                        prefix,
                        target.node.id,
                        truncate(target.node.name, 38),
                        render_verdicts(outcome.verdicts),
                        outcome.elapsed,
                        detail,
                        f" -> {outcome.sync.note}" if outcome.sync.note else "",
                    )
        except BaseException as exc:  # noqa: BLE001 - reported after the join below
            fatal.append(exc)
            abort.set()
        finally:
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
        for service, counts in local.per_service.items():
            for status, count in counts.items():
                stats.per_service[service][status] += count
        stats.tags_added += local.tags_added
        stats.tags_removed += local.tags_removed
        stats.tag_errors += local.tag_errors

    if fatal:
        raise fatal[0]

    services_checked = len(providers)
    stats.checked = (
        sum(sum(counts.values()) for counts in stats.per_service.values()) // services_checked
        if services_checked
        else 0
    )
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
        "cycle done in %s: checked %d/%d with %d worker(s)%s | "
        "tags added=%d removed=%d errors=%d",
        format_duration(stats.duration_seconds),
        stats.checked,
        stats.nodes,
        stats.workers,
        pace,
        stats.tags_added,
        stats.tags_removed,
        stats.tag_errors,
    )
    for service in settings.services:
        counts = stats.per_service.get(service) or {}
        LOG.info(
            "  %-10s available=%d restricted=%d partial=%d unknown=%d",
            service,
            counts.get(AVAILABLE, 0),
            counts.get(RESTRICTED, 0),
            counts.get(PARTIAL, 0),
            counts.get(UNKNOWN, 0),
        )
    if stats.interrupted:
        LOG.warning(
            "cycle was interrupted: %d of %d node(s) left unchecked, their tags are unchanged",
            stats.nodes - stats.checked,
            stats.nodes,
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
    providers = select_providers(settings.services)
    LOG.info(
        "ai-checker starting: services=%s sublink=%s interval=%s dry_run=%s",
        ",".join(provider.key for provider in providers),
        settings.sublink_url,
        format_duration(settings.interval),
        settings.dry_run,
    )
    LOG.info(
        "tags: %s",
        ", ".join(
            f"{provider.key}->{settings.available_tag(provider.key)}/{settings.blocked_tag(provider.key)}"
            for provider in providers
        ),
    )
    if settings.dry_run:
        LOG.warning("DRY_RUN is enabled: node tags will not be modified")
    if settings.workers > 1:
        LOG.info("workers: %d (each one runs its own mihomo)", settings.workers)
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
            try:
                stats = run_cycle(settings, client, workers, stop_event)
            except (SublinkError, MihomoError) as exc:
                # Same shape as the scheduler: one line instead of a traceback.
                LOG.error("check cycle aborted: %s", exc)
                exit_code = 1
            else:
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

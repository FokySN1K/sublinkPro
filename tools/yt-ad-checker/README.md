# yt-ad-checker

Containerised checker that finds which [SublinkPro](../../README.md) nodes serve
**YouTube or Rutube without ads**, and keeps a tag in sync with the result.

For every node the tool routes the probe through a private mihomo instance. For
YouTube a real Chromium session inspects the player; for Rutube a single JSON
request reads the ad schedule Rutube would serve to that exit IP. Ad-free nodes
get the clean tag and lose the ads tag; ad-serving nodes get the ads tag and lose
the clean tag, so a node always ends up with exactly one of the two. Nodes that
are unreachable or answer with an error are left untouched.

```
SublinkPro API ──► node list ──► mihomo (one proxy per node)
                                      │
                    ┌─────────────────┴──────────────────┐
                    ▼                                    ▼
        Chromium ──► youtube.com/watch    Rutube /api/play/options/<video>
                    │                                    │
                    └───────── clean / ads / unreachable / unknown
                                      │
                                      ▼
                       SublinkPro tags (batch-add / batch-remove)
```

## Services

| `SERVICE` | Probe | Tags (default) |
|---|---|---|
| `youtube` (default) | headful Chromium, four player signals | `youtube` / `youtube-ads` |
| `rutube` | one JSON request per video, no browser | `rutube` / `rutube-ads` |

One container checks one service: `SERVICE` is resolved at start-up and selects
the probe, the tag pair and the state file. To check both, run two instances
with their own `/data` volume — `docker-compose.local.yaml` does exactly that
with `yt-ad-checker` and `yt-ad-checker-rutube`. They write different tags, so
the instances never conflict.

## How a node is classified (YouTube)

Four independent signals are collected per probe:

| Signal | Source |
|---|---|
| `adPlacements` / `playerAds` | `ytInitialPlayerResponse` and the live `getPlayerResponse()` |
| `.ad-showing`, ad overlay nodes | the `#movie_player` element |
| Ad creative requests | `/pagead/adview`, `/pagead/interaction`, `*.2mdn.net`, `googlevideo…adformat=` |
| `playabilityStatus` | player response (guards against false "clean" verdicts) |

* **`ads`** – any ad signal was observed.
* **`clean`** – the player reports `playabilityStatus = OK`, the player element
  exists and no ad signal appeared during `PROBE_DWELL` seconds.
* **`unreachable`** – the page never loaded through that node. A node whose
  connection is refused, reset or timed out is skipped right away, so it never
  spends the probe budget.
* **`unknown`** – the page loaded but the player did not become usable
  (consent wall, bot check, `ERROR`/`UNPLAYABLE`/`LOGIN_REQUIRED`).

Only `ads` and `clean` change tags. `unreachable` and `unknown` never do, so a
temporary outage cannot strip a node of its tag.

> Beacons such as `googleads.g.doubleclick.net/pagead/id`,
> `static.doubleclick.net/instream/ad_status.js` and `/api/stats/ads?...&req=9`
> are deliberately **not** treated as ads: they fire on ad-free playbacks too.

Each probe loads the YouTube home page first and then the watched video: a
cookie-less context gets no ad request and no playable media stream, which would
make every node look ad-free. Playback is then started explicitly, because the
pre-roll is requested when playing begins.

### Several probe videos

One video can be demonetized for a country (or for everyone), which would report
every node as ad-free. `YOUTUBE_VIDEOS` therefore holds several videos and they
are used asymmetrically:

* the **first** video gets the full probe (play + `PROBE_DWELL`), as described
  above;
* when that verdict is `clean` or `unknown`, the remaining videos are opened
  without playback and only their ad schedule (`adPlacements` / `playerAds`) is
  read — a second or two each;
* an ad on **any** video makes the node `ads`, and the log line names the video
  that found it (`9bZkp7q19f0 adPlacements=2`);
* the video that showed an ad is remembered and moves to the front for the rest
  of the run (`probe video 9bZkp7q19f0 shows ads, it now goes first`), so the
  full probe keeps running on a monetized video; the preference resets on
  restart;
* a node is `clean` only when the full probe is clean and no other video
  scheduled an ad.

Extra videos can only raise a node to `ads`, never lower it to `clean`, so the
default list cannot create false ad-free verdicts. Drop extra entries — or set a
single `PROBE_URL` — if a cycle must stay as short as possible: every extra video
spends part of the per-node `PROBE_TIMEOUT` budget.

## How a node is classified (Rutube)

Rutube builds the ad schedule server-side and returns it with the play options of
every video:

```
GET https://rutube.ru/api/play/options/<video_id>/?no_404=true
→ { "advert": [{"name": "preroll201", "count": 1, ...}], "cuepoints": [], ... }
```

* **`ads`** – any `advert` entry with `count > 0` (preroll/midroll slots) or a
  non-empty `cuepoints`;
* **`clean`** – at least two probe videos answered and none scheduled a slot;
* **`unreachable`** – no video answered (node down, Rutube blocked, TLS reset);
* **`unknown`** – fewer than two videos answered, e.g. one geo-blocked one.

A single video can be demonetized for everyone, which is why several videos from
`RUTUBE_VIDEOS` are tried before a node is called clean. Ads in Rutube are mostly
RU inventory, so foreign exits legitimately show an empty schedule — that is the
ad-free result you are looking for, not a bug.

## Quick start

```bash
cd tools/yt-ad-checker
cp .env.example .env
# edit .env: set SUBLINK_URL and SUBLINK_API_KEY
docker compose up -d --build
docker compose logs -f
```

The API key is created in SublinkPro under **Settings → Access Keys** and is sent
as the `X-API-Key` header.

### Reaching SublinkPro

* **SublinkPro published on the host** (default): keep
  `SUBLINK_URL=http://host.docker.internal:8000`. The compose file adds the
  `host.docker.internal:host-gateway` mapping for Linux.
* **Shared Docker network**: uncomment the network blocks in
  `docker-compose.yaml` and set `SUBLINK_URL=http://sublinkpro:8000`.

## Configuration

Every knob is an environment variable (see `.env.example`).

| Variable | Default | Description |
|---|---|---|
| `SUBLINK_URL` | `http://host.docker.internal:8000` | SublinkPro base URL |
| `SUBLINK_API_KEY` | – (required) | Access key, sent as `X-API-Key` |
| `SERVICE` | `youtube` | `youtube` or `rutube` |
| `YOUTUBE_TAG` | `youtube` | YouTube: tag of ad-free nodes |
| `YOUTUBE_ADS_TAG` | `youtube-ads` | YouTube: tag of ad-serving nodes (`ADS_TAG` still works as an alias) |
| `RUTUBE_TAG` | `rutube` | Rutube: tag of ad-free nodes |
| `RUTUBE_ADS_TAG` | `rutube-ads` | Rutube: tag of ad-serving nodes |
| `RUTUBE_VIDEOS` | built-in list | Comma-separated Rutube video ids used as probes (must be monetized) |
| `ENSURE_TAG` | `true` | Create both tags when they do not exist yet |
| `DRY_RUN` | `false` | Log decisions without touching tags |
| `CHECK_INTERVAL` | `6h` | `30m`, `6h`, `1d`, `1h30m` or plain seconds |
| `RUN_ON_START` | `true` | Run a cycle immediately at start-up |
| `ONCE` | `false` | Run one cycle and exit (a manual run, ignores `RUN_ON_START`) |
| `ONLY_TAGS` | – | Check only nodes carrying at least one of these tags (comma separated, case-insensitive) |
| `SKIP_TAGS` | – | Never check nodes carrying any of these tags |
| `MAX_NODES` | `0` | Stop after this many nodes; `0` means no limit |
| `WORKERS` | `1` | Probe this many nodes in parallel (each worker gets its own mihomo and browser) |
| `YOUTUBE_VIDEOS` | built-in list | Comma-separated videos to probe (id or URL); `PROBE_URL` still works as an alias for a single video |
| `PROBE_TIMEOUT` | `45` | Total budget per node, seconds |
| `PROBE_DWELL` | `10` | Seconds spent watching the player for ad markers |
| `HEADLESS` | `false` | Disable the virtual display (not recommended) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warning`, `error` |

## Scheduling and long cycles

A full pass over ~100 nodes takes roughly 15–40 minutes, so `CHECK_INTERVAL` is
often shorter than a cycle. The scheduler therefore:

* runs **one cycle at a time** — probes are never overlapped;
* keeps a start-anchored cadence while cycles fit inside the period;
* when a cycle outlives its period, logs how many scheduled runs were skipped and
  anchors the next run to the end of the current one (`CHECK_INTERVAL` later);
* takes an exclusive lock on `/data/scheduler.lock`, so two containers sharing the
  volume cannot run at the same time.

### One-off runs and subsets

`ONCE=true` runs a single cycle and exits — handy for a manual check without
stopping a long-running container afterwards:

```bash
docker run --rm --init --shm-size 1g --env-file .env \
  -e ONCE=true -e SERVICE=youtube yt-ad-checker:latest
```

`ONLY_TAGS` / `SKIP_TAGS` / `MAX_NODES` shrink the work list, for example to
re-check only nodes that already carry the clean tag, or to smoke-test ten nodes
before a full run:

```bash
docker run --rm --init --shm-size 1g --env-file .env \
  -e ONCE=true -e ONLY_TAGS=youtube -e MAX_NODES=10 yt-ad-checker:latest
```

Tags are matched case-insensitively, `SKIP_TAGS` wins over `ONLY_TAGS`, and the
filter is applied before any probe, so skipped nodes cost nothing.

### Parallel workers

`WORKERS=N` probes N nodes at once. Each worker starts its own mihomo (ports
`MIHOMO_PROXY_PORT + i` and the API port `+ i`) and its own Chromium, so the cost
is RAM, not API calls: budget roughly 1 GB per worker on top of the base image
and keep `shm_size` proportional. A full pass over ~100 YouTube nodes drops from
~25 minutes to ~8 minutes with `WORKERS=3`; results and tags are identical
because every worker still owns a single node at a time.

The summary reports the workers used, how many nodes were checked and the
average pace per node; with more than one worker each log line is prefixed with
`W1`/`W2`/… so interleaved output can be untangled.

## Output

```
2026-11-01 20:14:03 INFO    main     === check cycle started ===
2026-11-01 20:14:09 INFO    main     [  1/ 97] #11    Москва                 ads         GL=UA   4.3s adPlacements=2 -> +youtube-ads -youtube
2026-11-01 20:14:21 INFO    main     [  2/ 97] #19    Ростов-на-Дону         clean       GL=RU  11.8s player ok, no ad signals -> +youtube
2026-11-01 20:14:33 WARN    main     [  3/ 97] #20    Краснодар              unreachable         6.0s connection failed: Timeout
2026-11-01 20:41:12 INFO    main     cycle done in 8m3s: checked 97/97 with 3 worker(s), 5.0s/node: clean=41 ads=49 unreachable=5 unknown=2 | 'youtube'/'youtube-ads' added=3 removed=1 errors=0
```

The last cycle summary is also written to `/data/state.json`. Besides the
verdict counters it records how many nodes were planned (`nodes`), how many were
actually finished (`checked`), the `workers` count, the `seconds_per_node` pace
and `interrupted`: a run stopped by SIGTERM keeps `checked < nodes` and logs a
warning, so a partial cycle cannot be mistaken for a complete one.

## How tags are applied

| Verdict | Added | Removed |
|---|---|---|
| `clean` | clean tag (`youtube` / `rutube`) | ads tag (`youtube-ads` / `rutube-ads`) |
| `ads` | ads tag (`youtube-ads` / `rutube-ads`) | clean tag (`youtube` / `rutube`) |
| `unreachable`, `unknown` | – | – |

* additions use `POST /api/v1/tags/node/batch-add`, removals use
  `POST /api/v1/tags/node/batch-remove` with a single `tagNames` entry — every
  other tag on the node stays intact;
* only differences are written: a node that is already in the target state costs
  no API call;
* with `DRY_RUN=true` the same decisions are logged as `would add …, remove …`
  while the counters in the summary still show what a real run would change.

Nodes that hold several URI links are checked link by link; the node counts as
`ads` if any link shows ads and as `clean` only if all of them are clean.

## Notes and limitations

* The checker reads the raw node links from `GET /api/v1/nodes/get`, so a node is
  tested exactly as its URI describes. Settings that only exist in SublinkPro's
  export layer (chain proxy / `DialerProxyName`, Clash-only extras) are not applied.
* `PLAYER_RESPONSE` based detection reflects what YouTube decides for the exit IP.
  A node exiting in a country without pre-roll ads is reported as `clean` — which
  is the behaviour you want when looking for nodes that play YouTube ad-free.
* Probe verdicts are point-in-time; YouTube A/B tests ad delivery, so a node can
  flip between cycles. Re-running with `DRY_RUN=true` and comparing two cycles is a
  good way to spot unstable nodes.
* Before each probe the node is dialled through mihomo (`/proxies/<name>/delay`,
  6 s): a node that cannot open a connection is reported `unreachable` in
  seconds, without starting the browser or spending `PROBE_TIMEOUT`. The same
  happens when the connection drops mid-probe (Chromium
  `net::ERR_CONNECTION_*`): the player wait, `PROBE_DWELL` and the extra videos
  are skipped.
* The Rutube probe needs no browser: a cycle over ~100 nodes finishes in a few
  minutes, and `HEADLESS`/`PROBE_DWELL` do not apply to it. `PROBE_TIMEOUT` still
  bounds the whole probe per node, but a node that cannot reach Rutube is dropped
  after the first failed request (~10 s) instead of consuming the whole budget.
* If a whole cycle reports no ads, the summary warns about `YOUTUBE_VIDEOS`: the
  probe list may have lost its ads for your exits. Replace the entries with a
  popular video that still shows a pre-roll in the target regions.
* Chromium needs shared memory; the compose file sets `shm_size: 1gb` (YouTube only).

## Tests

The unit tests run inside the image, so the host needs no Python setup:

```bash
docker build -t yt-ad-checker .
docker run --rm --entrypoint python yt-ad-checker -m unittest discover -s tests -t .
```

They cover tag mirroring (`clean`/`ads` flip the pair, `unreachable`/`unknown`
leave it alone, `DRY_RUN` only reports), video selection (`YOUTUBE_VIDEOS`,
`PROBE_URL`, id/URL normalisation, extra videos), the connection gate (a dead
node never reaches the probe and keeps its tags), node selection (`ONLY_TAGS`,
`SKIP_TAGS`, `MAX_NODES`), the worker port/state layout, cycle statistics (a
stopped run reports `checked < nodes` and is flagged as interrupted), and the
Rutube probe (ad-slot parsing, the two-answer rule, request timeouts and the
abort on a dead node).

## Publishing the image

`.github/workflows/yt-ad-checker-image.yml` builds and publishes the image on
every change under `tools/yt-ad-checker/` and on manual dispatch. It runs the
unit tests inside the freshly built image and pushes only when they pass:

```bash
docker pull ghcr.io/<owner>/yt-ad-checker:latest
docker run --rm -e SUBLINK_URL=... -e SUBLINK_API_KEY=... ghcr.io/<owner>/yt-ad-checker:latest
```

Every build is tagged `latest`, with its branch name and `sha-<short>`. The
registry also keeps a `buildcache` tag, so the next build reuses the heavy
Playwright layers instead of downloading them again. The job runs in forks only:
upstream ships the tool from source and keeps its own release pipeline.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `SUBLINK_API_KEY is required` | `.env` missing or key empty |
| HTTP 403 on every call | wrong/revoked access key |
| every node `unreachable` | SublinkPro URL unreachable from the container |
| every node `unknown` with `LOGIN_REQUIRED` | YouTube bot check for the exit IPs |
| `mihomo exited during start-up` | invalid node URIs; see the logged mihomo output |

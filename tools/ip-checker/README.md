# ip-checker

Containerised checker that finds which [SublinkPro](../../README.md) nodes exit
through addresses that public reputation databases still see as ordinary
ISP/residential lines — not as VPN, proxy, Tor or hosting — and keeps a
`clean-ip` tag in sync with the result.

Services that block VPN traffic (banks, streaming, some AI endpoints) do not
block a country: they block addresses that appear in VPN or hosting lists. A node
can therefore pass a per-service check and still be useless, because its address
is already known as a datacenter. This tool asks the reputation databases the
question directly.

For every node the tool routes a "what is my IP" request through a private mihomo
instance, then hands that address to every enabled source. A node whose address
is not flagged anywhere gets the tag; a node flagged as VPN/proxy/Tor/hosting
loses it. Nodes that cannot answer are left untouched.

```
SublinkPro API ──► node list ──► mihomo (one proxy per node)
                                      │
                     api.ipify.org ───┴──► exit address (read through the node)
                                      │
              ┌───────────────────────┼───────────────────────┐
              ▼                       ▼                       ▼
        iplocate.io              ip-api.com             proxycheck / IPQS / vpnapi
     privacy.is_vpn / is_hosting   hosting / proxy        proxy=yes · security.vpn
              └───────────────────────┼───────────────────────┘
                                      ▼
                        clean / flagged / unknown
                                      ▼
                 SublinkPro tag clean-ip (batch-add / batch-remove)
```

## Sources

| `SOURCES` key | Key needed | What it reports | Default |
|---|---|---|---|
| `iplocate` | no | `privacy.is_vpn`, `is_proxy`, `is_tor`, `is_hosting`, `is_abuser`, `is_icloud_relay`, `is_bogon` | ✅ |
| `ipapi` (alias `ip-api`) | no | `hosting`, `proxy` flags (plain HTTP only on the free tier) | ✅ |
| `proxycheck` | optional | `proxy: yes/no`, address `type` (e.g. `VPN`), `risk` | – |
| `ipqualityscore` (alias `ipqs`) | yes | `vpn`, `proxy`, `tor`, `fraud_score ≥ 90` | – |
| `vpnapi` | yes | `security.vpn`, `proxy`, `tor`, `relay` | – |

The two default sources need no account. `SOURCES` takes a comma separated list;
enabling a keyed source without its key is a configuration error instead of a
silent `unknown` on every node:

```bash
SOURCES=iplocate,ipapi,proxycheck,ipqualityscore
PROXYCHECK_KEY=...
IPQUALITYSCORE_KEY=...
```

## How a node is classified

| Verdict | Meaning | Tag |
|---|---|---|
| `clean` | no enabled source flagged the address, and at least `REQUIRE_CLEAN` sources actually answered | `clean-ip` added |
| `flagged` | at least one source recognised VPN / proxy / Tor / hosting; the log names the flag (`ipapi: hosting`) | `clean-ip` removed |
| `unknown` | no usable answer: quota, HTTP error, changed payload, no exit address, node unreachable | unchanged |

A single `flagged` answer wins over any number of clean ones: one database
recognising a VPN is enough to withhold the claim. `unknown` never moves a tag,
so a quota exhausted at 03:00 cannot drop yesterday's result.

Nodes holding several URI links are checked link by link. The node is `clean`
only when **every** link is clean, `flagged` when any link is flagged, and
`unknown` when a link could not be checked — the tag describes the node, not one
of its entries.

`REQUIRE_CLEAN` (default `1`) raises the bar: set it to the number of enabled
sources to demand that all of them agree.

### How the exit address is read

The address is read **through the node**, because NAT, extra hops and round-robin
egress mean the address is only observable from the other side. Three endpoints
are tried in order (`EXIT_IP_URL`, then `https://icanhazip.com` and
`https://ifconfig.me/ip`), and an answer is accepted only when it is a routable
unicast address — a private, loopback or bogon answer is rejected instead of
being looked up.

Every reputation lookup then goes through the same node and names the discovered
address explicitly, so a rotating exit cannot mix two addresses inside one
verdict.

## Quick start

```bash
cd tools/ip-checker
cp .env.example .env
# edit .env: set SUBLINK_URL and SUBLINK_API_KEY
docker compose up -d --build
docker compose logs -f
```

The API key is created in SublinkPro under **Settings → Access Keys** and is sent
as the `X-API-Key` header. It needs node read and tag write scopes.

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
| `SOURCES` | `iplocate,ipapi` | Comma separated IP reputation sources |
| `IPLOCATE_API_KEY` | – | Optional, raises the iplocate quota |
| `PROXYCHECK_KEY` | – | Optional, `proxycheck` works keyless at 100 queries a day |
| `IPQUALITYSCORE_KEY` | – | Required by `ipqualityscore` |
| `VPNAPI_KEY` | – | Required by `vpnapi` |
| `CLEAN_TAG` | `clean-ip` | Tag written on clean nodes |
| `REQUIRE_CLEAN` | `1` | How many sources must answer clean before the tag is written |
| `CHECK_INTERVAL` | `24h` | `30m`, `24h`, `1d`, `1h30m` or plain seconds |
| `RUN_ON_START` | `true` | Run a cycle immediately at start-up |
| `ONCE` | `false` | Run one cycle and exit (a manual run) |
| `ONLY_TAGS` | – | Check only nodes carrying at least one of these tags (comma separated, case-insensitive) |
| `SKIP_TAGS` | – | Never check nodes carrying any of these tags |
| `MAX_NODES` | `0` | Stop after this many nodes; `0` means no limit |
| `WORKERS` | `1` | Check this many nodes in parallel (each worker gets its own mihomo) |
| `ENSURE_TAG` | `true` | Create `CLEAN_TAG` when it does not exist yet |
| `DRY_RUN` | `false` | Log decisions without touching tags |
| `PROBE_TIMEOUT` | `15` | Per-request timeout, seconds |
| `EXIT_IP_URL` | `https://api.ipify.org?format=json` | First endpoint used to read the address; two public fallbacks follow it |
| `REACHABILITY_URL` | `https://www.gstatic.com/generate_204` | Used to drop a dead node before any lookup |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warning`, `error` |

## Scheduling and long cycles

Reputation moves far slower than service availability, and the keyless sources
have daily quotas, so one pass a day is the default. A pass over ~100 nodes takes
a few minutes. The scheduler:

* runs **one cycle at a time** — lookups are never overlapped;
* keeps a start-anchored cadence while cycles fit inside the period;
* when a cycle outlives its period, logs how many scheduled runs were skipped and
  anchors the next run to the end of the current one;
* takes an exclusive lock on `/data/scheduler.lock`, so two containers sharing
  the volume cannot run at the same time.

### One-off runs and subsets

`ONCE=true` runs a single cycle and exits — handy for a manual check:

```bash
docker run --rm --init --env-file .env \
  -e ONCE=true -e DRY_RUN=true -e MAX_NODES=10 ip-checker:latest
```

`ONLY_TAGS` / `SKIP_TAGS` / `MAX_NODES` shrink the work list, for example to
re-check only nodes that already carry `clean-ip`, or to smoke-test ten nodes
before a full run. Tags are matched case-insensitively, `SKIP_TAGS` wins over
`ONLY_TAGS`, and the filter is applied before any lookup, so skipped nodes cost
nothing.

### Parallel workers

`WORKERS=N` checks N nodes at once. Each worker starts its own mihomo (proxy port
`MIHOMO_PROXY_PORT + i`, API port `+ i`), so the cost is RAM, not API quota:
budget roughly 100 MB per worker. With more than one worker each log line is
prefixed with `W1`/`W2`/… so interleaved output can be untangled.

## Output

```
2026-11-09 12:00:02 INFO    main     === check cycle started ===
2026-11-09 12:00:03 INFO    sublink  created tag 'clean-ip'
2026-11-09 12:00:05 INFO    main     [  1/ 97] #11    Netherlands · Amsterdam            clean   45.86.12.7       2.4s  -> +clean-ip
2026-11-09 12:00:07 INFO    main     [  2/ 97] #19    Germany · Falkenstein              flagged 5.9.0.1          1.9s  5.9.0.1: iplocate: hosting; ipapi: hosting -> -clean-ip
2026-11-09 12:00:09 INFO    main     [  3/ 97] #20    Singapore                          unknown -                9.0s  api.ipify.org: timed out; icanhazip.com: timed out
2026-11-09 12:05:41 INFO    main     cycle done in 5m38s: checked 97/97 with 3 worker(s), 3.5s/node | tags added=41 removed=51 errors=0
2026-11-09 12:05:41 INFO    main       clean-ip     clean=41 flagged=51 unknown=5
```

The last cycle summary is also written to `/data/state.json`: `nodes`, `checked`,
`workers`, `seconds_per_node`, `interrupted`, the per-status counters and the tag
counters. A run stopped by SIGTERM keeps `checked < nodes`, so a partial cycle
cannot be mistaken for a complete one.

`LOG_LEVEL=debug` adds a line per link with every source verdict and the reason,
which is what to use when a verdict looks wrong.

## How tags are applied

| Verdict | Added | Removed |
|---|---|---|
| `clean` | `CLEAN_TAG` (`clean-ip`) | – |
| `flagged` | – | `CLEAN_TAG` (`clean-ip`) |
| `unknown` | – | – |

* additions use `POST /api/v1/tags/node/batch-add`, removals use
  `POST /api/v1/tags/node/batch-remove` with a single `tagNames` entry — every
  other tag on the node stays intact;
* only differences are written: a node that is already in the target state costs
  no API call;
* with `DRY_RUN=true` the same decisions are logged as `would add …, remove …`.

Exactly one tag is written, which is what makes it composable: `clean-ip` can be
combined with any tag another checker writes (`ai-*`, `youtube`, airport labels)
without a conflict.

## Notes and limitations

* **`clean` means "not flagged by the enabled sources", not "guaranteed
  residential".** A brand-new address that no database knows yet comes back clean
  from both keyless sources. For a strict filter enable more sources and set
  `REQUIRE_CLEAN` to their number.
* Public CDN/anycast addresses are flagged by design: `1.1.1.1` answers
  `iplocate: abuser` and `ipapi: hosting` — a shared address is not a residential
  line.
* Quotas are per exit address, because every lookup leaves through the node:
  `ip-api.com` allows 45 requests a minute per address and `proxycheck.io` 100
  queries a day without a key. One lookup per node per cycle stays far below
  both.
* The `ip-api.com` free tier answers over plain HTTP only. A hostile exit could
  rewrite that answer; the other sources use HTTPS, and one `flagged` answer from
  any of them still wins.
* The address is looked up as the first working endpoint sees it (IPv4 in
  practice). A node that exits only over IPv6 is judged by whatever
  `icanhazip.com`/`ifconfig.me` return for it.
* Reputation is point-in-time. An address can be reclassified after abuse, and a
  re-used address can be flagged for a previous owner's behaviour. Keep
  `CHECK_INTERVAL` at a day or less if the airport rotates addresses often.
* A node that cannot be reached or cannot answer any endpoint keeps its previous
  tag (`unknown`). That is deliberate: an outage must not rewrite results.
* This tool does not replace the per-service checkers. `clean-ip` says the
  address is not a known VPN exit; the AI checker (`tools/ai-checker`) says
  whether a specific AI service accepts it.

## Tests

The unit tests run inside the image, so the host needs no Python setup:

```bash
docker build -t ip-checker .
docker run --rm --entrypoint python ip-checker \
    -m unittest discover -s tests -t .
```

They cover every source parser (clean, flagged, quota, HTTP error, changed
payload, missing key), the alias/key resolution, the folding rules (`flagged`
wins, `REQUIRE_CLEAN`), the exit address parser and its fallback chain (a private
answer is rejected), node selection (`ONLY_TAGS`, `SKIP_TAGS`, `MAX_NODES`), the
per-link merge (all links clean, one flagged wins, an unchecked link keeps the
node `unknown`), the worker port/state layout, `DRY_RUN` and the tag diff.

The network path was verified end-to-end as well: a local SOCKS5 exit behind a
real mihomo instance, a stub SublinkPro API, and both directions — a residential
address produced `+clean-ip`, a hosting address produced `-clean-ip` with
`iplocate: hosting; ipapi: hosting`.

## Publishing the image

`.github/workflows/ip-checker-image.yml` builds and publishes the image on every
change under `tools/ip-checker/` and on manual dispatch. It runs the unit tests
inside the freshly built image and pushes only when they pass:

```bash
docker pull ghcr.io/<owner>/ip-checker:latest
docker run --rm -e SUBLINK_URL=... -e SUBLINK_API_KEY=... ghcr.io/<owner>/ip-checker:latest
```

Every build is tagged `latest`, with its branch name and `sha-<short>`. The job
runs in forks only: upstream ships the tool from source and keeps its own release
pipeline.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `SUBLINK_API_KEY is required` | `.env` missing or key empty |
| `ipqualityscore needs IPQUALITYSCORE_KEY` | the source is in `SOURCES` without its key |
| HTTP 403 on every call | wrong/revoked access key |
| every node `unknown` with `link unreachable` | the nodes are down, or egress is filtered inside the container |
| every node `unknown` with `api.ipify.org: …` | the exits block the "what is my IP" endpoints; set `EXIT_IP_URL` to a reachable one |
| every node `unknown` with `rate_limited` | the source quota is exhausted; wait for the reset, add a key, or drop the source |
| every node `flagged` | the nodes really are datacenter exits — that is the answer, not a bug |
| `mihomo exited during start-up` | invalid node URIs; see the logged mihomo output |
| `configuration error: unknown source` | a typo in `SOURCES`; the message lists the known keys |

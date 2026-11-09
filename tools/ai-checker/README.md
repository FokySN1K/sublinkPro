# ai-checker

Containerised checker that tells which [SublinkPro](../../README.md) nodes can
reach **ChatGPT, Claude, Gemini, Copilot, Perplexity, Grok, Mistral Le Chat,
DeepSeek, Qwen and Poe**, and keeps one tag pair per service in sync with the
result.

Every node is dialled through a private mihomo instance; each service is probed
with a plain HTTP request through that proxy, so no browser is involved. A node
that the service accepts gets `ai-<service>`, a node the service refuses by
region gets `ai-<service>-blocked`, and everything else — a captcha, a rate
limit, a timeout — leaves the node exactly as it was.

```
SublinkPro API ──► node list ──► mihomo (one proxy per node)
                                      │
                    ┌─────────────────┴──────────────────┐
                    ▼                                    ▼
        10 HTTP probes in parallel          /proxies/PROBE/delay (6 s gate)
        api.openai.com, claude.ai, …                        │
                    │                                       │
                    └──────── available / restricted / unknown
                                      │
                                      ▼
              SublinkPro tags: ai-<service> / ai-<service>-blocked
```

> SublinkPro already checks OpenAI, Claude and Gemini on the node-check page
> (one `UnlockSummary` per node, filtered by subscription `UnlockRules`). This
> tool is the complement: it covers ten services, writes **per-service tags**
> that any subscription or client can filter on, and never lets a bot check
> turn into a "blocked" verdict.

## Services

| `SERVICES` key | Service | Probe |
|---|---|---|
| `chatgpt` (alias `openai`) | ChatGPT / OpenAI | `api.openai.com/compliance/cookie_requirements` |
| `claude` | Claude | `claude.ai/` |
| `gemini` | Gemini | `gemini.google.com/` |
| `copilot` | Microsoft Copilot | `copilot.microsoft.com/` |
| `perplexity` | Perplexity | `perplexity.ai/` |
| `grok` | Grok (xAI) | `grok.com/` |
| `mistral` | Mistral Le Chat | `chat.mistral.ai/` |
| `deepseek` | DeepSeek | `chat.deepseek.com/` |
| `qwen` | Qwen Chat | `chat.qwen.ai/` |
| `poe` | Poe | `poe.com/` |

Empty `SERVICES` means all ten. Missing a service costs one request per node;
that is why `SERVICES` exists — a 300-node setup that only cares about ChatGPT
and Claude spends 2 requests per node instead of 20.

## Verdicts

| Verdict | Meaning | Tags |
|---|---|---|
| `available` | the service answers normally from this exit | `+ai-<service>`, `-ai-<service>-blocked` |
| `restricted` | the service itself says the region is unsupported | `+ai-<service>-blocked`, `-ai-<service>` |
| `partial` | only some surfaces work — ChatGPT web refused but the app reachable (or the reverse) | – |
| `unknown` | the answer proves nothing: challenge, 429, 5xx, timeout, dead node | – |

The `unknown` bucket is the important one. A Cloudflare interstitial, a rate
limit or an `ERR_CONNECTION_RESET` is not evidence of a country block, so it
never rewrites yesterday's verdict. This is the failure mode of naive checkers:
they grep for "unavailable" in a challenge page and mark half the fleet as
blocked.

## How a node is classified

Shared guards run before any provider rule:

* `cf-mitigated: challenge` header or a Cloudflare page (`just a moment`,
  `cf-chl`, `__cf_chl`, `attention required! | cloudflare`) → `unknown`;
* HTTP `429` → `unknown`; HTTP `5xx` → `unknown`;
* no answer at all (timeout, TLS reset, proxy error) → `unknown`.

Then the provider-specific rules apply:

**ChatGPT** — `unsupported_country` in the compliance response, or a plain
`403`, means `restricted`; any `2xx`/`3xx` means `available`. The verdict is
then cross-checked against `ios.chat.openai.com`, which answers the word `vpn`
to an exit it refuses: when the web and the app disagree the node becomes
`partial` (`web_only` / `mobile_app_only`), and an empty or challenged app
answer leaves the compliance verdict alone. This is the same endpoint pair the
core `services/unlock` checker uses, so both agree.

**Claude** — a redirect that lands on `app-unavailable-in-region`, a regional
phrase, or `403` means `restricted`; a `2xx` login page means `available`.

**Gemini** — `"45631641":true` in the page means `available`, a "Gemini isn't
available in your country" phrase means `restricted`. If neither marker is
found the verdict is `unknown` (`gemini_marker_missing`): Google rewrites that
page often, and guessing a block from a missing flag would be wrong. `403` is
`unknown` here as well, because Gemini answers `403` to plenty of healthy exits.

The flag is embedded inside an escaped JSON blob (`\"45631641\":true`), so both
spellings are matched.

**Perplexity** — the landing page sits behind a managed challenge for most
datacenter exits, while `perplexity.ai/api/auth/session` answers an anonymous
visitor with `{}`. The JSON endpoint is therefore probed first and the page is
only used as a fallback.

**Claude, Mistral, Poe** — these domains protect every HTML path with a
Cloudflare managed challenge that only a JavaScript client can pass, so from a
datacenter exit they usually report `unknown` even with a browser-like TLS
fingerprint. That is deliberate: a challenge says nothing about the region, and
`unknown` never overwrites a tag. Exits that are not challenged (residential,
mobile, some hosts) resolve normally.

**Everything else** — a regional phrase from the shared vocabulary makes the
node `restricted`, a `2xx`/`3xx` makes it `available`, and a `403` stays
`unknown`: without a regional phrase in the body, `403` is far more often a bot
rule than a country block.

`loc=XX` from the service's own `cdn-cgi/trace` is attached to the log line as a
hint when the exit is in a commonly blocked region. It never decides the
verdict — the service is the authority.

## Who actually blocks regions

Vendor-level restrictions as of November 2026, for orientation only; the
checker is the source of truth for a concrete exit.

| Service | Not offered by the vendor in |
|---|---|
| ChatGPT / OpenAI | Belarus, China, Cuba, Iran, North Korea, Russia, Syria, Venezuela (Hong Kong and Macau are absent from the supported list) |
| Claude | a similar list; anything not on Anthropic's supported-regions list is unsupported |
| Gemini | Afghanistan, Belarus, China, Cuba, Iran, North Korea, Russia, Syria (plus Hong Kong/Macau for the app) |
| Copilot | served globally, but Microsoft accounts and some features are region-pinned |
| Perplexity | no formal country ban; blocked at ISP level in Russia and unavailable behind some national filters |
| Grok | console opened in the EU in July 2026; blocked in Indonesia and Malaysia since January 2026 |
| Mistral Le Chat | no state-level bans; GDPR-related blocks in some regions |
| DeepSeek | no vendor ban, but blocked or banned on government networks in Italy, South Korea, Australia, Taiwan and others |
| Qwen | offered globally |
| Poe | no explicit country bans; upstream model providers may refuse a region |

Two different things get conflated in "no access from country X":

* **the vendor refuses the region** — the service answers with a block page, a
  redirect or `unsupported_country` → `restricted`, tag `ai-<service>-blocked`;
* **the local network blocks the service** — the connection dies (RKN, DNS
  filtering, SNI blocking) → the probe times out → `unknown`, tag untouched.

The second case looks identical to "the node is down", which is why it stays
`unknown` instead of being reported as a restriction.

## Quick start

```bash
cd tools/ai-checker
cp .env.example .env
# edit .env: set SUBLINK_URL and SUBLINK_API_KEY
docker compose up -d --build
docker compose logs -f
```

The API key is created in SublinkPro under **Settings → Access Keys** and is
sent as the `X-API-Key` header. A dry run is the safest first step:

```bash
docker compose run --rm -e DRY_RUN=true -e ONCE=true -e MAX_NODES=5 ai-checker
```

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
| `SERVICES` | all | Comma separated keys from the table above; `openai` is an alias for `chatgpt` |
| `TAG_PREFIX` | `ai-` | Tags are `<prefix><service>` and `<prefix><service>-blocked` |
| `CHECK_INTERVAL` | `6h` | `30m`, `6h`, `1d`, `1h30m` or plain seconds |
| `RUN_ON_START` | `true` | Run a cycle immediately at start-up |
| `ONCE` | `false` | Run one cycle and exit (a manual run, ignores `RUN_ON_START`) |
| `ONLY_TAGS` | – | Check only nodes carrying at least one of these tags |
| `SKIP_TAGS` | – | Never check nodes carrying any of these tags |
| `MAX_NODES` | `0` | Stop after this many nodes; `0` means no limit |
| `WORKERS` | `1` | Check this many nodes in parallel (max 8, each worker gets its own mihomo) |
| `ENSURE_TAG` | `true` | Create missing tags (green `available`, red `blocked`) |
| `DRY_RUN` | `false` | Log the decisions without touching tags |
| `PROBE_TIMEOUT` | `15` | Per-request timeout, seconds |
| `REACHABILITY_URL` | `https://www.gstatic.com/generate_204` | Any answer proves the node carries traffic; mihomo dials it before the probes |
| `PROBE_USER_AGENT` | built-in Chrome UA | Override for diagnostics; several services challenge a bare `python-requests` UA |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warning`, `error` |

Internal knobs (`STATE_DIR`, `MIHOMO_BIN`, `MIHOMO_API`, `MIHOMO_PROXY_PORT`)
already have container-friendly defaults and rarely need changing.

## Tags

For every service a node ends up with exactly one of two tags, or with neither:

| Verdict | Added | Removed |
|---|---|---|
| `available` | `ai-<service>` | `ai-<service>-blocked` |
| `restricted` | `ai-<service>-blocked` | `ai-<service>` |
| `unknown` | – | – |

* additions use `POST /api/v1/tags/node/batch-add`, removals use
  `POST /api/v1/tags/node/batch-remove` — every other tag on the node stays
  intact;
* only differences are written: a node already in the target state costs no API
  call;
* with `DRY_RUN=true` the same decisions are logged as `would add …, remove …`;
* `TAG_PREFIX` lets one instance check "real" nodes (`ai-`) and another check a
  staging subset (`stg-ai-`) without collisions.

Use the tags in a subscription (`Tag` filters) or in a client to build a
per-service outbound list — for example a subscription that only keeps nodes
tagged `ai-chatgpt`.

## Scheduling and workers

* one cycle runs at a time — probes are never overlapped;
* the cadence is start-anchored while cycles fit inside the period; when a cycle
  outlives it, the summary logs how many runs were skipped and the next run is
  anchored to the end of the current one;
* `/data/scheduler.lock` is exclusive, so two containers sharing the volume
  cannot run at once;
* `WORKERS=N` checks N nodes at once. Each worker starts its own mihomo
  (proxy port `+i`, API port `+i`) and writes its own state under
  `worker-<n>/`; with `WORKERS=1` the configured ports and directory are used
  untouched. Parallelism here is cheap: an HTTP-only worker needs tens of
  megabytes, not a browser.

A full pass over ~200 nodes with all ten services and `WORKERS=2` takes a few
minutes; a node that cannot open a connection is dropped by the 6 s
reachability gate instead of spending `PROBE_TIMEOUT` ten times.

### One-off runs and subsets

```bash
docker run --rm --init --env-file .env \
  -e ONCE=true -e SERVICES=chatgpt,claude -e MAX_NODES=10 \
  ghcr.io/<owner>/ai-checker:latest
```

Tags are matched case-insensitively, `SKIP_TAGS` wins over `ONLY_TAGS`, and the
filter is applied before any probe.

## Output

```
2026-11-09 10:02:11 INFO    main     === check cycle started ===
2026-11-09 10:02:12 INFO    main     fetched 97 node(s) from http://sublinkpro:8000
2026-11-09 10:02:19 INFO    main     [  1/ 97] #11    Москва          chatgpt+ claude+ gemini+ copilot+ perplexity+ grok+ mistral+ deepseek+ qwen+ poe+   7.9s -> +ai-chatgpt -ai-chatgpt-blocked
2026-11-09 10:02:24 INFO    main     [  2/ 97] #19    Гонконг         chatgpt- claude- gemini? copilot+ perplexity- grok? mistral+ deepseek+ qwen+ poe+   5.6s chatgpt: unsupported_country loc=HK; claude: app_unavailable_in_region; perplexity: region_blocked -> +ai-chatgpt-blocked -ai-chatgpt …
2026-11-09 10:02:26 WARN    main     [  3/ 97] #20    Краснодар       chatgpt? claude? gemini? copilot? perplexity? grok? mistral? deepseek? qwen? poe?   2.1s -> (no change)
2026-11-09 10:09:52 INFO    main     cycle done in 7m41s: checked 97/97 with 2 worker(s), 4.8s/node | tags added=41 removed=17 errors=0
2026-11-09 10:09:52 INFO    main       chatgpt    available=52 restricted=38 partial=0 unknown=7
2026-11-09 10:09:52 INFO    main       claude     available=44 restricted=45 partial=0 unknown=8
```

`+` available, `-` restricted, `~` partial, `?` unknown. The last cycle is also
written to `/data/state.json`, including how many nodes were planned (`nodes`),
how many finished (`checked`), the `workers` count, the `seconds_per_node` pace
and `interrupted` — a run stopped by SIGTERM keeps `checked < nodes`, so a
partial cycle cannot be mistaken for a complete one.

## Notes and limitations

* Probes are HTTP-only. That keeps the image at ~280 MB (Python slim plus the
  mihomo binary, no Chromium, no virtual display), but a service that hides its
  block behind JavaScript will answer `available` where a browser would show a
  block page. The current markers are chosen so that this fails towards
  `unknown`, not towards a false `restricted`.
* Because `unknown` never touches a tag, a node keeps its previous tag when a
  service cannot be judged: a node that was tagged `ai-claude` months ago stays
  tagged until a cycle resolves it again. `DRY_RUN=true` plus two comparable
  cycles is the way to see how much of the fleet is actually being decided.
* Cloudflare-protected domains answer `unknown` from exits with a bad IP
  reputation. Adding more services of that kind does not add information —
  check the summary counters after the first cycle and drop what stays at
  `unknown=100%`.
* The checker reads the raw node links from `GET /api/v1/nodes/get`, so a node
  is tested exactly as its URI describes. Chain proxy (`DialerProxyName`) and
  other Clash-only extras from the export layer are not applied.
* A node with several URI links is checked link by link: one working link is
  enough for `available`, while `restricted` requires that no link worked.
* Verdicts are point-in-time. Vendors A/B rollout regions, so a node can flip
  between cycles; comparing two `DRY_RUN` cycles is a good way to spot unstable
  nodes.
* IP-based region detection is what the vendor sees, not what your account sees.
  A paid plan region, a phone number or a billing country can still block
  sign-up even when the exit is `available`; conversely a `restricted` verdict
  is definitive.
* The tool does not log in anywhere and does not send credentials — it only
  reads what an anonymous visitor is served.
* `PROBE_TIMEOUT` bounds a single request, and the ten probes of one node run in
  a small pool (`5`), so a node with half-dead connectivity costs at most ~2
  timeouts per service batch.

## Tests

The unit tests run inside the image, so the host needs no Python setup:

```bash
docker build -t ai-checker .
docker run --rm --entrypoint python ai-checker -m unittest discover -s tests -t .
```

They cover the provider rules (region markers, challenge/429/5xx handling, the
ChatGPT web/app `partial` merge, the escaped Gemini flag, the Perplexity JSON
endpoint fallback, per-provider thresholds, the `openai` alias), configuration
parsing (`CHECK_INTERVAL`, `SERVICES`, defaults and validation), and the tag
decisions (`available`/`restricted` flip the pair, `unknown` is a no-op,
`DRY_RUN` only reports, repeated verdicts cost no API call).

## Publishing the image

`.github/workflows/ai-checker-image.yml` builds and publishes the image on every
change under `tools/ai-checker/` and on manual dispatch. It runs the unit tests
inside the freshly built image and pushes only when they pass:

```bash
docker pull ghcr.io/<owner>/ai-checker:latest
docker run --rm -e SUBLINK_URL=... -e SUBLINK_API_KEY=... ghcr.io/<owner>/ai-checker:latest
```

Every build is tagged with its branch name and `sha-<short>`; `latest` follows
the default branch only, so a feature push never changes what production pulls.
The job runs in forks only: upstream ships the tool from source and keeps its
own release pipeline.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `SUBLINK_API_KEY is required` | `.env` missing or the key is empty |
| HTTP 401/403 on every call | wrong or revoked access key |
| every node is all `?` with `link unreachable` | SublinkPro URL or the node URIs are unreachable from the container |
| every node is `?` with `cloudflare_challenge` | the exits are challenged; raise `PROBE_TIMEOUT`, or accept that HTTP-only probing cannot judge these services |
| `gemini_marker_missing` on every node | Gemini changed its page; inspect a `LOG_LEVEL=debug` run |
| `mihomo exited during start-up` | invalid node URIs; see the logged mihomo output |
| tags are created but never change | `DRY_RUN=true`, or every verdict is `unknown` |
| `another ai-checker instance is already running` | a second container shares the same `/data` volume |

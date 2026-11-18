English | [简体中文](share-groups.zh-CN.md)

# Share Groups, Placeholders and Device Binding

Subscription delivery has three levels. Each level has one job, and settings flow from the top down.

| Level | Entity | Responsibility |
|:---|:---|:---|
| 1. Node pool | Existing subscription (`/api/v1/subcription/...`) | Which nodes are produced: filters, renaming, chain proxy, node selection. Each pool carries an immutable `unique_key`, so renaming it never breaks automation |
| 2. Share group | Share group (`/api/v1/share-groups/...`) | Delivery policy: allowed templates, hiding the config, device rules, traffic limit, expiry, telemetry |
| 3. Subscription link | Share link (`/api/v1/shares/...`) | Traffic delivery: one token, one owner, its own limits and overrides |

A link always belongs to a node pool and may belong to a share group. When it belongs to a group, the link **inherits a snapshot of the group at creation time** and can only narrow it.

---

## Share Group Settings

| Setting | Meaning |
|:---|:---|
| **Name / description** | Admin-facing labels |
| **Node pool** | The subscription (level 1) whose nodes this group serves |
| **Enabled** | Turning a group off makes every link in it return the `group_disabled` placeholder |
| **Hide config** | Forces the `mihomo_snkp` profile for every request. Non-SNKP applications receive the `unsupported_app` placeholder instead of a readable config |
| **Templates** | Which renderers the group may serve: `auto`, `mihomo`, `mihomo_snkp`, `surge`, `v2ray`. A link cannot request a template that is not listed |
| **Clash template file** | Template file used to render Clash-family profiles for every link in the group. Empty inherits the node pool's own `clash` template |
| **Surge template file** | Template file used to render Surge profiles for every link in the group. Empty inherits the node pool's own `surge` template |
| **mihomo → mihomo_snkp** | When SNKP asks for `clash`/`mihomo`, it receives `mihomo_snkp` |
| **Allow legacy clients** | When on, clients without device headers keep working (unbound, nothing is bound). When off, they receive the `unsupported_app` placeholder |
| **Device mode** | `open` (first seen binds silently), `auto_bind` (binds until the active limit is hit), `approve` (new devices stay `pending` until approved in the UI) |
| **Max devices** | Active-device limit; `0` means unlimited. Pending devices do not consume a slot |
| **Traffic limit** | Quota in bytes; `0` means unlimited. Only values reported by the client count |
| **Expiry** | Never, after N days, or at a fixed date/time |
| **Telemetry** | Master switch plus three independent report intervals: `sessions`, `nodes`, `reliability` (300–86400 seconds each, default 3600) |

A node pool only aggregates and renames nodes; links are created and managed inside a share group. Renaming a node pool is safe: the pool keeps its `unique_key`, links already created from it keep working, and API clients can send `unique_key` instead of `oldname` when updating a pool.

Selecting a Clash or Surge template file on a group overrides the renderer template for **every** link served by that group, including links that were created before the selection. Clearing the field falls back to the node pool's template.

The template list is enforced by the server: a request whose resolved renderer is not listed receives the `template_disabled` placeholder instead of a config. The client URLs dialog mirrors the same allowlist, so it only offers links that will actually be served; hidden groups show only *Auto detect* and *Mihomo SNKP*. Sub-Store export targets convert the profile themselves and stay outside the allowlist.

## Subscription Link Snapshots

Creating a link inside a group copies the group's traffic limit, device limit, expiry and telemetry intervals into the link. Later edits to the group policy (limits, expiry, telemetry) do **not** rewrite existing links; changing the group's templates, `hide_config`, device mode or enabled flag applies immediately.

Each link can override:

- name and token (random by default, both editable),
- traffic limit, device limit,
- expiry,
- all three telemetry intervals.

Override fields left empty (`0`, or "same as group") follow the snapshot.

Generated tokens are 32 lowercase hex characters (16 random bytes), which the share form shows under the token field. Custom tokens are accepted and may be up to 64 characters long.

Links that predate groups keep `group_id = NULL` and behave exactly as before: no device binding, no placeholders for policy reasons, no SNKP auto-detection changes.

---

## Client Detection

For links inside a group the server resolves the renderer in this order:

1. `hide_config` → `mihomo_snkp`, always.
2. Explicit `?client=` value: `mihomo_snkp`, `clash`/`mihomo`/`clashmeta` (→ `mihomo`, or `mihomo_snkp` when SNKP asks and the group allows it), `surge`, `v2ray`/`uri`, or any enabled Sub-Store target.
3. SNKP detection via `User-Agent` (contains `snkp`) or the `X-Sublink-Client: snkp` marker → `mihomo_snkp`.
4. `User-Agent` detection: `surge` → Surge, `clash`/`mihomo` → mihomo, `v2ray`/`sing-box`/`shadowrocket` → v2ray.
5. Anything else (empty or unknown `User-Agent`) with `auto` → **`mihomo_snkp`**.

Legacy links (no group) keep the original behaviour of the auto mode.

The resolved template is checked against the group's template list before the profile is built. `auto` is a detection mode, not a renderer: the concrete result (`mihomo`, `mihomo_snkp`, `surge`, `v2ray`) must be allowed. Sub-Store targets are exempt because the sidecar performs its own conversion.

## Node Unique Keys

Every rendered node carries a stable `sublinkpro_unique_key` field next to its name, derived from the node's content hash (falling back to the link hash). It exists because **name processing** can rename every node in a pool to the same value (for example `name processing → 1`): the display name stops being a usable identity, while the injected key stays unique per node.

| Renderer | Where the key lives |
|:---|:---|
| Clash / Mihomo | `sublinkpro_unique_key` proxy key in the YAML item |
| `vless://`, `trojan://`, `ss://`, `hysteria2://`, … | `sublinkpro_unique_key` query parameter, appended after the rename step |
| `vmess://` | extra field in the base64 JSON payload |
| `ssr://`, Surge and unregistered schemes | no extension point — emitted verbatim without the key |

Standard clients ignore it: Clash/Mihomo skip unknown proxy keys, Xray ignores unknown JSON fields and URI parsers ignore unknown query parameters. The field never participates in the content hash, so hashing a link that already carries the key yields the same node hash — it cannot feed back into the value it was derived from.

## Device Binding

Device identity has two parts:

| Header | Meaning |
|:---|:---|
| `X-HWID` | Owner/account identity. One HWID may cover several installations |
| `X-Device-ID` | One installation on one machine |
| `X-Device-Name` | Optional friendly name shown in the admin UI |
| `X-Device-Platform` | Optional platform label (iOS, Android, Windows, …) |
| `X-App-Version` | Optional client version |
| `X-Sublink-Client` | Optional explicit client marker (`snkp`) |

Statuses:

- `active` — serves the profile and consumes a slot.
- `pending` — serves the `device_pending` placeholder and does not consume a slot.
- `blocked` — serves the `device_blocked` placeholder.

Each active device also receives a per-device secret. Responses carry `X-Sublink-Device-Status`, and for active devices `X-Sublink-Device-Secret` and `X-Sublink-Device-Name` (URL-encoded). `HEAD` requests never bind a device.

---

## Placeholders (Заглушка)

A placeholder is an editable response served instead of a profile when a request cannot be served. Each placeholder has a name, a case key, a title, a content body and an enabled flag.

| Case | Served when |
|:---|:---|
| `device_limit` | The active-device limit is already reached |
| `device_pending` | The device waits for approval (`approve` mode) |
| `device_blocked` | The device was blocked by an administrator |
| `quota_exhausted` | Reported traffic reached the link's limit |
| `expired` | The link expired |
| `group_disabled` | The link's group is disabled |
| `template_disabled` | The resolved renderer is not in the group's template list |
| `unsupported_app` | The request cannot be served to this application (`hide_config`, or legacy clients disallowed) |
| `invalid_token` | The token is unknown |

Resolution order for a case: **link override → group mapping → the default placeholder in the shared library**. Placeholders whose case key has no entry fall back to the built-in default text.

Placeholder content supports these single-brace variables:

| Variable | Value |
|:---|:---|
| `{case}` | The case key |
| `{share_name}` | Link name |
| `{group_name}` | Group name |
| `{device_name}` | Device name, when known |
| `{limit}` | Device limit |
| `{used}` | Reported traffic, formatted |
| `{expires_at}` | Expiry label |

The placeholder is rendered in the same format the client asked for (mihomo YAML, Surge, v2ray, or the requested Sub-Store target) and keeps the original profile envelope and filename, so clients keep the link identity and simply see a profile whose proxy list is the placeholder text.

## Response Headers

| Header | Meaning |
|:---|:---|
| `X-Sublink-Server` | Always `sublinkpro` |
| `X-Sublink-Proto` | Protocol version (`1`) |
| `X-Sublink-Template` | The renderer that was actually used |
| `X-Sublink-Features` | Comma list: `status`, `stubs`, plus `devices` for managed links and `telemetry` when telemetry is on |
| `X-Sublink-Device-Status` | `active`, `pending`, `blocked`, `unbound`, `limit`, `new`, `error` |
| `X-Sublink-Device-Secret` | Per-device secret. Also present on placeholder responses when the request resolves to a bound device, so SNKP never replaces a working profile with a stub |
| `X-Sublink-Device-Name` | URL-encoded device name, active devices only |
| `X-Sublink-Reason` | The placeholder case that was served, if any |
| `X-Sublink-Stub` | The placeholder case that was actually rendered |
| `X-Sublink-Report-Interval-Sessions/Nodes/Reliability` | Telemetry intervals in seconds, when telemetry is enabled |
| `subscription-userinfo` | `upload=<reported>; download=0; total=<limit>; expire=<effective>` for links that carry a quota or an expiry; stores that read `upload + download` therefore see the full reported usage. Links without a limit and without an expiry keep the legacy airport aggregate, and placeholder responses keep the share quota |

## Status Endpoint

`GET /c/status?token=<shareToken>` returns a JSON snapshot and **never binds a device**:

- `valid`, `reason`, `template`,
- `share` (id, name, enabled, `expire_type`/`expire_days`/`expire_at`, `expire_at_unix`, `created_at`, `traffic_limit_bytes`, `reported_traffic_bytes`, `remaining_bytes`, `quota_exhausted`, `max_devices`),
- `subscription` (id, name),
- `group` (id, name, enabled, hide_config, device_mode, templates, telemetry_enabled),
- `device` (status, and details plus `secret` when active),
- `devices` (total/active/pending/blocked counts) for managed links,
- `telemetry` (enabled, the three intervals and `report_interval_seconds` — the preferred interval, mirroring `sessions`),
- `notices` — the resolved placeholder title and content, when `valid` is false.

Example (trimmed):

```json
{
  "valid": true,
  "reason": "",
  "template": "mihomo_snkp",
  "share": {
    "id": 12,
    "traffic_limit_bytes": 107374182400,
    "reported_traffic_bytes": 701245,
    "remaining_bytes": 106672936955,
    "quota_exhausted": false,
    "expire_at_unix": 1797613975
  },
  "device": {"status": "unbound"},
  "devices": {"total": 2, "active": 1, "pending": 1, "blocked": 0},
  "telemetry": {"enabled": true, "sessions": 600, "nodes": 3600, "reliability": 3600, "report_interval_seconds": 600}
}
```

`valid`, `reason` and `template` live at the **top level** of the envelope — there is no `share.valid`. The same `X-Sublink-*` headers as the profile response are set, so a client can decide whether to fetch the profile at all.

## Telemetry

`POST /c/telemetry?token=<shareToken>` ingests a traffic report. The link must belong to a group with **Telemetry** enabled; otherwise the endpoint answers `403`.

Two report modes are supported:

- **Delta mode** (default): the client sends the bytes since its previous report.
- **Absolute mode**: the client sends its installation lifetime counter (`reported_total_bytes`) together with an installation epoch (`install_id`). The server derives the delta from the stored device baseline, so a lost response or a retried request can never double count.

Delta report:

```json
{"up": 12345, "down": 678900, "report_id": "r-1"}
```

Absolute report:

```json
{"reported_total_bytes": 987654, "install_id": "install-abc", "up": 1234, "down": 0}
```

| Field | Meaning |
|:---|:---|
| `up` | Uploaded bytes since the last report. In absolute mode only splits the accepted delta between the upload and download halves of the hourly buckets |
| `down` | Downloaded bytes since the last report, same split rule in absolute mode |
| `report_id` | Optional. Delta mode only: a retry with the same id inside a 30-minute window is answered as a duplicate and writes nothing |
| `reported_total_bytes` | Absolute installation counter. Requires a resolved device; without device identity the report falls back to delta semantics |
| `install_id` | Installation epoch (trimmed, max 64 characters). A value different from the stored epoch restarts the baseline from zero, so reinstalls and moved data are not counted twice |

At least one of `up`, `down`, `reported_total_bytes` is required. Negative values, non-numeric values, values above 1 PiB per direction and absolute totals above 1 EiB are rejected with `400` and never touch the counter.

Optional headers:

| Header | Meaning |
|:---|:---|
| `X-Sublink-Device-Secret` | Strongest check: the secret must belong to a device bound to this link |
| `X-HWID` + `X-Device-ID` | When both are present and the pair is already bound, the report is attributed to that device |

Reports without any identity are accepted and counted per link. Blocked or pending devices are rejected with `403`.

Absolute mode rules (with a resolved device):

- `delta = reported_total_bytes - baseline`; when `install_id` differs from the stored epoch the baseline restarts from `0` first;
- `delta <= 0` → duplicate: `accepted_bytes` is `0`, `duplicate` is `true`, the baseline stays untouched — a stale smaller total is never mistaken for a reinstall;
- `delta > 0` → one conditional `UPDATE ... WHERE id = ? AND report_baseline_bytes = ? AND report_baseline_epoch = ?` writes the new baseline, the epoch and `reported_traffic_bytes + delta`; a lost race is re-read and retried up to three times.

Accepted reports atomically add the delta to the link's `reported_traffic_bytes`, update `last_usage_at`, refresh the cached link and appear in the access log with the `telemetry` action. Duplicates are answered normally but are not written to the access log.

Response data:

```json
{
  "reported_traffic_bytes": 701245,
  "traffic_limit_bytes": 107374182400,
  "remaining_bytes": 106672936955,
  "quota_exhausted": false,
  "last_usage_at": "2026-11-18T09:12:00Z",
  "device_status": "active",
  "device_reported_traffic_bytes": 123456,
  "accepted_bytes": 8192,
  "duplicate": false
}
```

| Field | Meaning |
|:---|:---|
| `reported_traffic_bytes` | Link lifetime counter |
| `traffic_limit_bytes` | Link quota; `0` means unlimited |
| `remaining_bytes` | `limit - reported`, floored at `0`; `0` for unlimited links |
| `quota_exhausted` | Whether the link reached its limit |
| `device_status` | Resolved device state (`active`, `unbound`, …) |
| `device_reported_traffic_bytes` | Device lifetime counter (not the link counter) |
| `accepted_bytes` | Bytes this report added; `0` for duplicates |
| `duplicate` | `true` when the report was ignored as a replay |

Once a link reaches its traffic limit, profile requests return the `quota_exhausted` placeholder while the endpoint keeps accepting reports so the final usage stays accurate.

### Device attribution and hourly usage

When a report carries a device identity (`X-Sublink-Device-Secret`, or the bound `X-HWID` + `X-Device-ID` pair), the same bytes are also written to the device:

- `reported_traffic_bytes` — a lifetime counter on the device, following the same "every accepted report is added" rule as the link,
- `report_baseline_bytes` / `report_baseline_epoch` — the last accepted absolute total and its installation epoch, returned by the admin device endpoints as well,
- `last_report_at` — the time of the last accepted report,
- hourly buckets keyed by `(device, hour)` — kept for **7 days** and pruned in the background. Same-hour reports merge into one row. The server stores UTC buckets and the UI folds them into the viewer's local hour-of-day, so "when the device is online" follows the timezone of whoever opens the chart.

A report that cannot be attributed to a bound device is still counted per link.

### Where reports are visible

- **Share Groups page** — the group card shows the aggregate **Reported traffic** (`reported_traffic_bytes` summed across the group's links, displayed as `used / limit`), the **Last report** time and a **Quota exhausted (N)** badge when any link has reached its own limit.
- **Telemetry tab** — one row per installation (HWID + device ID) aggregated across every share it uses: status, the subscriptions it consumes, lifetime and 7-day traffic, last seen and last report time. Filter by keyword, node pool or status; click a row to open the device dialog with traffic tiles, the list of subscriptions the device uses, a 24-hour usage chart and the recent activity log.
- **Links dialog** — the **Devices** action on a managed link opens its per-link device list (platform, version, status, traffic, last report) with approve, block, unblock and remove controls.
- **Link cards** — each managed link card shows `Traffic used / limit`, the remaining quota (`limit - reported`, `∞` when unlimited), the number of active and pending devices, and the last report time; links at their limit carry a **Quota exhausted** chip. Opening a link repeats the traffic summary above the client URL list.
- **Activity dialog** — telemetry reports appear with the translated **Telemetry** action label together with the reason labels of the placeholder cases (for example `quota_exhausted`).

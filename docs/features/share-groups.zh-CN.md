[English](share-groups.md) | 简体中文

# 订阅组、占位页与设备绑定

订阅分发分为三层。每一层只负责一件事，配置自上而下流动。

| 层级 | 实体 | 职责 |
|:---|:---|:---|
| 1. 节点池 | 现有订阅（`/api/v1/subcription/...`） | 产出哪些节点：过滤、重命名、链式代理、节点选择。每个节点池带有不可变的 `unique_key`，重命名不会影响自动化 |
| 2. 订阅组 | 订阅组（`/api/v1/share-groups/...`） | 下发策略：可用模板、隐藏配置、设备规则、流量限制、有效期、遥测 |
| 3. 订阅链接 | 分享链接（`/api/v1/shares/...`） | 流量分发：一个令牌、一个归属，独立的限制与覆盖项 |

链接始终属于一个节点池，可以属于一个订阅组。属于订阅组时，链接**在创建时继承组的快照**，之后只能收紧，不能放宽。

---

## 订阅组设置

| 设置 | 含义 |
|:---|:---|
| **名称 / 描述** | 管理端标签 |
| **节点池** | 该组下发哪一份订阅（第 1 层）的节点 |
| **启用** | 关闭后，组内所有链接都会返回 `group_disabled` 占位页 |
| **隐藏配置** | 所有请求强制使用 `mihomo_snkp`。非 SNKP 应用会收到 `unsupported_app` 占位页，而不是可读配置 |
| **模板** | 组允许下发的渲染器：`auto`、`mihomo`、`mihomo_snkp`、`surge`、`v2ray`。链接无法请求未列出的模板 |
| **Clash 模板文件** | 该组所有链接渲染 Clash 系配置时使用的模板文件。留空时继承节点池自身的 `clash` 模板 |
| **Surge 模板文件** | 该组所有链接渲染 Surge 配置时使用的模板文件。留空时继承节点池自身的 `surge` 模板 |
| **mihomo → mihomo_snkp** | SNKP 请求 `clash`/`mihomo` 时改发 `mihomo_snkp` |
| **允许旧版客户端** | 开启时，不带设备头的客户端仍可正常使用（不绑定任何设备）。关闭时收到 `unsupported_app` 占位页 |
| **设备模式** | `open`（首次访问静默绑定）、`auto_bind`（绑定到活跃上限为止）、`approve`（新设备保持 `pending`，等待管理端批准） |
| **设备上限** | 活跃设备上限；`0` 表示不限。`pending` 设备不占名额 |
| **流量限制** | 配额（字节）；`0` 表示不限。只统计客户端上报的数值 |
| **有效期** | 永不过期、N 天后过期、或指定时间过期 |
| **遥测** | 总开关，以及三个独立上报周期：`sessions`、`nodes`、`reliability`（各 300–86400 秒，默认 3600） |

节点池只负责聚合与重命名节点；链接统一在订阅组内创建和管理。重命名节点池是安全的：节点池保留原 `unique_key`，已创建的链接继续可用，API 调用方更新节点池时可以提交 `unique_key` 而不是 `oldname`。

在组上选择 Clash 或 Surge 模板文件后，会覆盖该组**所有**链接所用的渲染模板，包括选择之前已经创建的链接。清空该字段则回退到节点池的模板。

模板列表由服务端强制校验：解析出的渲染器不在列表中时，返回 `template_disabled` 占位页而不是配置。前端的“客户端链接”弹窗同样按该列表过滤，只展示实际可用的链接；隐藏配置的组只展示*自动识别*与 *Mihomo SNKP*。Sub-Store 转换目标由 sidecar 自行转换，不受该列表限制。

## 订阅链接快照

在组内创建链接时，会把组的流量限制、设备上限、有效期和遥测周期复制到链接中。之后修改组策略（限制、有效期、遥测）**不会**改写已有链接；但组的模板、`hide_config`、设备模式和启用状态会立即生效。

每条链接可以覆盖：

- 名称与令牌（默认随机，均可修改），
- 流量限制、设备上限，
- 有效期，
- 三个遥测周期。

留空（`0` 或“跟随组”）的覆盖项以快照为准。

自动生成的令牌为 32 位小写十六进制字符（16 字节随机数），链接表单会在令牌输入框下方显示该说明。自定义令牌最长 64 个字符。

未分组的旧链接保持 `group_id = NULL`，行为与以前完全一致：不绑定设备、不会因策略原因返回占位页、auto 模式也不改变 SNKP 判定。

---

## 客户端识别

组内链接按以下顺序解析渲染器：

1. `hide_config` → 一律 `mihomo_snkp`。
2. 显式 `?client=` 参数：`mihomo_snkp`、`clash`/`mihomo`/`clashmeta`（→ `mihomo`；SNKP 请求且组允许时 → `mihomo_snkp`）、`surge`、`v2ray`/`uri`，或任一已启用的 Sub-Store 目标。
3. 通过 `User-Agent`（包含 `snkp`）或 `X-Sublink-Client: snkp` 标记识别 SNKP → `mihomo_snkp`。
4. `User-Agent` 识别：`surge` → Surge，`clash`/`mihomo` → mihomo，`v2ray`/`sing-box`/`shadowrocket` → v2ray。
5. `auto` 模式下其他情况（`User-Agent` 为空或无法识别）→ **`mihomo_snkp`**。

未分组的旧链接保留 auto 模式的原有行为。

渲染前还会用组的模板列表校验解析结果。`auto` 只是识别模式而非渲染器：具体结果（`mihomo`、`mihomo_snkp`、`surge`、`v2ray`）必须在列表中。Sub-Store 目标由 sidecar 自行转换，不参与该校验。

## 节点唯一键

每个渲染出的节点都会在名称旁带上稳定的 `sublinkpro_unique_key` 字段，取值来自节点内容哈希（取不到时回退到链接哈希）。它用于应对**名称处理**把所有节点重命名为同一个值的情况（例如 `名称处理 → 1`）：显示名称不再是可用标识，而注入的唯一键仍然逐节点唯一。

| 渲染器 | 键的位置 |
|:---|:---|
| Clash / Mihomo | YAML 条目中的 `sublinkpro_unique_key` 代理字段 |
| `vless://`、`trojan://`、`ss://`、`hysteria2://` 等 | `sublinkpro_unique_key` 查询参数，在重命名之后追加 |
| `vmess://` | base64 JSON 载荷中的额外字段 |
| `ssr://`、Surge 与未注册的协议 | 没有扩展位置 —— 按原样输出，不带该键 |

主流客户端会忽略它：Clash/Mihomo 会跳过未知代理字段，Xray 会忽略未知 JSON 字段，URI 解析器会忽略未知查询参数。该字段不参与内容哈希，因此对已带该键的链接再次计算哈希仍得到相同的节点哈希，不会反过来影响它本身的取值。

## 设备绑定

设备身份由两部分组成：

| 请求头 | 含义 |
|:---|:---|
| `X-HWID` | 归属（账号）标识。一个 HWID 可以覆盖多台安装 |
| `X-Device-ID` | 单台机器上的一次安装 |
| `X-Device-Name` | 可选，管理端显示的名称 |
| `X-Device-Platform` | 可选，平台标签（iOS、Android、Windows 等） |
| `X-App-Version` | 可选，客户端版本 |
| `X-Sublink-Client` | 可选，显式客户端标记（`snkp`） |

状态：

- `active` — 正常下发配置，占用一个名额。
- `pending` — 返回 `device_pending` 占位页，不占名额。
- `blocked` — 返回 `device_blocked` 占位页。

每个活跃设备还会获得专属密钥。响应会带上 `X-Sublink-Device-Status`，活跃设备还会带 `X-Sublink-Device-Secret` 与 `X-Sublink-Device-Name`（URL 编码）。`HEAD` 请求永远不会绑定设备。

---

## 占位页（Заглушка）

占位页是在请求无法正常下发配置时返回的可编辑内容。每个占位页包含名称、场景键（case key）、标题、正文和启用开关。

| 场景 | 触发条件 |
|:---|:---|
| `device_limit` | 已达到活跃设备上限 |
| `device_pending` | 设备等待批准（`approve` 模式） |
| `device_blocked` | 设备被管理员封禁 |
| `quota_exhausted` | 上报流量达到链接限制 |
| `expired` | 链接已过期 |
| `group_disabled` | 链接所属组已停用 |
| `template_disabled` | 解析出的渲染器不在组的模板列表中 |
| `unsupported_app` | 该应用无法获得配置（`hide_config`，或不允许旧版客户端） |
| `invalid_token` | 令牌不存在 |

场景解析顺序：**链接覆盖 → 组映射 → 共享库中的默认占位页**。场景键没有对应条目时，使用内置默认文案。

占位页正文支持以下单花括号变量：

| 变量 | 取值 |
|:---|:---|
| `{case}` | 场景键 |
| `{share_name}` | 链接名称 |
| `{group_name}` | 组名称 |
| `{device_name}` | 设备名称（已知时） |
| `{limit}` | 设备上限 |
| `{used}` | 已上报流量（格式化） |
| `{expires_at}` | 过期时间标签 |

占位页会按客户端请求的格式渲染（mihomo YAML、Surge、v2ray，或所请求的 Sub-Store 目标），并保留原始配置的外壳与文件名，因此客户端仍认得出这条链接，只是代理列表变成了占位文案。

## 响应头

| 响应头 | 含义 |
|:---|:---|
| `X-Sublink-Server` | 固定为 `sublinkpro` |
| `X-Sublink-Proto` | 协议版本（`1`） |
| `X-Sublink-Template` | 实际使用的渲染器 |
| `X-Sublink-Features` | 逗号列表：`status`、`stubs`，托管链接额外带 `devices`，开启遥测时带 `telemetry` |
| `X-Sublink-Device-Status` | `active`、`pending`、`blocked`、`unbound`、`limit`、`new`、`error` |
| `X-Sublink-Device-Secret` | 设备专属密钥。请求能解析到已绑定设备时，占位页响应也会带上，避免 SNKP 用在用占位页覆盖正常配置 |
| `X-Sublink-Device-Name` | URL 编码的设备名称，仅活跃设备 |
| `X-Sublink-Reason` | 触发的占位页场景（如有） |
| `X-Sublink-Stub` | 实际渲染的占位页场景 |
| `X-Sublink-Report-Interval-Sessions/Nodes/Reliability` | 遥测周期（秒），仅在开启遥测时返回 |
| `subscription-userinfo` | 带配额或有效期的链接为 `upload=<reported>; download=0; total=<limit>; expire=<effective>`；读取 `upload + download` 的客户端（如 SNKP）因此能看到完整已用流量。既无限制也无有效期的链接继续使用旧的机场汇总，占位页响应同样保留分享配额 |

## 状态接口

`GET /c/status?token=<shareToken>` 返回 JSON 快照，且**永远不会绑定设备**：

- `valid`、`reason`、`template`，
- `share`（id、name、enabled、`expire_type`/`expire_days`/`expire_at`、`expire_at_unix`、`created_at`、`traffic_limit_bytes`、`reported_traffic_bytes`、`remaining_bytes`、`quota_exhausted`、`max_devices`），
- `subscription`（id、name），
- `group`（id、name、enabled、hide_config、device_mode、templates、telemetry_enabled），
- `device`（状态；活跃时附带详情与 `secret`），
- `devices`（total/active/pending/blocked 计数），仅托管链接，
- `telemetry`（是否启用、三个周期，以及首选周期 `report_interval_seconds`，等于 `sessions`），
- `notices` —— `valid` 为 false 时解析出的占位页标题与正文。

示例（节选）：

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

`valid`、`reason`、`template` 位于信封的**顶层** —— 不存在 `share.valid`。响应同时带上与配置请求相同的 `X-Sublink-*` 头，客户端可以先据此判断是否需要拉取配置。

## 遥测上报

`POST /c/telemetry?token=<shareToken>` 用于接收流量上报。链接必须属于已开启**遥测**的订阅组，否则返回 `403`。

支持两种上报模式：

- **增量模式**（默认）：客户端上报距上次上报的字节数。
- **绝对模式**：客户端上报安装级累计计数器（`reported_total_bytes`）与安装代号（`install_id`）。服务端根据设备上保存的基线计算差值，即使响应丢失或请求重试也不会重复计数。

增量上报：

```json
{"up": 12345, "down": 678900, "report_id": "r-1"}
```

绝对上报：

```json
{"reported_total_bytes": 987654, "install_id": "install-abc", "up": 1234, "down": 0}
```

| 字段 | 含义 |
|:---|:---|
| `up` | 距上次上报的上行字节数。绝对模式下仅用于把接受的差值拆分到小时桶的上行部分 |
| `down` | 距上次上报的下行字节数，绝对模式下的拆分规则相同 |
| `report_id` | 可选。仅增量模式：30 分钟内相同 id 的重试会被视为重复，不写入任何数据 |
| `reported_total_bytes` | 安装级绝对计数器。需要能解析出设备；没有设备身份时回退为增量语义 |
| `install_id` | 安装代号（trim，最长 64 字符）。与已保存代号不同时基线从 0 重新开始，重装或迁移数据不会被重复计数 |

`up`、`down`、`reported_total_bytes` 至少提供一个。负数、非数字、单方向超过 1 PiB、绝对总量超过 1 EiB 的数值会被拒绝（`400`），不会写入计数器。

可选请求头：

| 请求头 | 含义 |
|:---|:---|
| `X-Sublink-Device-Secret` | 最严格的校验：密钥必须属于绑定在该链接上的设备 |
| `X-HWID` + `X-Device-ID` | 两者同时存在且已绑定时，上报归属于该设备 |

不带任何身份信息的上报同样有效，按链接累计。`blocked` 或 `pending` 设备会被拒绝（`403`）。

绝对模式规则（已解析出设备）：

- `delta = reported_total_bytes - baseline`；当 `install_id` 与已保存代号不同时，基线先重置为 `0`；
- `delta <= 0` → 视为重复：`accepted_bytes` 为 `0`，`duplicate` 为 `true`，基线保持不变 —— 迟到的较小总量不会被误判为重装；
- `delta > 0` → 一条条件更新 `UPDATE ... WHERE id = ? AND report_baseline_bytes = ? AND report_baseline_epoch = ?` 写入新基线、代号和 `reported_traffic_bytes + delta`；并发竞争失败时重读并重试，最多 3 次。

成功的上报会原子地把增量累加到链接的 `reported_traffic_bytes`，更新 `last_usage_at`，刷新缓存，并在访问日志中记为 `telemetry` 动作。重复上报正常返回，但不写访问日志。

响应数据：

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

| 字段 | 含义 |
|:---|:---|
| `reported_traffic_bytes` | 链接累计计数器 |
| `traffic_limit_bytes` | 链接配额；`0` 表示不限 |
| `remaining_bytes` | `limit - reported`，下限为 `0`；不限流量的链接为 `0` |
| `quota_exhausted` | 链接是否达到限额 |
| `device_status` | 解析出的设备状态（`active`、`unbound` 等） |
| `device_reported_traffic_bytes` | 设备累计计数器（不是链接计数器） |
| `accepted_bytes` | 本次上报写入的字节数；重复上报为 `0` |
| `duplicate` | 上报因重复而被忽略时为 `true` |

链接达到流量限制后，配置请求会返回 `quota_exhausted` 占位页，但上报接口继续接收数据，以便最终用量保持准确。

### 设备归属与小时用量

上报带有设备身份（`X-Sublink-Device-Secret`，或已绑定的 `X-HWID` + `X-Device-ID` 组合）时，同一份字节数也会写入设备：

- `reported_traffic_bytes` —— 设备累计计数器，与链接遵循相同的“每次成功上报都累加”规则，
- `report_baseline_bytes` / `report_baseline_epoch` —— 最近接受的绝对总量及其安装代号，管理端设备接口同样会返回，
- `last_report_at` —— 最近一次成功上报的时间，
- 按 `(设备, 小时)` 聚合的小时桶 —— 保留 **7 天**，由后台自动清理。同一小时的上报会合并为一行。服务端保存 UTC 小时桶，前端按浏览者本地时区折叠一天 24 小时，因此“设备什么时候用网”跟随打开图表之人的时区。

无法归属到已绑定设备的上报仍按链接累计。

### 上报数据的展示位置

- **订阅组页面** — 组卡片展示汇总的**已上报流量**（组内所有链接 `reported_traffic_bytes` 之和，以 `已用 / 上限` 显示）、**最近上报**时间，以及任一链接超出自身限额时的**流量已用尽（N）**标记。
- **遥测标签页** — 每个安装（HWID + 设备 ID）一行，跨它使用的所有分享链接聚合：状态、使用的订阅、累计与近 7 天流量、最近进入网络与最近上报时间。可按关键词、节点池或状态筛选；点击行打开设备详情，包含流量统计卡片、该设备使用的订阅列表、24 小时用量图表与访问记录。
- **链接弹窗** — 受管链接上的**设备**按钮打开该链接的设备列表（平台、版本、状态、流量、最近上报），并提供批准、封禁、解封与移除操作。
- **链接卡片** — 每个受管链接卡片显示 `流量 已用 / 上限`、剩余流量（`limit - reported`，不限时为 `∞`）、活跃与待批准设备数量，以及最近上报时间；达到限额的链接会带上**流量已用尽**标签。打开链接详情时，客户端链接列表上方会重复展示同样的摘要。
- **访问记录弹窗** — 遥测上报以已翻译的**遥测上报**动作展示，占位页原因（如 `quota_exhausted`）也会显示为可读文案。

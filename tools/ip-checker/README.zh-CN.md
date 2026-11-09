# ip-checker

容器化检查工具：找出哪些 [SublinkPro](../../README.zh-CN.md) 节点的出口地址在公开信誉数据库中仍未被视为 VPN、代理、Tor
或数据中心，而是普通 ISP／住宅线路，并把 `clean-ip` 标签与之同步。

会屏蔽 VPN 流量的服务（银行、流媒体、部分 AI 接口）屏蔽的并不是某个国家，而是出现在 VPN／机房名单里的地址。因此一个节点可能通过了
按服务维度的检查，却依然不可用——它的地址早已被标记为数据中心。本工具直接向信誉数据库提出这个问题。

对每个节点，工具都会通过独立的 mihomo 实例发起一次「我的 IP 是什么」请求，然后把该地址交给所有已启用的数据源。没有被任何数据源标记的
节点获得标签；被识别为 VPN／代理／Tor／机房的节点失去标签；无法给出结果的节点保持原状。

```
SublinkPro API ──► 节点列表 ──► mihomo（每个节点一个代理）
                                      │
                     api.ipify.org ───┴──► 出口地址（经由节点读取）
                                      │
              ┌───────────────────────┼───────────────────────┐
              ▼                       ▼                       ▼
        iplocate.io              ip-api.com             proxycheck / IPQS / vpnapi
     privacy.is_vpn / is_hosting   hosting / proxy        proxy=yes · security.vpn
              └───────────────────────┼───────────────────────┘
                                      ▼
                        clean / flagged / unknown
                                      ▼
                 SublinkPro 标签 clean-ip（batch-add / batch-remove）
```

## 数据源

| `SOURCES` 键 | 是否需要密钥 | 返回内容 | 默认 |
|---|---|---|---|
| `iplocate` | 否 | `privacy.is_vpn`、`is_proxy`、`is_tor`、`is_hosting`、`is_abuser`、`is_icloud_relay`、`is_bogon` | ✅ |
| `ipapi`（别名 `ip-api`） | 否 | `hosting`、`proxy` 标记（免费版仅支持明文 HTTP） | ✅ |
| `proxycheck` | 可选 | `proxy: yes/no`、地址 `type`（如 `VPN`）、`risk` | – |
| `ipqualityscore`（别名 `ipqs`） | 是 | `vpn`、`proxy`、`tor`、`fraud_score ≥ 90` | – |
| `vpnapi` | 是 | `security.vpn`、`proxy`、`tor`、`relay` | – |

默认的两个数据源无需任何账号。`SOURCES` 接受逗号分隔的列表；启用需要密钥的数据源却不提供密钥会被视为配置错误，而不是让每个节点悄悄变成
`unknown`：

```bash
SOURCES=iplocate,ipapi,proxycheck,ipqualityscore
PROXYCHECK_KEY=...
IPQUALITYSCORE_KEY=...
```

## 节点如何判定

| 结论 | 含义 | 标签 |
|---|---|---|
| `clean` | 没有任何已启用数据源标记该地址，且至少 `REQUIRE_CLEAN` 个数据源确实给出了答复 | 添加 `clean-ip` |
| `flagged` | 至少一个数据源识别出 VPN／代理／Tor／机房，日志会写明具体标记（`ipapi: hosting`） | 移除 `clean-ip` |
| `unknown` | 没有可用答复：配额耗尽、HTTP 错误、报文结构变化、读不到出口地址、节点不可达 | 保持原状 |

单个 `flagged` 会压过任意数量的 clean 答复：只要有一个数据库认出这是 VPN，就不该给出"干净"的结论。`unknown` 从不改动标签，所以凌晨三点
耗尽配额不会把昨天的结果抹掉。

含多条 URI 的节点会逐条链路检查：只有当**每条**链路都干净时节点才算 `clean`，任意一条被标记即为 `flagged`，有链路无法检查则为
`unknown`——标签描述的是节点整体，而不是其中的某一条链路。

`REQUIRE_CLEAN`（默认 `1`）可以提高门槛：把它设为已启用数据源的数量，即要求所有数据源一致同意。

### 如何读取出口地址

地址是**经由节点**读取的，因为经过 NAT、额外跳数或轮询出口后，只有对端才能看到真实地址。工具依次尝试三个接口
（`EXIT_IP_URL`，随后是 `https://icanhazip.com` 与 `https://ifconfig.me/ip`），并且只接受可路由的单播地址——私有地址、回环地址或
bogon 地址会被拒收，而不是拿去查询。

随后每一次信誉查询同样经由该节点发出，并显式指定已发现的地址，因此轮询出口不会在同一次判定里混入两个地址。

## 快速开始

```bash
cd tools/ip-checker
cp .env.example .env
# 编辑 .env：填写 SUBLINK_URL 与 SUBLINK_API_KEY
docker compose up -d --build
docker compose logs -f
```

API 密钥在 SublinkPro 的 **设置 → 访问密钥** 中创建，通过 `X-API-Key` 请求头发送，需要节点读取与标签写入权限。

### 连接到 SublinkPro

* **SublinkPro 发布在宿主机上**（默认）：保持
  `SUBLINK_URL=http://host.docker.internal:8000`。compose 文件已经为 Linux 添加了
  `host.docker.internal:host-gateway` 映射。
* **共享 Docker 网络**：取消 `docker-compose.yaml` 中网络配置的注释，并把
  `SUBLINK_URL` 改为 `http://sublinkpro:8000`。

## 配置

所有开关都是环境变量（见 `.env.example`）。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SUBLINK_URL` | `http://host.docker.internal:8000` | SublinkPro 基础地址 |
| `SUBLINK_API_KEY` | –（必填） | 访问密钥，通过 `X-API-Key` 发送 |
| `SOURCES` | `iplocate,ipapi` | 逗号分隔的 IP 信誉数据源 |
| `IPLOCATE_API_KEY` | – | 可选，可提高 iplocate 配额 |
| `PROXYCHECK_KEY` | – | 可选，`proxycheck` 无密钥时每天可用 100 次 |
| `IPQUALITYSCORE_KEY` | – | `ipqualityscore` 必需 |
| `VPNAPI_KEY` | – | `vpnapi` 必需 |
| `CLEAN_TAG` | `clean-ip` | 干净节点上写入的标签 |
| `REQUIRE_CLEAN` | `1` | 写入标签前需要多少个数据源给出 clean |
| `CHECK_INTERVAL` | `24h` | `30m`、`24h`、`1d`、`1h30m` 或纯秒数 |
| `RUN_ON_START` | `true` | 启动后立即执行一轮 |
| `ONCE` | `false` | 只运行一轮然后退出（手动执行） |
| `ONLY_TAGS` | – | 只检查带有其中任一标签的节点（逗号分隔，忽略大小写） |
| `SKIP_TAGS` | – | 永不检查带有其中任一标签的节点 |
| `MAX_NODES` | `0` | 检查到该数量后停止；`0` 表示不限制 |
| `WORKERS` | `1` | 并行检查的节点数（每个 worker 拥有独立 mihomo） |
| `ENSURE_TAG` | `true` | `CLEAN_TAG` 不存在时自动创建 |
| `DRY_RUN` | `false` | 只记录决策，不修改标签 |
| `PROBE_TIMEOUT` | `15` | 单次请求超时，秒 |
| `EXIT_IP_URL` | `https://api.ipify.org?format=json` | 读取出口地址的首选接口，之后会自动尝试两个公共备用接口 |
| `REACHABILITY_URL` | `https://www.gstatic.com/generate_204` | 用于在任何查询之前剔除不可用节点 |
| `LOG_LEVEL` | `info` | `debug`、`info`、`warning`、`error` |

## 调度与长周期

信誉变化远慢于服务可达性，而无密钥数据源有每日配额，因此默认每天一轮。约 100 个节点的一轮只需几分钟。调度器会：

* **同一时间只跑一轮**——查询从不重叠；
* 当一轮能装进周期内时，保持以开始时间为锚点的节奏；
* 当一轮超过周期时，记录跳过了多少次计划运行，并把下一次锚定在本轮结束之后；
* 在 `/data/scheduler.lock` 上加独占锁，共用该卷的两个容器不会同时运行。

### 单次运行与子集

`ONCE=true` 只运行一轮然后退出，适合手动检查：

```bash
docker run --rm --init --env-file .env \
  -e ONCE=true -e DRY_RUN=true -e MAX_NODES=10 ip-checker:latest
```

`ONLY_TAGS` / `SKIP_TAGS` / `MAX_NODES` 可以裁剪待检列表，例如只复检已带 `clean-ip` 的节点，或在全量运行前先试跑十个节点。标签匹配忽略
大小写，`SKIP_TAGS` 优先于 `ONLY_TAGS`，过滤在查询之前完成，因此被跳过的节点不产生任何开销。

### 并行 worker

`WORKERS=N` 同时检查 N 个节点。每个 worker 启动自己的 mihomo（代理端口
`MIHOMO_PROXY_PORT + i`，API 端口同样 `+ i`），因此消耗的是内存而不是接口配额：每个 worker 预留约 100 MB。启用多个 worker 时，每行日志都会
带上 `W1`/`W2`/… 前缀，便于分辨交错的输出。

## 输出

```
2026-11-09 12:00:02 INFO    main     === check cycle started ===
2026-11-09 12:00:03 INFO    sublink  created tag 'clean-ip'
2026-11-09 12:00:05 INFO    main     [  1/ 97] #11    Netherlands · Amsterdam            clean   45.86.12.7       2.4s  -> +clean-ip
2026-11-09 12:00:07 INFO    main     [  2/ 97] #19    Germany · Falkenstein              flagged 5.9.0.1          1.9s  5.9.0.1: iplocate: hosting; ipapi: hosting -> -clean-ip
2026-11-09 12:00:09 INFO    main     [  3/ 97] #20    Singapore                          unknown -                9.0s  api.ipify.org: timed out; icanhazip.com: timed out
2026-11-09 12:05:41 INFO    main     cycle done in 5m38s: checked 97/97 with 3 worker(s), 3.5s/node | tags added=41 removed=51 errors=0
2026-11-09 12:05:41 INFO    main       clean-ip     clean=41 flagged=51 unknown=5
```

最近一轮的汇总同时写入 `/data/state.json`：`nodes`、`checked`、`workers`、`seconds_per_node`、`interrupted`，以及按状态与标签的
计数器。被 SIGTERM 停止的运行会保持 `checked < nodes`，因此不完整的一轮不会被误认为完整。

`LOG_LEVEL=debug` 会为每条链路额外输出一行，列出各数据源的结论与原因，判定可疑时正是该用它。

## 标签如何写入

| 结论 | 添加 | 移除 |
|---|---|---|
| `clean` | `CLEAN_TAG`（`clean-ip`） | – |
| `flagged` | – | `CLEAN_TAG`（`clean-ip`） |
| `unknown` | – | – |

* 添加使用 `POST /api/v1/tags/node/batch-add`，移除使用
  `POST /api/v1/tags/node/batch-remove` 并只带一个 `tagNames` 条目——节点上的其他标签全部保留；
* 只写入差异：已经处于目标状态的节点不产生任何 API 调用；
* `DRY_RUN=true` 时同样的决策会记录为 `would add …, remove …`。

只写一个标签正是为了可组合：`clean-ip` 可以与任何其他检查器写入的标签（`ai-*`、`youtube`、机场标签）共存而不冲突。

## 说明与限制

* **`clean` 的含义是"未被已启用的数据源标记"，而不是"保证是住宅 IP"。** 一个数据库都还不认识的全新地址，在两个无密钥数据源那里都会
  返回 clean。若需要更严格的筛选，请启用更多数据源并把 `REQUIRE_CLEAN` 设为它们的数量。
* 公共 CDN／anycast 地址会被标记，这是有意为之：`1.1.1.1` 会得到
  `iplocate: abuser` 与 `ipapi: hosting`——共享地址不是住宅线路。
* 配额是按出口地址计算的，因为每次查询都经由节点发出：`ip-api.com` 每个地址每分钟 45 次，`proxycheck.io` 无密钥时每天 100 次。每轮每节点
  一次查询远低于这两个上限。
* `ip-api.com` 免费版只通过明文 HTTP 应答。恶意出口有可能改写该应答；其余数据源使用 HTTPS，且其中任意一个 `flagged` 依然会生效。
* 地址按首个可用接口看到的结果判定（实际为 IPv4）。仅通过 IPv6 出口的节点由
  `icanhazip.com`/`ifconfig.me` 的返回结果判定。
* 信誉是时点数据。地址可能因滥用被重新归类，被回收的地址也可能因前一位使用者的行为而被标记。若机场经常更换地址，请把
  `CHECK_INTERVAL` 保持在一天以内。
* 无法连接或无法从任何接口取得答复的节点会保留原有标签（`unknown`）。这是刻意设计：一次故障不应改写结果。
* 本工具不替代按服务维度的检查器。`clean-ip` 说明该地址不是已知的 VPN 出口；AI 检查器（`tools/ai-checker`）说明某个具体 AI 服务是否
  接受它。

## 测试

单元测试在镜像内运行，宿主机无需安装 Python：

```bash
docker build -t ip-checker .
docker run --rm --entrypoint python ip-checker \
    -m unittest discover -s tests -t .
```

覆盖内容包括：每个数据源的解析（clean、flagged、配额、HTTP 错误、报文变化、缺少密钥）、别名与密钥解析、聚合规则（`flagged` 优先、
`REQUIRE_CLEAN`）、出口地址解析与备用链路（私有地址被拒收）、节点选择（`ONLY_TAGS`、`SKIP_TAGS`、`MAX_NODES`）、按链路合并
（全部干净、一条被标记即失败、存在未检查链路则保持 `unknown`）、worker 端口与目录布局、`DRY_RUN` 以及标签差异。

网络链路也做了端到端验证：真实 mihomo 后面的本地 SOCKS5 出口、桩化的 SublinkPro API，以及两个方向——住宅地址得到 `+clean-ip`，
机房地址得到 `-clean-ip`，原因为 `iplocate: hosting; ipapi: hosting`。

## 发布镜像

`.github/workflows/ip-checker-image.yml` 会在 `tools/ip-checker/` 有改动时以及手动触发时构建并发布镜像。它会在刚构建好的镜像内运行
单元测试，通过后才推送：

```bash
docker pull ghcr.io/<owner>/ip-checker:latest
docker run --rm -e SUBLINK_URL=... -e SUBLINK_API_KEY=... ghcr.io/<owner>/ip-checker:latest
```

每次构建都会打上 `latest`、分支名和 `sha-<short>` 标签。该任务只在 fork 中运行：上游从源码分发工具，并保有自己的发布流水线。

## 故障排查

| 现象 | 可能原因 |
|---|---|
| `SUBLINK_API_KEY is required` | `.env` 缺失或密钥为空 |
| `ipqualityscore needs IPQUALITYSCORE_KEY` | `SOURCES` 中包含了需要密钥的数据源 |
| 所有调用返回 HTTP 403 | 访问密钥错误或已被撤销 |
| 所有节点 `unknown` 且提示 `link unreachable` | 节点本身不可用，或容器内出口被过滤 |
| 所有节点 `unknown` 且提示 `api.ipify.org: …` | 出口屏蔽了"我的 IP"接口；把 `EXIT_IP_URL` 换成可达的接口 |
| 所有节点 `unknown` 且提示 `rate_limited` | 数据源配额耗尽；等待重置、添加密钥或去掉该数据源 |
| 所有节点 `flagged` | 节点确实是机房出口——这是结论，不是缺陷 |
| `mihomo exited during start-up` | 节点 URI 非法；查看日志中的 mihomo 输出 |
| `configuration error: unknown source` | `SOURCES` 拼写错误；错误信息会列出可用键名 |

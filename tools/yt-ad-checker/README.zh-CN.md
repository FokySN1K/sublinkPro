# yt-ad-checker

容器化的检测工具：找出 [SublinkPro](../../README.zh-CN.md) 中哪些节点可以
**无广告访问 YouTube 或 Rutube**，并据此同步节点标签。

每次检测都会通过独立的 mihomo 实例把检测请求路由到单个节点。YouTube 使用真实
Chromium 会话读取播放器信号；Rutube 只需一次 JSON 请求，读取 Rutube 会向该出口
IP 投放的广告排期。无广告的节点打上 clean 标签并移除 ads 标签；有广告的节点反之，
因此节点最终只会保留其中一个标签。不可用或返回错误的节点保持原状。

```
SublinkPro API ──► 节点列表 ──► mihomo（每个节点一个代理）
                                      │
                    ┌─────────────────┴──────────────────┐
                    ▼                                    ▼
        Chromium ──► youtube.com/watch    Rutube /api/play/options/<video>
                    │                                    │
                    └───────── clean / ads / unreachable / unknown
                                      │
                                      ▼
                       SublinkPro 标签（batch-add / batch-remove）
```

## 服务

| `SERVICE` | 检测方式 | 默认标签 |
|---|---|---|
| `youtube`（默认） | 有头 Chromium，四组播放器信号 | `youtube` / `youtube-ads` |
| `rutube` | 每个视频一次 JSON 请求，无需浏览器 | `rutube` / `rutube-ads` |

一个容器只检测一种服务：`SERVICE` 在启动时解析一次，决定检测方式、标签对与状态文件。
需要同时检测两者时，用两个实例并各自挂载独立的 `/data` —— `docker-compose.local.yaml`
中的 `yt-ad-checker` 与 `yt-ad-checker-rutube` 即为此配置；两者写入不同标签，互不冲突。

## 判定依据（YouTube）

单次检测会采集四组相互独立的信号：

| 信号 | 来源 |
|---|---|
| `adPlacements` / `playerAds` | `ytInitialPlayerResponse` 与实时的 `getPlayerResponse()` |
| `.ad-showing`、广告浮层节点 | `#movie_player` 元素 |
| 广告素材请求 | `/pagead/adview`、`/pagead/interaction`、`*.2mdn.net`、`googlevideo…adformat=` |
| `playabilityStatus` | 播放器响应（避免把异常页面误判为无广告） |

* **`ads`** —— 出现任一广告信号。
* **`clean`** —— `playabilityStatus = OK`、播放器元素存在，且 `PROBE_DWELL`
  秒内没有任何广告信号。
* **`unreachable`** —— 页面未能在该节点上打开。连接被拒绝、重置或超时的节点会被
  直接跳过，不再消耗检测预算。
* **`unknown`** —— 页面打开了，但播放器不可用（同意页、机器人校验、
  `ERROR`/`UNPLAYABLE`/`LOGIN_REQUIRED`）。

只有 `ads` 与 `clean` 会改动标签；`unreachable` 与 `unknown` 不会，因此临时
故障不会误删标签。

> `googleads.g.doubleclick.net/pagead/id`、`static.doubleclick.net/instream/ad_status.js`、
> `/api/stats/ads?...&req=9` 这类检测信标会被忽略：无广告的播放同样会触发它们。

每次检测先打开 YouTube 首页，再打开待测视频：全新的无 Cookie 会话拿不到广告
请求，媒体流也无法播放，会让所有节点都看起来“无广告”。随后会显式启动播放，
因为前贴片广告是在开始播放时请求的。

### 多个检测视频

单个视频可能对某个国家（甚至所有人）停止投放广告，从而把所有节点都判成“无广告”。
`YOUTUBE_VIDEOS` 因此提供多个视频，并以不对称的方式使用：

* **第一个**视频走完整检测（播放 + `PROBE_DWELL`），见上一节；
* 当结果为 `clean` 或 `unknown` 时，其余视频只打开页面、不播放，仅读取广告排期
  （`adPlacements` / `playerAds`），通常每个 1–2 秒；
* **任一**视频出现广告，节点即为 `ads`，日志会标出是哪个视频
  （`9bZkp7q19f0 adPlacements=2`）；
* 找到广告的视频会被记住，并在本轮后续节点中排到最前
  （`probe video 9bZkp7q19f0 shows ads, it now goes first`），完整检测因此总是跑在
  仍有广告的视频上；重启后恢复配置顺序；
* 只有完整检测无广告、且其它视频也没有排期，节点才判定为 `clean`。

额外的视频只会把节点提升为 `ads`，绝不会把节点降为 `clean`，因此默认列表不会制造
假的无广告判定。若希望整轮检测尽可能短，可以减少条目或只设置一个 `PROBE_URL`：
每个额外视频都会占用该节点的 `PROBE_TIMEOUT` 预算。

## 判定依据（Rutube）

Rutube 在服务端生成广告排期，并随视频的播放参数一起返回：

```
GET https://rutube.ru/api/play/options/<video_id>/?no_404=true
→ { "advert": [{"name": "preroll201", "count": 1, ...}], "cuepoints": [], ... }
```

* **`ads`** —— 任一 `advert` 条目的 `count > 0`（前贴片/中插槽位），或
  `cuepoints` 非空；
* **`clean`** —— 至少两个测试视频有响应，且都没有排期；
* **`unreachable`** —— 所有视频都没有响应（节点不可用、Rutube 被屏蔽、TLS 重置）；
* **`unknown`** —— 有响应的视频少于两个（例如其中一个被地区屏蔽）。

单个视频可能对所有人都不投放广告，因此会用 `RUTUBE_VIDEOS` 中的多个视频依次
检测后才判定为 clean。Rutube 的广告库存主要在俄罗斯，境外出口返回空排期是正常
的“无广告”结果，不是缺陷。

## 快速开始

```bash
cd tools/yt-ad-checker
cp .env.example .env
# 编辑 .env：填写 SUBLINK_URL 与 SUBLINK_API_KEY
docker compose up -d --build
docker compose logs -f
```

API Key 在 SublinkPro 的 **设置 → 访问密钥** 中创建，通过 `X-API-Key` 请求头发送。

### 访问 SublinkPro

* **SublinkPro 端口已映射到宿主机**（默认）：保持
  `SUBLINK_URL=http://host.docker.internal:8000`，compose 文件已为 Linux 添加
  `host.docker.internal:host-gateway` 映射。
* **共用 Docker 网络**：取消 `docker-compose.yaml` 中的网络注释，并把
  `SUBLINK_URL` 设为 `http://sublinkpro:8000`。

## 配置项

全部通过环境变量提供（见 `.env.example`）。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SUBLINK_URL` | `http://host.docker.internal:8000` | SublinkPro 基础地址 |
| `SUBLINK_API_KEY` | –（必填） | 访问密钥，作为 `X-API-Key` 发送 |
| `SERVICE` | `youtube` | `youtube` 或 `rutube` |
| `YOUTUBE_TAG` | `youtube` | YouTube：无广告节点的标签 |
| `YOUTUBE_ADS_TAG` | `youtube-ads` | YouTube：有广告节点的标签（`ADS_TAG` 仍作为别名可用） |
| `RUTUBE_TAG` | `rutube` | Rutube：无广告节点的标签 |
| `RUTUBE_ADS_TAG` | `rutube-ads` | Rutube：有广告节点的标签 |
| `RUTUBE_VIDEOS` | 内置列表 | 逗号分隔的 Rutube 视频 id，用作检测样本（必须有广告） |
| `ENSURE_TAG` | `true` | 标签不存在时自动创建这两个标签 |
| `DRY_RUN` | `false` | 只记录判定结果，不修改标签 |
| `CHECK_INTERVAL` | `6h` | `30m`、`6h`、`1d`、`1h30m` 或纯秒数 |
| `RUN_ON_START` | `true` | 启动后立即执行一轮 |
| `ONCE` | `false` | 只执行一轮后退出（手动运行，忽略 `RUN_ON_START`） |
| `ONLY_TAGS` | – | 仅检测带有其中任一标签的节点（逗号分隔，不区分大小写） |
| `SKIP_TAGS` | – | 不检测带有其中任一标签的节点 |
| `MAX_NODES` | `0` | 只检测前 N 个节点；`0` 表示不限制 |
| `WORKERS` | `1` | 并行检测的节点数（每个 worker 各自启动 mihomo 与浏览器） |
| `YOUTUBE_VIDEOS` | 内置列表 | 逗号分隔的检测视频（id 或链接）；`PROBE_URL` 仍可作为单个视频的别名 |
| `PROBE_TIMEOUT` | `45` | 单个节点的总耗时上限（秒） |
| `PROBE_DWELL` | `10` | 观察播放器广告信号的时长（秒） |
| `HEADLESS` | `false` | 关闭虚拟显示（不推荐） |
| `LOG_LEVEL` | `info` | `debug`、`info`、`warning`、`error` |

## 调度与超长轮次

约 100 个节点的一轮检测通常需要 15–40 分钟，`CHECK_INTERVAL` 往往比一轮更短。
调度器因此：

* **同一时刻只跑一轮**，检测不会重叠；
* 轮次能装进周期时，按下发时间保持节拍；
* 轮次超过周期时，记录被跳过的次数，并把下一轮锚定在当前轮结束之后
  （即再等一个 `CHECK_INTERVAL`）；
* 对 `/data/scheduler.lock` 加独占锁，共用卷的两个容器不可能同时运行。

### 单次运行与节点筛选

`ONCE=true` 只执行一轮后退出，适合手动检测而不影响长期运行的容器：

```bash
docker run --rm --init --shm-size 1g --env-file .env \
  -e ONCE=true -e SERVICE=youtube yt-ad-checker:latest
```

`ONLY_TAGS` / `SKIP_TAGS` / `MAX_NODES` 用于缩小检测范围，例如只复检已有 clean
标签的节点，或在完整运行前先抽检十个节点：

```bash
docker run --rm --init --shm-size 1g --env-file .env \
  -e ONCE=true -e ONLY_TAGS=youtube -e MAX_NODES=10 yt-ad-checker:latest
```

标签匹配不区分大小写，`SKIP_TAGS` 优先于 `ONLY_TAGS`；筛选发生在任何检测之前，
被排除的节点不产生任何开销。

### 并行 worker

`WORKERS=N` 同时检测 N 个节点。每个 worker 各自启动一个 mihomo（代理端口
`MIHOMO_PROXY_PORT + i`，API 端口同样 `+ i`）和一个 Chromium，因此消耗主要是内存：
在基础镜像之上按每个 worker 约 1 GB 估算，并相应调大 `shm_size`。约 100 个 YouTube
节点的整轮检测在 `WORKERS=3` 时由约 25 分钟降到约 8 分钟；结果与标签完全一致，
因为每个 worker 仍然一次只负责一个节点。

汇总会给出使用的 worker 数量、实际检测的节点数和平均每个节点的耗时；worker 多于
一个时，每行日志会加上 `W1`/`W2`/… 前缀，便于区分交错的输出。

### 主机规格

每个 YouTube worker 都会常驻一个 Chromium，因此资源消耗集中在 YouTube 检测；Rutube
检测只发 HTTP 请求。

| 配置 | 内存 | CPU |
|---|---|---|
| YouTube，`WORKERS=1` | 约 1 GB | 约 1 核 |
| 每增加一个 YouTube worker | +0.5–1 GB | +1 核 |
| Rutube，`WORKERS=3` | 约 0.3 GB | 可忽略 |

1 vCPU / 1 GB 的小型 VPS 可以同时运行 SublinkPro 与 Rutube 检测，但跑不了 YouTube
检测：OOM killer 会先杀掉 Chromium，随之而来的负载又会拖垮面板。此时应把 YouTube
容器放到另一台机器上，把 `SUBLINK_URL` 指向服务器映射出的 API 端口
（`http://<server>:8443`），服务器上只保留 Rutube 实例。

## 输出

```
2026-11-01 20:14:03 INFO    main     === check cycle started ===
2026-11-01 20:14:09 INFO    main     [  1/ 97] #11    Москва                 ads         GL=UA   4.3s adPlacements=2 -> +youtube-ads -youtube
2026-11-01 20:14:21 INFO    main     [  2/ 97] #19    Ростов-на-Дону         clean       GL=RU  11.8s player ok, no ad signals -> +youtube
2026-11-01 20:14:33 WARN    main     [  3/ 97] #20    Краснодар              unreachable         6.0s connection failed: Timeout
2026-11-01 20:41:12 INFO    main     cycle done in 8m3s: checked 97/97 with 3 worker(s), 5.0s/node: clean=41 ads=49 unreachable=5 unknown=2 | 'youtube'/'youtube-ads' added=3 removed=1 errors=0
```

最近一轮的汇总同时写入 `/data/state.json`。除各判定计数外，还包含计划检测的节点数
（`nodes`）、实际完成数（`checked`）、`workers`、平均速度 `seconds_per_node` 与
`interrupted`：被 SIGTERM 中断的轮次满足 `checked < nodes` 并输出告警，因此不完整
的轮次不会被误认为完整运行。

## 标签写入规则

| 判定 | 添加 | 移除 |
|---|---|---|
| `clean` | clean 标签（`youtube` / `rutube`） | ads 标签（`youtube-ads` / `rutube-ads`） |
| `ads` | ads 标签（`youtube-ads` / `rutube-ads`） | clean 标签（`youtube` / `rutube`） |
| `unreachable`、`unknown` | – | – |

* 添加走 `POST /api/v1/tags/node/batch-add`，移除走
  `POST /api/v1/tags/node/batch-remove` 且只带一个 `tagNames` 条目，节点上的
  其它标签保持不变；
* 只写入差异：已经处于目标状态的节点不会发起请求；
* `DRY_RUN=true` 时同样的判定记录为 `would add …, remove …`，但汇总中的计数仍
  反映真实运行会发生的改动。

一个节点包含多条 URI 链接时逐条检测：任一条有广告即判定为 `ads`，全部无广告
才算 `clean`。

## 说明与限制

* 检测读取 `GET /api/v1/nodes/get` 返回的原始链接，因此节点完全按照 URI 描述的
  方式建立连接；仅存在于 SublinkPro 导出层的能力（链式代理 `DialerProxyName`、
  Clash 专属字段）不会生效。
* 判定基于出口 IP 实际收到的响应：出口地区本身不投放前贴片广告的节点会被判定为
  `clean`，这正是筛选“无广告节点”时想要的结果。
* 判定带有时间点属性，YouTube 会做 A/B 测试，同一节点在不同轮次可能翻转。可先用
  `DRY_RUN=true` 跑两轮比对，找出不稳定的节点。
* 每次检测前先用 mihomo 拨号预检（`/proxies/<name>/delay`，6 秒）：无法建立连接的
  节点会在数秒内判定为 `unreachable`，不启动浏览器，也不占用 `PROBE_TIMEOUT`。
  检测中途连接中断（Chromium `net::ERR_CONNECTION_*`）同样处理：跳过播放器等待、
  `PROBE_DWELL` 和额外视频。
* Rutube 检测不需要浏览器：约 100 个节点的整轮检测几分钟即可完成，
  `HEADLESS`/`PROBE_DWELL` 对它无效。`PROBE_TIMEOUT` 仍是单个节点的总耗时上限，
  但无法连通 Rutube 的节点在第一次请求失败（约 10 秒）后即被放弃，不会耗尽整个
  时间预算。
* 若整轮检测都没有出现广告，汇总会提示检查 `YOUTUBE_VIDEOS`：检测列表对当前出口
  可能已不再投放广告，需要换成目标地区仍有前贴片的视频。
* Chromium 需要共享内存，compose 已设置 `shm_size: 1gb`。

## 测试

单元测试在镜像内运行，宿主机无需配置 Python：

```bash
docker build -t yt-ad-checker .
docker run --rm --entrypoint python yt-ad-checker -m unittest discover -s tests -t .
```

覆盖标签同步（`clean`/`ads` 互换标签对，`unreachable`/`unknown` 保持不变，
`DRY_RUN` 只记录）、视频选择（`YOUTUBE_VIDEOS`、`PROBE_URL`、id/链接归一化、
额外视频）、连接预检（不通的节点不会进入检测，标签保持不变）、节点筛选
（`ONLY_TAGS`、`SKIP_TAGS`、`MAX_NODES`）、worker 的端口与状态目录划分、轮次统计
（被中断的轮次满足 `checked < nodes` 并标记为 `interrupted`），以及
Rutube 检测（广告位解析、至少两个视频应答的规则、请求超时与节点不通时的提前放弃）。

## 发布镜像

`.github/workflows/yt-ad-checker-image.yml` 会在 `tools/yt-ad-checker/` 有改动时
以及手动触发时构建并发布镜像。构建完成后先在镜像内运行单元测试，通过后才推送：

```bash
docker pull ghcr.io/<owner>/yt-ad-checker:latest
docker run --rm -e SUBLINK_URL=... -e SUBLINK_API_KEY=... ghcr.io/<owner>/yt-ad-checker:latest
```

每次构建都会打上 `latest`、分支名和 `sha-<short>` 标签；仓库中还保留一个
`buildcache` 标签，让下一次构建复用体积较大的 Playwright 层而无需重新下载。
该任务只在 fork 仓库中运行：上游以源码方式分发该工具，并维护自己的发布流水线。

## 排错

| 现象 | 可能原因 |
|---|---|
| `SUBLINK_API_KEY is required` | 缺少 `.env` 或密钥为空 |
| 每次调用都返回 403 | 访问密钥错误或已失效 |
| 所有节点 `unreachable` | 容器内无法访问 SublinkPro 地址 |
| 所有节点 `unknown` 且为 `LOGIN_REQUIRED` | 出口 IP 触发 YouTube 机器人校验 |
| `mihomo exited during start-up` | 节点 URI 非法，查看 mihomo 日志输出 |

# ai-checker

容器化的检测工具：找出 [SublinkPro](../../README.zh-CN.md) 中哪些节点可以访问
**ChatGPT、Claude、Gemini、Copilot、Perplexity、Grok、Mistral Le Chat、DeepSeek、
Qwen 与 Poe**，并为每种服务同步一对标签。

每个节点都通过独立的 mihomo 实例拨号，每种服务只发一次普通 HTTP 请求（经由该代理），
不使用浏览器。服务正常响应的节点打上 `ai-<service>`；服务明确按地区拒绝的节点打上
`ai-<service>-blocked`；其余情况——人机校验、限流、超时——完全不改动节点。

```
SublinkPro API ──► 节点列表 ──► mihomo（每个节点一个代理）
                                      │
                    ┌─────────────────┴──────────────────┐
                    ▼                                    ▼
        10 个 HTTP 探测并行执行           /proxies/PROBE/delay（6 秒连通性门禁）
        api.openai.com、claude.ai、…                        │
                    │                                       │
                    └──────── available / restricted / unknown
                                      │
                                      ▼
              SublinkPro 标签：ai-<service> / ai-<service>-blocked
```

> SublinkPro 的节点检测页已经能检测 OpenAI、Claude 与 Gemini（每个节点一份
> `UnlockSummary`，可由订阅的 `UnlockRules` 过滤）。本工具是其补充：覆盖十种服务、
> 写入**按服务区分的标签**供任意订阅或客户端过滤，并且绝不会把人机校验变成
> “blocked” 判定。

## 服务

| `SERVICES` 键 | 服务 | 检测地址 |
|---|---|---|
| `chatgpt`（别名 `openai`） | ChatGPT / OpenAI | `api.openai.com/compliance/cookie_requirements` |
| `claude` | Claude | `claude.ai/` |
| `gemini` | Gemini | `gemini.google.com/` |
| `copilot` | Microsoft Copilot | `copilot.microsoft.com/` |
| `perplexity` | Perplexity | `perplexity.ai/` |
| `grok` | Grok（xAI） | `grok.com/` |
| `mistral` | Mistral Le Chat | `chat.mistral.ai/` |
| `deepseek` | DeepSeek | `chat.deepseek.com/` |
| `qwen` | Qwen Chat | `chat.qwen.ai/` |
| `poe` | Poe | `poe.com/` |

`SERVICES` 留空表示全部十种。少选一种服务就少一次节点请求，这正是 `SERVICES`
存在的意义：一个只关心 ChatGPT 与 Claude 的 300 节点部署，每个节点发 2 次请求
而不是 20 次。

## 判定结果

| 结果 | 含义 | 标签 |
|---|---|---|
| `available` | 服务从该出口正常响应 | `+ai-<service>`、`-ai-<service>-blocked` |
| `restricted` | 服务自身声明该地区不受支持 | `+ai-<service>-blocked`、`-ai-<service>` |
| `partial` | 只有部分入口可用，例如 ChatGPT 网页被拒而应用可达（或反之） | – |
| `unknown` | 响应无法说明问题：校验页、429、5xx、超时、节点不可用 | – |

`unknown` 是最关键的一类。Cloudflare 校验页、限流或 `ERR_CONNECTION_RESET`
都不是地区封锁的证据，因此绝不会覆盖上一次的判定。朴素检测工具的典型故障正是在
校验页里搜索 “unavailable”，把大半节点误标为 blocked。

## 判定依据

任何服务的规则之前都会先跑共用守卫：

* 响应头 `cf-mitigated: challenge`，或 Cloudflare 页面（`just a moment`、
  `cf-chl`、`__cf_chl`、`attention required! | cloudflare`）→ `unknown`；
* HTTP `429` → `unknown`；HTTP `5xx` → `unknown`；
* 完全没有响应（超时、TLS 重置、代理错误）→ `unknown`。

随后应用各服务自己的规则：

**ChatGPT** —— 合规接口返回 `unsupported_country` 或普通 `403` 判为 `restricted`；
`2xx`/`3xx` 判为 `available`。随后用 `ios.chat.openai.com` 交叉验证：该接口对
被拒出口返回 `vpn`。当网页与应用的结论相反时节点判为 `partial`
（`web_only` / `mobile_app_only`）；应用接口为空或返回校验页时保留合规接口的结论。
这与核心 `services/unlock` 检测器使用同一对接口，两者结论一致。

**Claude** —— 重定向落到 `app-unavailable-in-region`、出现地区提示语或 `403`
判为 `restricted`；`2xx` 登录页判为 `available`。

**Gemini** —— 页面含 `"45631641":true` 判为 `available`，含 “Gemini isn't
available in your country” 判为 `restricted`。两个标记都不存在时判为 `unknown`
（`gemini_marker_missing`）：Google 经常改版该页面，靠“标记缺失”推断封锁是错的。
这里的 `403` 同样保持 `unknown`，因为大量正常出口也会收到 `403`。

该标记位于转义的 JSON 片段中（`\"45631641\":true`），两种写法都会被匹配。

**Perplexity** —— 首页对大多数机房出口返回托管校验页，而
`perplexity.ai/api/auth/session` 会直接向匿名访问者返回 `{}`，因此优先探测该 JSON
接口，只有它无法给出结论时才回退到首页。

**Claude、Mistral、Poe** —— 这些域名的所有 HTML 路径都由仅 JavaScript 客户端能通过的
Cloudflare 托管校验保护，因此在机房出口通常得到 `unknown`，即使使用浏览器 TLS 指纹
也一样。这是刻意为之：校验页不能说明地区问题，而 `unknown` 永不覆盖标签。未被校验的
出口（住宅、移动网络、部分主机）会正常得出结论。

**其余服务** —— 命中共用地区提示语判为 `restricted`，`2xx`/`3xx` 判为
`available`，`403` 保持 `unknown`：响应体里没有地区提示语时，`403` 更可能是机器人
规则而不是国家封锁。

服务自身 `cdn-cgi/trace` 返回的 `loc=XX` 会在出口属于常见封锁地区时作为提示附加到
日志行。它从不参与判定——服务本身才是权威。

## 谁真的做了地区封锁

下表是 2026 年 11 月供应商层面的限制，仅供参照；具体出口以检测结果为准。

| 服务 | 供应商不提供服务的地区 |
|---|---|
| ChatGPT / OpenAI | 白俄罗斯、中国、古巴、伊朗、朝鲜、俄罗斯、叙利亚、委内瑞拉（香港、澳门不在支持列表中） |
| Claude | 类似名单；不在 Anthropic supported-regions 列表中的地区一律不受支持 |
| Gemini | 阿富汗、白俄罗斯、中国、古巴、伊朗、朝鲜、俄罗斯、叙利亚（应用端另不含香港/澳门） |
| Copilot | 全球可用，但 Microsoft 账号与部分功能按地区绑定 |
| Perplexity | 没有正式的国别封禁；在俄罗斯被运营商级屏蔽，在部分国家过滤下不可用 |
| Grok | 2026 年 7 月在欧盟开放；2026 年 1 月起在印尼、马来西亚被封锁 |
| Mistral Le Chat | 没有国家层面的封禁；部分地区的 GDPR 限制 |
| DeepSeek | 供应商未封禁，但在意大利、韩国、澳大利亚、台湾等地的政府网络中受限或被禁 |
| Qwen | 全球提供 |
| Poe | 没有明确的国别封禁；上游模型供应商可能拒绝某些地区 |

“从某国无法访问”其实包含两件被混为一谈的事：

* **供应商拒绝该地区** —— 服务返回封锁页、重定向或 `unsupported_country`
  → `restricted`，打上 `ai-<service>-blocked`；
* **本地网络封锁该服务** —— 连接直接中断（RKN、DNS 过滤、SNI 阻断）
  → 探测超时 → `unknown`，不改动标签。

第二种情况与“节点本身故障”在探测层面无法区分，因此保持 `unknown` 而不是报成地区
限制。

## 快速开始

```bash
cd tools/ai-checker
cp .env.example .env
# 编辑 .env：填写 SUBLINK_URL 与 SUBLINK_API_KEY
docker compose up -d --build
docker compose logs -f
```

API Key 在 SublinkPro 的 **设置 → 访问密钥** 中创建，通过 `X-API-Key` 请求头发送。
最安全的第一步是先做一次 dry run：

```bash
docker compose run --rm -e DRY_RUN=true -e ONCE=true -e MAX_NODES=5 ai-checker
```

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
| `SERVICES` | 全部 | 逗号分隔的服务键；`openai` 是 `chatgpt` 的别名 |
| `TAG_PREFIX` | `ai-` | 标签为 `<prefix><service>` 与 `<prefix><service>-blocked` |
| `CHECK_INTERVAL` | `6h` | `30m`、`6h`、`1d`、`1h30m` 或纯秒数 |
| `RUN_ON_START` | `true` | 启动后立即执行一轮 |
| `ONCE` | `false` | 只执行一轮后退出（手动运行，忽略 `RUN_ON_START`） |
| `ONLY_TAGS` | – | 仅检测带有其中任一标签的节点 |
| `SKIP_TAGS` | – | 不检测带有其中任一标签的节点 |
| `MAX_NODES` | `0` | 只检测前 N 个节点；`0` 表示不限制 |
| `WORKERS` | `1` | 并行检测的节点数（上限 8，每个 worker 各自启动 mihomo） |
| `ENSURE_TAG` | `true` | 自动创建缺失的标签（可用为绿色，封锁为红色） |
| `DRY_RUN` | `false` | 只记录判定结果，不修改标签 |
| `PROBE_TIMEOUT` | `15` | 单次请求超时（秒） |
| `REACHABILITY_URL` | `https://www.gstatic.com/generate_204` | 任意响应即证明节点可承载流量；mihomo 在探测前先拨号该地址 |
| `PROBE_USER_AGENT` | 内置 Chrome UA | 诊断用覆盖项；多个服务对裸 `python-requests` UA 会返回校验页 |
| `LOG_LEVEL` | `info` | `debug`、`info`、`warning`、`error` |

内部参数（`STATE_DIR`、`MIHOMO_BIN`、`MIHOMO_API`、`MIHOMO_PROXY_PORT`）在容器内
已有合适的默认值，通常无需修改。

## 标签

每个节点对每种服务最终只会带上两个标签中的一个，或者两个都不带：

| 判定 | 添加 | 移除 |
|---|---|---|
| `available` | `ai-<service>` | `ai-<service>-blocked` |
| `restricted` | `ai-<service>-blocked` | `ai-<service>` |
| `unknown` | – | – |

* 添加使用 `POST /api/v1/tags/node/batch-add`，移除使用
  `POST /api/v1/tags/node/batch-remove`——节点上的其他标签完全不受影响；
* 只写差异：已经处于目标状态的节点不会产生任何 API 调用；
* `DRY_RUN=true` 时同样的决策会记录为 `would add …, remove …`；
* `TAG_PREFIX` 允许一个实例检测“正式”节点（`ai-`），另一个实例检测一部分节点
  （`stg-ai-`）而互不冲突。

可以在订阅（Tag 过滤）或客户端中使用这些标签分别构建出站列表，例如只保留
`ai-chatgpt` 标签节点的订阅。

## 调度与并行

* 同一时刻只跑一轮，探测不会重叠；
* 轮次能装进周期时，按下发时间保持节拍；轮次超过周期时，汇总会记录被跳过的次数，
  并把下一轮锚定在当前轮结束之后；
* `/data/scheduler.lock` 为独占锁，共用卷的两个容器不可能同时运行；
* `WORKERS=N` 同时检测 N 个节点。每个 worker 各自启动 mihomo（代理端口 `+i`，
  API 端口 `+i`），状态写入各自的 `worker-<n>/` 目录；`WORKERS=1` 时使用配置中的
  端口与目录，行为与之前完全一致。这里的并行成本很低：纯 HTTP worker 只需要几十兆
  内存，而不是一个浏览器。

十种服务、`WORKERS=2` 下约 200 个节点的整轮检测只需几分钟；无法建立连接的节点会被
6 秒的连通性门禁直接淘汰，而不是把 `PROBE_TIMEOUT` 耗十次。

### 单次运行与节点筛选

```bash
docker run --rm --init --env-file .env \
  -e ONCE=true -e SERVICES=chatgpt,claude -e MAX_NODES=10 \
  ghcr.io/<owner>/ai-checker:latest
```

标签匹配不区分大小写，`SKIP_TAGS` 优先于 `ONLY_TAGS`，筛选发生在任何探测之前。

## 输出

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

`+` 可用、`-` 被封锁、`~` 部分可用、`?` 未知。最后一轮结果同样写入
`/data/state.json`，包含计划检测的节点数（`nodes`）、实际完成数（`checked`）、
`workers` 数量、`seconds_per_node` 平均耗时以及 `interrupted`——被 SIGTERM 中断的
轮次会保持 `checked < nodes`，因此部分完成的轮次不会被误认为完整轮次。

## 说明与限制

* 探测只用 HTTP。这让镜像保持在约 280 MB（Python slim 加 mihomo 二进制，没有
  Chromium，也没有虚拟显示），但把封锁藏在 JavaScript 里的服务会给出 `available`，而
  浏览器会看到封锁页。当前标记的选择方式保证这种情况偏向 `unknown`，而不是错误的
  `restricted`。
* 因为 `unknown` 不会改动标签，服务无法判定时节点会保留旧标签：几个月前被打上
  `ai-claude` 的节点会一直保留，直到某一轮重新得出结论。用 `DRY_RUN=true` 加两次
  可对比的轮次，可以看出实际被判定清楚的比例。
* Cloudflare 防护的域名在 IP 信誉较差的出口上会返回 `unknown`。再加同类服务不会增加
  信息：第一轮结束后看汇总计数器，把持续 `unknown=100%` 的服务去掉。
* 工具通过 `GET /api/v1/nodes/get` 读取原始节点链接，因此节点完全按其 URI 描述被
  测试。链式代理（`DialerProxyName`）等仅存在于导出层的 Clash 额外配置不会生效。
* 一个节点带多个 URI 链接时逐条检测：只要有一条可用就判为 `available`，而
  `restricted` 要求所有链接都不可用。
* 判定是时间点结果。供应商会分批开放地区，节点可能在两轮之间翻转；对比两次
  `DRY_RUN` 结果是识别不稳定节点的好办法。
* 基于 IP 的地区检测反映的是供应商看到的出口，而不是账号看到的结果。付费套餐的
  地区、手机号或账单国家仍可能阻止注册，即使出口判定为 `available`；反过来，
  `restricted` 判定则是确定的。
* 工具不做任何登录，也不发送凭据——它只读取匿名访问者会拿到的内容。
* `PROBE_TIMEOUT` 限制单次请求，同一节点的十次探测在小型线程池（`5`）中执行，
  因此连通性半死的节点每批服务最多消耗约两次超时。

## 测试

单元测试在镜像内运行，宿主机无需安装 Python：

```bash
docker build -t ai-checker .
docker run --rm --entrypoint python ai-checker -m unittest discover -s tests -t .
```

覆盖内容包括：各服务规则（地区提示语、校验页/429/5xx 处理、ChatGPT 网页与应用两端
的 `partial` 合并、Gemini 转义标记、Perplexity 的 JSON 接口回退、各服务阈值、
`openai` 别名）、配置解析（`CHECK_INTERVAL`、`SERVICES`、默认值与校验）、以及标签决策
（`available`/`restricted` 翻转标签对、`unknown` 不改动、`DRY_RUN` 只记录、
重复判定不产生 API 调用）。

## 发布镜像

`.github/workflows/ai-checker-image.yml` 在每次改动 `tools/ai-checker/` 以及手动触发
时构建并发布镜像。它在刚构建好的镜像内运行单元测试，只有通过才推送：

```bash
docker pull ghcr.io/<owner>/ai-checker:latest
docker run --rm -e SUBLINK_URL=... -e SUBLINK_API_KEY=... ghcr.io/<owner>/ai-checker:latest
```

每次构建都会打上分支名与 `sha-<short>` 标签；`latest` 只跟随默认分支，因此功能
分支的推送不会影响生产环境拉取的镜像。该任务只在 fork 中运行：上游从源码分发该
工具，并维护自己的发布流水线。

## 故障排查

| 现象 | 可能原因 |
|---|---|
| `SUBLINK_API_KEY is required` | `.env` 缺失或密钥为空 |
| 每次调用都返回 401/403 | 访问密钥错误或已被吊销 |
| 所有节点都是 `?` 且带 `link unreachable` | 容器无法访问 SublinkPro 地址或节点 URI |
| 所有节点都是 `?` 且带 `cloudflare_challenge` | 出口被校验页拦截；可提高 `PROBE_TIMEOUT`，或接受纯 HTTP 探测无法判断这些服务 |
| 所有节点都是 `gemini_marker_missing` | Gemini 页面改版；用 `LOG_LEVEL=debug` 观察一次运行 |
| `mihomo exited during start-up` | 节点 URI 非法；查看日志中的 mihomo 输出 |
| 标签被创建但从不变化 | `DRY_RUN=true`，或所有判定都是 `unknown` |
| `another ai-checker instance is already running` | 另一个容器共用同一个 `/data` 卷 |

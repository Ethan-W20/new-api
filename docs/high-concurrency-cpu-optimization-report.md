# new-api 高并发 CPU 占用优化审查报告

> 唯一报告文件：本目标后续轮次只更新本文档，不再创建第二份报告。

## 目标与完成规则

- 目标：全方面阅读 `new-api`，在不影响功能的前提下，寻找可大幅降低高并发状态下 CPU / 调度 / 锁竞争 / 热路径序列化占用的优化点。
- 执行规则：每轮并行 6 个子代理审查；主代理汇总、去重、完善本文档。
- 原停止条件：连续 3 轮中，6 个子代理均未发现新的 CPU 占用优化方向，才视为完成。
- 最新停止条件：用户在第 10 轮进行中明确要求“第十轮结束后就终结 goal”；因此第 10 轮合并完成后不再启动第 11 轮。
- 当前状态：第 10 轮继续发现新增 CPU / 锁 / 分配 / 调度优化点；已按最新停止条件完成 10 轮审查并进入终结。
- 透传模式裁剪：已按“全局/渠道请求体透传已开启”的运行前提，删除或收缩透传后不再适用的请求侧处理记录；保留透传后仍会执行的认证、限流、分发、计费、日志、错误、响应、SSE、支付和后台统计路径。

## 轮次记录

### 第 1 轮：2026-05-18

初始 5 个 `explore` 子代理因 `gpt-5.3-codex-spark` 未配置价格失败，已关闭并改用非 spark 标准角色补齐；有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | 入口、路由、中间件、请求体解析 | 发现重复 JSON 解析、日志、限流、重复 token/user setting 获取等热点 |
| Agent 2 | relay streaming core | 发现 OpenAI 流式 token 处理、SSE Render、scanner、ping/timer/goroutine 热点 |
| Agent 3 | provider channel | 发现 OpenAI/Gemini/Ollama/Coze/Cohere/Cloudflare/Zhipu 等流式转换分配热点 |
| Agent 4 | model/db/cache/quota/service | 发现预扣重复 token 查询、Redis quota 增量、DB batch flush、日志 user setting 重取 |
| Agent 5 | common/logger/http client/goroutine/sync | 发现重复 ping、proxy client cold-start、日志锁、rate limiter、perf metrics、affinity stats 热点 |
| Agent 6 | 全局 critic | 发现 channel selection、stream goroutine、token/sensitive join、perf Redis、body materialization 等方向 |

第 1 轮结论：存在大量明确优化方向，未满足停止条件。

### 第 2 轮：2026-05-18

第 2 轮要求 6 个子代理先阅读本文档，再从各自范围寻找“报告未覆盖或需要修正/降级/合并”的点。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | ingress/router/middleware/controller/auth/distributor | 发现 `/v1/models` 重复用户设置获取、channel 设置解析、多 key 选择、model rate limiter check key、active stats、specific-channel auth DB 查询、MJ 无条件日志 |
| Agent 2 | relay/helper streaming core | 发现 header override per-request 处理、stream flush batch 全局创建锁、scanner string materialization；修正 ping 生命周期与 flush batching 表述 |
| Agent 3 | provider channels | 发现 Baidu/Vertex token refresh stampede、Tencent/xAI JSON roundtrip、Dify/Xunfei/Tencent 字符串累积、PaLM 单响应 goroutine、Ali per-poll client、Replicate multipart buffering、MiniMax/SiliconFlow 局部热点 |
| Agent 4 | model/service/cache/quota/billing/token | 发现 tiered billing request capture、billing expr cache 全局锁、subscription billing 额外 DB/事务、pricing RWMap 多锁、token accounting 锁/扫描 |
| Agent 5 | benchmark/test gaps | 发现缺少 relay e2e benchmark、Redis/DB command counter、proxy cold-start、stream leak、global flush-batch lock、OpenAI token-skip benchmark |
| Agent 6 | critical review | 修正 P0-4 ping 表述；发现 channel config hot-path parsing、disabled-debug logging allocation、CustomEvent 分配优化点 |

第 2 轮结论：继续发现多项新增优化方向，连续无新增轮数重置为 0 / 3，未满足停止条件。

### 第 3 轮：2026-05-18

第 3 轮要求 6 个子代理继续在已扩展报告基础上只报告新增/修正/无新增。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | under-reviewed backend areas | 发现敏感词 AC hot path、SSRF/fetch policy 重解析、多模态文件 base64/copy、header-nav config 解析 |
| Agent 2 | concurrency/locks/network hot paths | 发现 channel affinity selection body scan、token/user cache cold-miss stampede、Chat→Responses policy 扫描、MJ byte→string copy |
| Agent 3 | provider/channel | 发现 AWS client per request、provider auth/client 与响应处理局部热点 |
| Agent 4 | model/service/quota/cache/token/billing | 发现 legacy/realtime preconsume 重复 quota 读取、token model-limit map 每请求重建、channel status update 写锁扫描 |
| Agent 5 | benchmark/test gaps | 发现 sensitive-word、header-nav、LogJson 等 benchmark/test gap 与低风险修复 |
| Agent 6 | critical review | 修正 subscription_first COUNT 建议、Redis TTL 语义；发现 affinity matching regex/include/template overhead |

第 3 轮结论：继续发现新增优化方向，连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 4 轮：2026-05-18（FlushWriter / SSE batching 专项复核）

第 4 轮按用户要求聚焦 `relay/helper/common.go` 的 `FlushWriter` 合并 flush、`ForceFlush`、`Done()` 终止 flush，并对照 `CLIProxyAPI-Ethan/sdk/api/handlers/stream_forwarder.go`。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | 并发安全 / 架构语义 | 未发现致命/高风险；确认 batch 存在请求 `gin.Context`，`pending/timer/flusher` 受 mutex 保护；建议清理 `closed` timer 边界 |
| Agent 2 | panic / timer / writer 生命周期 | 未发现进程级致命 panic；发现 `time.AfterFunc` timer goroutine 触碰 `ResponseWriter.Flush()` 生命周期与错误静默风险 |
| Agent 3 | 调用链 / 漏改审查 | 未发现主链路漏 `ForceFlush`；发现导出 `FlushWriter` 名称仍暗示立即 flush，以及部分 `c.Stream/c.Render` 直写 SSE 路径未进入 batch |
| Agent 4 | 测试 / 验证 | 目标 helper 测试和聚焦 race 测试通过；现有覆盖已较充分，剩余缺口收窄为 `FlushWriter` 导出 API 字节级/语义测试 |
| Agent 5 | diff / 调用路径核验 | 确认 flush 改动已在 `48f302aa`；静态未发现 flush batching 导致 CPU 优化目标外的阻断点 |
| Agent 6 | 反方审查 | 未找到致命/高风险证据；确认普通 chunk 最多新增 10ms 可见延迟，ping/done/EOF 路径已 `ForceFlush` |

第 4 轮结论：flush batching 本身未发现致命错误，但继续发现新增优化/加固方向（未覆盖直写 SSE、timer goroutine 生命周期、导出 API 语义、测试缺口），连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 5 轮：2026-05-18

第 5 轮继续要求 6 个子代理先阅读本文档，只报告报告未覆盖或需要修正的高并发 CPU / 锁 / 分配 / 调度优化点；纯 bug / 合规 / 发布风险暂不纳入本文档。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | 架构 / request lifecycle | 发现 task/video 官方适配链多层解析、multipart form 缺 request-scope cache、model rate-limit group snapshot、token IP allowlist parse、用户分组 map copy、proxy client cold-start |
| Agent 2 | 并发 / 缓存 / DB stampede | 发现 rankings 冷 miss DB 聚合 stampede、DataExport 每日志 gopool + 全局锁、subscription plan/info cache miss 无 singleflight |
| Agent 3 | provider / task 运行路径 | 发现 Volcengine TTS alloc、task Vertex ADC 重解析、provider 响应/认证局部热点 |
| Agent 4 | benchmark / verification gap | 发现 `pkg/cachex`、`BodyStorage`、gzip/brotli request decompress 缺并发 benchmark 与行为回归 |
| Agent 5 | 去重 / 修正 | 修正 FlushWriter 测试缺口范围、补充音频/TTS direct flush、降级 CustomEvent 为分配优化点、明确 proxy client 应有正式条目、细化 header-nav/billing expr 测试缺口 |
| Agent 6 | 冷门路径 / critic | 发现 WebSocket realtime 无效 channel 与拷贝/日志、task polling diff/redaction、主动 realtime fetch 上游放大、notify-limit 多 round-trip、model/ratio sync、channel cache rebuild、GPT image price map 分配 |

第 5 轮结论：继续发现新增优化方向，连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 6 轮：2026-05-18

第 6 轮继续要求 6 个子代理先阅读本文档，只报告报告未覆盖或需要修正的高并发 CPU / 锁 / 分配 / 调度优化点；纯 bug / 合规 / 发布风险不纳入本文档。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | API / admin / task fetch | 发现 `/api` response gzip CPU、用户日志读取 JSON roundtrip、task/video fetch `[]byte` 再 `bytes.Buffer` |
| Agent 2 | DB/Redis/cache/goroutine | 发现 subscription DB timestamp roundtrip、email verification 限流/验证码锁、多模态同 URL 跨请求 stampede、MemoryCache disabled 每请求 DB |
| Agent 3 | provider/media/rerank | 发现 image URL→b64_json 全量 buffer/base64、realtime audio base64 全量 decode、embedding/rerank Join、rerank response 全量 JSON roundtrip |
| Agent 4 | benchmark gap | 发现 rerank large documents 和 audio duration/TTS 计费路径缺专项 benchmark/pprof |
| Agent 5 | provider key / logging | 发现 provider 复合 API key 每请求解析、disabled debug/logging allocation 热点 |
| Agent 6 | batch jobs / notifications | 发现 upstream model ignored regex 每模型编译、Codex credential refresh 重取 channel + 全量 rebuild、上游模型更新通知 fan-out 重复解析管理员 setting |

第 6 轮结论：继续发现新增优化方向，连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 7 轮：2026-05-18

第 7 轮继续要求 6 个子代理先阅读本文档，只报告报告未覆盖或需要修正的高并发 CPU / 锁 / 分配 / 调度优化点；纯 bug / 合规 / 发布风险不纳入本文档。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | pricing / file / subscription / model meta | 发现 `/api/pricing` 同步重建、inline base64 全量 decode、subscription reset/expire 逐条事务、model meta enrich 重复规则匹配 |
| Agent 2 | web gzip / stdlib logs | 发现 web/dashboard 动态 gzip、图片 token/quota/MJ 路径未 gate `log.Printf` |
| Agent 3 | provider conversion | 发现 MokaAI embedding 大向量全量 roundtrip、Mistral text-only 转 media 数组、Perplexity 大通用 DTO、OpenRouter enterprise envelope 二次 unmarshal |
| Agent 4 | benchmark gap | 发现 i18n / public misc endpoints 缺并发 benchmark 与快照验证 |
| Agent 5 | verifier | 修正 header override regex 表述；发现全局 RequestId、I18n、PostTextConsumeQuota decimal/gopool、video content proxy 大对象 |
| Agent 6 | critic | 发现 RequestId/i18n、Passkey WebAuthn 构建、`/api/status` snapshot、Generic OAuth access policy 解析热点 |

第 7 轮结论：继续发现新增优化方向，连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 8 轮：2026-05-18

第 8 轮继续要求 6 个子代理先阅读本文档，只报告报告未覆盖或需要修正的高并发 CPU / 锁 / 分配 / 调度优化点；纯 bug / 合规 / 发布风险不纳入本文档。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | session / bytes write / embeddings | 发现全局 session middleware 挂到 relay/token API、`IOCopyBytesGracefully` bytes→buffer→copy、Ollama embedding 大向量 roundtrip |
| Agent 2 | DB/Redis/OAuth/payment | 发现 `/api/usage/token` 重复查 token、Waffo/Waffo Pancake 每请求构建 SDK/解析 RSA key |
| Agent 3 | provider/channel response | 发现 provider 响应侧大对象解析与局部 DTO 分配热点 |
| Agent 4 | error-path verification | 发现 `RelayErrorHandler` 错误风暴大 body CPU/alloc、`ResetStatusCode` mapping 每次解析缺 benchmark |
| Agent 5 | report verifier | 合并 Replicate multipart 重复项；补 StreamScanner string alloc、io.net deployment 管理路径、OAuth client 构造、更多 disabled debug allocation 证据 |
| Agent 6 | critic | 发现 OpenAI Responses 原生处理非流式大对象/流式逐帧完整 unmarshal |

第 8 轮结论：继续发现新增优化方向，连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 9 轮：2026-05-18

第 9 轮继续要求 6 个子代理先阅读本文档，只报告报告未覆盖或需要修正的高并发 CPU / 锁 / 分配 / 调度优化点；纯 bug / 合规 / 发布风险不纳入本文档。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | error / violation-fee | 发现 violation-fee marker 检测会重复触发 `ToOpenAIError`/`MaskSensitiveInfo`，错误风暴下 regex/URL parse 成本未覆盖 |
| Agent 2 | checkin / Epay / topup config | 发现签到状态多次 DB roundtrip、Epay client 每请求重建、支付方式/TopUpInfo 缺 immutable snapshot |
| Agent 3 | Gemini / Responses response | 发现 Gemini embedding 大向量全量 roundtrip 与 Responses 响应侧解析热点 |
| Agent 4 | billing/payment benchmark gaps | 补充预扣后上游失败退款 gopool 风暴、支付订单锁 mutex profile 与幂等验证缺口 |
| Agent 5 | provider/error conversion | 发现 Ollama stream/non-stream timestamp/line/string 分配、Gemini structured schema 每请求递归清洗、错误脱敏 CPU 热点、Claude usage patch JSON 重写 |
| Agent 6 | payment webhook / diagnostics | 发现 Stripe/Creem/Epay webhook body 字符串化与日志/重复解析、订单锁全局 `createLock`、pprof/Pyroscope/performance 端点高压反向增加 CPU/syscall |

第 9 轮结论：继续发现新增优化方向，连续无新增轮数保持 0 / 3，未满足停止条件。

### 第 10 轮：2026-05-18

第 10 轮继续要求 6 个子代理先阅读本文档，只报告报告未覆盖或需要修正的高并发 CPU / 锁 / 分配 / 调度优化点；纯 bug / 合规 / 发布风险不纳入本文档。用户随后要求第 10 轮结束后终结 goal。有效结果来自 6 个子代理：

| 子代理 | 范围 | 结论 |
| --- | --- | --- |
| Agent 1 | ingress/controller/admin/payment | 发现 `/api/option` 写锁读配置、Waffo webhook body 字符串化与二次 JSON、Creem products 每请求解析 |
| Agent 2 | relay retry/billing/task refund | 发现自动禁用渠道关键词扫描、每次 retry 记录 channel error 日志、订阅退款 sleep/gopool/事务、任务退款重复查 token |
| Agent 3 | provider/media response | 发现 Native Gemini passthrough 大向量无用 unmarshal、OpenAI STT 非 JSON ReadAll/unmarshal、MiniMax TTS hex 全量 decode、AWS Claude debug-off LogJson |
| Agent 4 | model/service/cache/metrics | 发现 `SyncOptions` 无变化也重建配置、perf-metrics summary 每请求聚合/Range、日志统计两条聚合 SQL 可合并 |
| Agent 5 | verifier / token cache | 发现 token Redis key 生成中 `GenerateHMAC` 同一请求链重复计算；其余仅给出 CPU 项合并建议 |
| Agent 6 | critic / task/payment | 发现兑换码充值 per-user lock 创建全局串行等锁竞争热点 |

第 10 轮结论：继续发现新增优化方向；按用户最新指令，第 10 轮合并后终结 goal，不再继续三轮无新增循环。

## 优先级优化清单

### P0-1：避免 relay 热路径重复请求体 JSON 解析

- 证据：
  - `router/relay-router.go:84-101`：请求先经过 `Distribute()`，再进入 `controller.Relay()`。
  - `middleware/distributor.go:172-178`, `:273-279`, `:328-342`：分发阶段为提取 model 等信息读取/解析 body。
  - `controller/relay.go:109` + `relay/helper/valid_request.go:19-47`, `:115-118`：Relay 校验阶段再次解析。
  - `common/gin.go:108-136`：`UnmarshalBodyReusable()` 每次 `storage.Bytes()` + JSON unmarshal。
- 问题：大请求（长 messages/tools）在高并发下重复 `encoding/json.Unmarshal` 和 body materialization。
- 建议：
  1. 在 distributor 中缓存已解析的 `dto.Request` 或至少缓存 `model`/基础 metadata 到 `gin.Context`。
  2. Relay 校验阶段优先复用上下文 parsed request；缺失或特殊 multipart/audio/Gemini 路径再 fallback 当前逻辑。
- 风险：中等；必须保留 multipart、audio、Gemini、body 重放和 retry 行为。
- 验证：添加请求体复用单测；用大 messages/tools 对 `/v1/chat/completions` 做 pprof，对比 `encoding/json.Unmarshal` CPU/alloc。

### P0-2：OpenAI 流式响应不应保留所有 chunk 后 join + 全量重解析

- 证据：
  - `relay/channel/openai/relay-openai.go:122-143`：`streamItems []string` 保存每个 SSE chunk。
  - `relay/channel/openai/relay-openai.go:178-184`：无条件调用 `processTokens(...)`。
  - `relay/channel/openai/helper.go:93-107`：构造 `"[" + strings.Join(...) + "]"` 后再 `json.Unmarshal`。
  - 当 `containStreamUsage` 已经为真时，解析出的 token 文本只在 `!containStreamUsage` 下使用。
- 问题：长流式响应下 O(total bytes) 额外内存、join、JSON 重解析；高并发时放大 CPU/GC。
- 低风险第一步：把 `processTokens(...)` 移入 `if !containStreamUsage { ... }`。
- 后续优化：在 scanner callback 中增量累计 `responseTextBuilder` / `toolCount`，只保留 last / second-last chunk。
- 风险：第一步低；增量化中等，涉及 usage/token 计费正确性。
- 验证：10k chunk benchmark；fixture 覆盖 final usage present/absent、audio second-last usage、force-format、thinking-to-content。

### P0-3：限流器高并发锁竞争与 Redis 多 round-trip

- 证据：
  - `common/rate-limit.go:45-69`：内存限流使用单个全局 mutex。
  - `common/rate-limit.go:28-41`：清理也持有同一把锁扫描所有 key。
  - `middleware/rate-limit.go:21-65`, `:155-196`：Redis IP/user 限流每请求多命令 + 时间格式化/解析。
  - `middleware/model-rate-limit.go:24-75`, `:84-127`, `:131-163`：模型请求限流也有类似路径。
- 建议：
  1. 内存限流按 hash 分片，或 per-key bucket 独立锁。
  2. 时间戳改用 Unix int，减少 format/parse。
  3. Redis 用 Lua 或 pipeline 合并 LLEN/LINDEX/LPUSH/LTRIM/EXPIRE，必要时复用已有 `common/limiter` token bucket。
- 风险：中等；窗口边界语义必须保持一致。
- 验证：边界 allow/deny 单测；`b.RunParallel` 多 key/单 hot key mutex profile；Redis ops/request 对比。

### P0-4：重复/高开销 SSE ping 与 stream goroutine/timer 结构

- 证据：
  - `relay/channel/api_request.go:384-481`, `:498-505`：请求阶段启动 ping keepalive，且 `sendPingData` 每次 tick 新建 goroutine + `time.After`。
  - `relay/helper/stream_scanner.go:78-80`, `:136-183`：`StreamScannerHandler` 又启动 ping ticker/goroutine。
  - `relay/helper/stream_scanner.go:186-283`：每个 stream 还有 data handler goroutine、scanner goroutine、channel handoff、waitgroup/timer。
  - `relay/helper/common.go:142-173`：当前 10ms batch flush 用 `time.AfterFunc`，timer callback 会在独立 goroutine 中调用 `ResponseWriter.Flush()`；对比 `CLIProxyAPI-Ethan/sdk/api/handlers/stream_forwarder.go:52-110`，Ethan 在单一 forwarding select loop 内维护 `pendingFrames/flushTimer`，生命周期更集中。
- 建议：
  1. 不可简单删除 request-stage ping；应设计覆盖“等待上游首包前”和“读取上游响应体期间”的单一生命周期 ping owner，或显式 handoff。
  2. 去掉每次 ping 的 goroutine + `time.After`，改为持锁直接写 + 复用 timer/上下文取消。
  3. 不要为了 `wg.Wait()` 再起一个 gopool 任务；改成更轻量的 bounded wait 或单 owner loop。
  4. 中长期把 batch flush timer 收敛进请求/stream 的单 owner loop；短期保留 `AfterFunc` 时至少记录/计数 `flushDue` 错误，避免静默丢 flush 信号。
- 风险：中等；要保留 ping cadence、慢 handler 解耦、timeout、client disconnect 行为。
- 验证：现有 `relay/helper/stream_scanner_test.go` ping/slow-handler 测试；新增高并发 SSE goroutine/block profile。

### P1-1：SSE 每帧写入绕开 `gin.Render`/`CustomEvent` 热路径

- 证据：
  - `relay/helper/common.go:StringData/ObjectData/ClaudeData/ClaudeChunkData/ResponseChunkData` 每 token 调 `c.Render(... CustomEvent ...)`。
  - `common/custom-event.go:65-86` 每次做 `fmt.Sprint`、replacer、prefix 检查、header 写入。
  - `common/custom-event.go:15-29` 定义的是小写 `writeString`，普通 `io.StringWriter`/`WriteString` 不会命中，fallback 会把 string 转为 `[]byte`。
  - `SetEventStreamHeaders` 已经集中设置 SSE headers。
  - 第 4/5 轮补充：仍有未进入 helper batching 的直写路径，例如 `relay/channel/cohere/relay-cohere.go:113,162,165`、`relay/channel/zhipu/relay-zhipu.go:186,195,211,214`、`relay/channel/palm/relay-palm.go:90,93,96`、`relay/channel/xunfei/relay-xunfei.go:139,151,154`，以及音频/TTS 直 flush：`relay/channel/openai/relay-openai.go:324` 每 4KB chunk `flusher.Flush()`、`relay/channel/volcengine/tts.go:281` 每个音频消息 `c.Writer.Flush()`。
- 建议：在 hot path 用 `io.WriteString`/`c.Writer.WriteString` 直接输出完全相同 SSE 字节：`data: ...\n\n`、`event: ...\n`，但必须保留当前 10 帧/10ms flush batching 语义。
- 覆盖范围修正：如果目标是全站 stream 降 flush/Render CPU，需把上述直写 provider 迁移到统一 helper 或提供 batching-aware stream/audio wrapper；否则当前优化主要覆盖 `StringData/ClaudeData/ClaudeChunkData/ResponseChunkData/PingData/Done` 调用链。
- 风险：中低；必须字节级保持 SSE 格式。
- 验证：OpenAI/Claude/Responses/Ping/Done byte-for-byte 单测；10k frame benchmark。

### P1-2：预扣费与日志重复获取 token / user setting

- 证据：
  - `middleware/auth.go:332-380`：认证已 `ValidateUserToken` 并 `userCache.WriteContext(c)`。
  - `service/quota.go:383-400`：`PreConsumeTokenQuota` 再 `GetTokenByKey`。
  - `model/user_cache.go:27-44`：用户 setting 已写上下文。
  - `model/log.go:208-223`：`RecordConsumeLog` 再 `GetUserSetting(userId, false)`。
- 建议：
  1. 预扣前置检查优先使用 context/token quota 信息；严格扣减仍保持当前原子/DB/Redis路径。
  2. `RecordConsumeLog` 优先读 `ContextKeyUserSetting`，缺失才 fallback。
- 风险：中等；quota 正确性敏感。只复用作前置判断和日志选项更安全。
- 验证：Redis/DB call count 从 2 次 token load 降为 1；日志启用时 setting parse profile。

### P1-3：高并发日志同步写与无条件序列化

- 证据：
  - `main.go:174-177`：安装 gin access logger。
  - `middleware/logger.go:19-39`：每请求格式化访问日志。
  - `logger/logger.go:97-118`：每 log 获取 `LogWriterMu.RLock`、格式化时间、同步 `fmt.Fprintf`，`logCount/setupLogWorking` 还存在并发风险。
  - `model/log.go:208-216`：consume log stdout 前对 params 做 JSON 序列化。
- 建议：将 verbose consume stdout log 置于 debug/config gate；logger 计数改 atomic；评估 async/buffered logger 或成功 relay access log 可配置关闭。
- 风险：可观测性/日志顺序变化。
- 验证：日志格式 snapshot；`LogInfo` 并发 benchmark；`go test -race` logger。

### P1-4：channel selection 每请求排序/扫描可预计算

- 证据：
  - `model/channel_cache.go:96-180`：`GetRandomSatisfiedChannel` 每次构造 priority、排序、扫描 channels、求 weight。
  - `model/channel_satisfy.go:8-27`：membership 线性扫描。
- 建议：`InitChannelCache` 时按 `(group, model)` 构建不可变 bucket：priority、totalWeight、channel list/effective weight、channelID set。
- 风险：中等；要保留 retry-priority、权重平滑、禁用 channel、multi-key polling 状态。
- 验证：old/new selection table tests；并发 `InitChannelCache` + selection race；10/100/1000 channel benchmark。

### P1-5：quota Redis 增量、DB batch flush、affinity/perf metrics 可批处理

- 证据：
  - `common/redis.go:275-299`：`RedisHIncrBy` 先 TTL 再 pipeline，至少两阶段。
  - `model/user.go:886-925`, `model/token.go:375-421`：quota delta 可能 gopool fan-out。
  - `model/utils.go:70-95`、`model/user.go:963-1003`、`model/channel.go:824-833`：batch flush 按 id/type 循环单条 update，user used_quota/request_count 分开。
  - `pkg/perf_metrics/metrics.go:57-77`, `pkg/perf_metrics/metrics.go:310-327`：每请求 sync.Map/atomics/Redis pipeline。
  - `service/channel_affinity.go:798-844`：usage stats 每事件 get/mutate/set。
- 建议：
  1. RedisHIncrBy Lua 化，单 round-trip 保持 TTL 语义。
  2. user used_quota + request_count 合并 accumulator。
  3. perf metrics/affinity usage stats 采用 per-worker/local batch flush 或 Redis HINCRBY/Lua。
- 风险：中等；统计精度、TTL、崩溃丢数据窗口需明确。
- 验证：SQL statement count、Redis command count、metrics total golden tests、并发 benchmark。

### P1-7：channel hot-path 配置解析、header override、multi-key 选择应预编译/缓存

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- 证据：
  - `middleware/distributor.go:345-368`：选中 channel 后仍会读取 `GetSetting()`、`GetOtherSettings()`、`GetHeaderOverride()` 等 channel runtime 配置。
  - `model/channel.go:920-984`：上述方法解析 JSON/map。
  - `relay/channel/api_request.go:302-313`, `:340-347`, `:366-371`：API/Form/WSS 请求仍可能调用 `processHeaderOverride`。
  - `relay/channel/api_request.go:173-270`：header override / header passthrough 每请求分配 map、扫描规则、规范化 header；regex 已有 cache，剩余主要是 map/规则扫描成本。
  - `middleware/distributor.go:370-382` + `model/channel.go:198-265`：multi-key 每请求加锁、扫描 key status、分配 enabled index。
- 建议：
  1. `InitChannelCache` 时预解析 immutable channel runtime metadata：settings、other settings、header override、status mapping。
  2. header override 建 per-channel plan：静态 header、passthrough flags、复用 regex cache/compiled regex、placeholder descriptor；无 override fast path 零分配返回。
  3. multi-key 维护 enabled-key ring/bitmap；random 模式避免每次分配 `enabledIdx`，polling 仅更新 atomic/小锁 cursor。
- 风险：中等；channel 更新失效、retry header override、runtime placeholders、key disable/enable 语义必须覆盖。
- 验证：large header override benchmark；retry + channel cache refresh race；multi-key 10/100/1000 key fairness/distribution benchmark.

### P1-8：billing expression、pricing ratio 与 subscription billing 热路径

- 证据：
  - `relay/helper/price.go:241-261`：tiered billing 总是 `ResolveIncomingBillingExprRequestInput()`。
  - `relay/helper/billing_expr_request.go:29-90`：读取 reusable body 并 clone body/header。
  - `pkg/billingexpr/run.go:51-103`, `:126-138`：每次 eval 构造 env map/closures 和 normalized headers。
  - `pkg/billingexpr/compile.go:142-167` 已有 `UsedVars()`，可知道是否引用 `param`/`header`。
  - `pkg/billingexpr/compile.go:35-108`：全局 RWMutex cache，满 256 后全量清空。
  - `relay/helper/price.go:67-115` + `types/rw_map.go:33-37`：ModelPriceHelper 每请求多次 RWMap lock。
  - `service/billing_session.go:401-425`、`model/subscription.go:685-696`, `:984-1066`, `:1189-1205`：subscription_first 先 COUNT，再 row-lock 事务 preconsume，settle 又开事务。
- 建议：
  1. 用 compiled `UsedVars` 跳过不需要的 body/header capture；简单 token-only expr 走轻量 eval。
  2. billing expr cache 用 `sync.Map`/atomic COW + singleflight；替换满 256 全清为 bounded LRU/generation cache。
  3. pricing/ratio 按 model/group/settings generation 构建 immutable snapshot，减少每请求多把 RWMutex。
  4. subscription_first 不应盲目删除 preflight；将 `COUNT` 换成 `EXISTS/SELECT 1 LIMIT 1` 或 cached active-subscription presence，避免无订阅用户进入昂贵 transaction，同时只锁最终候选行；actual quota 等于 preconsume 时减少 settle 二次事务。
- 风险：中高；计费、订阅、表达式缓存失效是高风险区域，必须先 benchmark 与 golden test。
- 验证：已有 billing expr 行为测试基础上补 large-body no-param/with-param、`UsedVars` 跳过 request capture、billing expr cache mutex profile；subscription concurrent same-user SQL count；pricing wildcard/group-special golden tests。

### P1-9：token accounting 锁、扫描与已禁用 debug 日志分配

- 证据：
  - `service/token_counter.go:397-407`：`CountTextToken` 在热路径调用 tokenizer/estimator。
  - `service/tokenizer.go:26-54`：OpenAI token count 每次取 `tokenEncoderMutex.RLock`。
  - `service/token_estimator.go:35-70`, `:178-214`：静态 multipliers 仍走 RWMutex，数学/URL delimiter 检查扫描 string constants。
  - `service/token_counter.go:354-369`：`CountTokenInput` 循环中 `+=`。
  - `relay/compatible_handler.go:177`、`relay/embedding_handler.go:61`、`relay/gemini_handler.go:166,265`：在调用 `logger.LogDebug` 前已经 `fmt.Sprintf` 或 `string(jsonData)`；`logger/logger.go:88-94` 才检查 `DebugEnabled`。
- 建议：
  1. 静态 estimator multipliers 改 immutable/atomic pointer；encoder cache 初始化后 lock-free 或 `sync.Map`。
  2. delimiter 检查用 lookup table/switch；`CountTokenInput` 改 `strings.Builder`。
  3. Debug 日志调用点先 `if common.DebugEnabled` 再构造大字符串，或改成 lazy format API。
- 风险：低到中；token estimate 不能漂移，debug 日志只改变禁用时的无效分配。
- 验证：token golden tests；`b.RunParallel` CountTextToken/EstimateToken/CountTokenInput；disabled-debug alloc benchmark。

### P1-10：provider token refresh / response/client 局部热点

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- Baidu access-token refresh stampede：`relay/channel/baidu/adaptor.go:105-111`、`relay/channel/baidu/relay-baidu.go:191-245`；用 `singleflight.Group` 和 per-token refresh guard。
- Vertex access-token miss herd：`relay/channel/vertex/service_account.go:31-57`, `:66-99`, `:114-125`；缓存 parsed `*rsa.PrivateKey`，token exchange singleflight。
- xAI/Dify/Tencent/Xunfei fallback text：`relay/channel/xai/text.go:55-73`、`relay/channel/dify/relay-dify.go:222-255`、`relay/channel/tencent/relay-tencent.go:98-137`、`relay/channel/xunfei/relay-xunfei.go:168-191`；builder + upstream usage present 时懒累计。
- PaLM：`relay/channel/palm/relay-palm.go:57-99` 为单 full-body response 起 goroutine/channel；可同步写 SSE。
- Ali image polling：`relay/channel/ali/image.go:189-259` 每次 poll 新 `http.Client`；改共享 client。
- MiniMax TTS：`relay/channel/minimax/tts.go:94-105` 每次创建 content type map；改 switch/包级 map。
- 风险：低到中；token refresh/client 缓存需要并发与 key rotation golden。
- 验证：Baidu/Vertex token refresh singleflight benchmark；Dify/Xunfei/Tencent fallback text benchmark；Ali polling shared-client benchmark；MiniMax content-type alloc benchmark。

### P1-11：敏感词、SSRF、header-nav 等策略/配置热路径应按 generation 预编译

- 敏感词：
  - 证据：`controller/relay.go:126-138` 从 `meta.CombineText` 做 prompt sensitive check；`service/sensitive.go:47-48` lower 全 prompt；`service/str.go:75-108` 每次为 dict normalize/sort/hash/cache lookup；`service/str.go:143` 转 `[]rune` 匹配。
  - 建议：`setting.SensitiveWordsFromString` 更新时维护 normalized dict + compiled AC machine + generation，request path 跳过 sort/hash；只保留 lower/text scan。
  - 风险：中等；要保持大小写语义和热更新。
- SSRF/fetch validation：
  - 证据：`service/download.go:62-64` origin fetch 校验；`service/http_client.go:24-28` redirect 校验；`common/ssrf_protection.go:333-354` 每次重建 `SSRFProtection`；`:339-340` 重解析 port range；`common/ip.go:33-49` 重解析 CIDR/IP。
  - 建议：按 fetch setting generation 编译 port set/ranges、domain suffix/exact、CIDR/IP nets 的 immutable snapshot；DNS 如要缓存必须短 TTL + rebinding tests。
  - 风险：中高；安全敏感。
- header-nav：
  - 证据：`router/api-router.go:33-40` 使用 HeaderNav middlewares；`middleware/header_nav.go:23-36`, `:104-133` 每请求拿 OptionMap 锁并 unmarshal JSON config。
  - 建议：option 更新时解析为 atomic immutable `map[module]access`，请求只做 lookup。
- 验证：sensitive-word large prompt benchmark、SSRF policy benchmark/security golden tests、header-nav parallel benchmark + option reload tests。

### P1-12：channel affinity selection 与 token/user cache 冷启动路径

- channel affinity selection：
  - 证据：`middleware/distributor.go:102` selection 前调用 `GetPreferredChannelByAffinity`；`service/channel_affinity.go:564-588` 遍历规则；`:314-322` 对 gjson key source 读取 `storage.Bytes()` + `gjson.GetBytes`；`:248-265` regex cache lookup/compile；`:272-283` include matching 每次 lower subject/pattern；`:596-602` 匹配后 clone `ParamOverrideTemplate`。
  - 建议：预编译 affinity rules（regex、normalized includes、key-source plan）；先 model/path miss fast path 再触 body；每请求 cache gjson 提取值；template clone 延迟到真正 override 应用。
- token/user cache cold-miss stampede：
  - 证据：`middleware/auth.go:332` → `model.ValidateUserToken`；`model/token.go:255-275` token cache miss 无 per-key singleflight；`model/user_cache.go:94-102` user miss 类似；`common/redis.go:107-154`, `:161-238` HGETALL 反射 decode。
  - 建议：token/user DB fallback + Redis backfill 加 per-key singleflight；热点 token/user typed Redis codec，避免每请求反射。
  - 风险：中等；token revocation/user disable 必须立即生效，不可长 negative cache。
- 验证：parallel cold-cache auth benchmark，DB queries/Redis commands/gopool tasks/allocs；affinity large-body hit/miss benchmark。

### P1-14：SDK client / provider auth 非请求体转换热点

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- AWS Bedrock client：
  - 证据：`relay/channel/aws/relay-aws.go:50-88`, `:91-96` 每请求构造 `bedrockruntime.Client`、credentials cache、region/proxy config。
  - 建议：按 channel generation/key type/region/proxy/api-key fp 缓存 client，channel update invalidates。
- Midjourney request body：
  - 证据：`service/midjourney.go:195-200` `[]byte` → `string` → `strings.NewReader`。
  - 建议：`bytes.NewReader(reqBody)` + `http.NewRequestWithContext`。
- 风险：低到中；provider auth/client invalidation 与 MJ 请求字节需 golden tests。
- 验证：AWS client reuse benchmark + key rotation golden；Midjourney request body alloc benchmark。

### P1-15：quota/token/channel cache 细节与 Redis TTL 语义修正

- legacy/realtime quota：
  - 证据：`service/quota.go:90-100` `PreWssConsumeQuota` 重读 `GetUserQuota()`/`GetTokenByKey()`；`service/pre_consume_quota.go:33-67` legacy `PreConsumeQuota` 重读；`service/billing_session.go:349-373` wallet path 重读 `GetUserQuota()`。
  - 建议：扩展 P1-2，context/RelayInfo quota snapshot 只作 pre-check/trust input，最终扣减仍原子。
- token model-limit map：
  - 证据：`middleware/auth.go:421-423` 每请求 `token.GetModelLimitsMap()`；`model/token.go:336-349` split + map alloc；消费者 `middleware/distributor.go:57-69`、`controller/model.go:126-135`。
  - 建议：token load/cache/update 时预计算 immutable model-limit set；请求上下文只引用只读副本/atomic snapshot。
- channel status update write-lock：
  - 证据：`model/channel_cache.go:96-103` selection 取 `RLock`；`:225-246` `CacheUpdateChannelStatus` 写锁下扫描全部 group/model/channel slices。
  - 建议：维护 channel-id membership index，或 copy-on-write rebuild 后短锁 swap generation。
- Redis increment TTL 语义修正：
  - 证据：`common/redis.go:253-272`, `:285-299` `RedisIncr/RedisHIncrBy` 仅 `ttl > 0` 时 mutate，否则 nil；调用方包括 `model/user_cache.go:135-139`、`model/token_cache.go:30-32`、`service/notify-limit.go:80-81`。
  - 建议：Lua 替换前先加 TTL/no-TTL/missing-key tests，明确保留还是修正该行为。
- subscription_first 修正：
  - `P1-8` 中“去掉 preflight COUNT”不可盲目做；`service/billing_session.go:418-424` 用轻量存在性检查避免进入昂贵 subscription transaction。应改为 `EXISTS/SELECT 1 LIMIT 1` 或 cached active-subscription presence，而非 blind removal。

### P1-16：BodyStorage 与请求解压链路减少重复 materialization

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- BodyStorage / gzip / brotli 验证缺口：
  - 证据：报告已覆盖重复 body parse，但未单列 `common/body_storage.go` 存储层和 `middleware/gzip.go` request decompress 路径的 replay、disk fallback、post-decompression size limit benchmark。
  - 建议：大 body path 优化前先补 `BodyStorage` memory/disk repeated `Bytes/Seek`、cleanup、retry replay；gzip/br 解压补 parallel alloc benchmark 和解压后大小限制测试。
- 风险：中等；retry body replay、临时文件生命周期、解压后大小限制必须保持。
- 验证：`BenchmarkBodyStorage_*`、gzip/br decompression bomb guard、repeated replay alloc/RSS benchmark。

### P1-17：缓存冷 miss / 单例构造 / 数据导出避免 stampede 与全局锁

- `/api/rankings` cache miss 聚合 stampede：
  - 证据：`router/api-router.go:40` → `controller/rankings.go:10-11`；`service/rankings.go:143-151`, `:183-197` miss 后构建 snapshot；`model/usedata_rankings.go:21-47` 做 `quota_data` sum/group/order 聚合。
  - 建议：按 `period` 加 singleflight 或 stale-while-revalidate，TTL 边界同 period 只构建一次。
- subscription plan/info cache miss：
  - 证据：`pkg/cachex/hybrid_cache.go:80-108` miss 直接返回；`model/subscription.go:358-372` plan miss 查 DB；`:1161-1177` user subscription info miss 查 subscription 再查 plan。
  - 建议：plan/info 层 per-key singleflight；谨慎短 TTL negative cache。
- `pkg/cachex.HybridCache` 自身：
  - 证据：被 `service/channel_affinity.go`、`model/subscription.go` 等热路径使用，但缺并发 benchmark；Redis miss / JSON codec / `DeleteByPrefix` scan 成本未量化。
  - 建议：为 memory/Redis/json codec/delete-by-prefix 增加 command-count 与 parallel benchmark，指导是否做 codec/singleflight/namespace 优化。
- DataExport quota data：
  - 证据：`main.go:104` 启用；`model/log.go:254-257` 每条 consume log `gopool.Go`；`model/usedata.go:34-35`, `:58-64`, `:67-88` 全局 `CacheQuotaDataLock`，flush 持锁做逐条 DB `First/Create/Updates`。
  - 建议：删除 per-log gopool fan-out，改有界 channel / 分片 accumulator；flush 短锁 swap map，锁外批量 upsert。
- proxy client cold-start：
  - 证据：`service/http_client.go:94-99` cache miss 后释放锁；`:101-121` 无锁 parse/create transport；`:122-124` 写回 map；同 proxy 并发冷启动会重复构造 client/transport。
  - 建议：`singleflight.Group` 或 `LoadOrStore` / double-check；reset 时保留 `CloseIdleConnections()`。
- 风险：中等；cache invalidation、subscription 更新、proxy reset、数据导出准确性要保持。
- 验证：rankings 并发 cold miss SQL count；subscription plan/info 同 key DB count；`HybridCache` command/alloc benchmark；DataExport mutex/block/goroutine profile；proxy client transport creation count。

### P1-18：auth / group / notify / model-rate-limit 配置热路径快照化

- model request rate-limit group config：
  - 证据：`middleware/model-rate-limit.go:186-190` 每请求 `setting.GetGroupRateLimit`；`setting/rate_limit.go:38-50` 全局 RWMutex map lookup；更新路径 `setting/rate_limit.go:30-35` 重建 map。
  - 建议：更新时构建 immutable snapshot / atomic pointer，请求只做 lock-free lookup；与 `model request memory limiter` 的 non-mutating peek 一起验证。
- token IP allowlist：
  - 证据：`middleware/auth.go:351-360` 每请求 `token.GetIpLimits()` + `IsIpInCIDRList`；`model/token.go:59-78` 每次 clean/split；`common/ip.go:33-49` 每次 `net.ParseCIDR` / `net.ParseIP`。
  - 建议：token load/cache/update 时预编译 `[]netip.Prefix` + exact IP set，context 引用只读 snapshot。
- user usable groups / auto group：
  - 证据：`middleware/auth.go:382-386` 校验 token group；`middleware/distributor.go:110-114` auto group path；`service/group.go:10-36` 每次复制全局 map；`:45-52` auto group 基于复制 map。
  - 建议：按 userGroup + group-ratio/config generation 预计算 effective group set 与 auto group slice。
- notify-limit：
  - 证据：`service/notify-limit.go:57-81` Redis 模式 GET→SET/INCR 多 round-trip；`:98-115` 内存模式 `sync.Map Load` 后修改再 `Store`。
  - 建议：Redis Lua `INCR + EXPIRE + limit check` 单 round-trip；内存使用 per-key atomic/mutex 计数器。
- 风险：中等；token 更新、group 配置更新、notify 防刷窗口语义必须保持。
- 验证：IP allowlist 100 CIDR benchmark；group N=10/100/1000 parallel benchmark；rate-limit config snapshot benchmark；notify-limit Redis command count + parallel correctness/perf tests。

### P1-19：provider response / auth / 计费表局部分配热点

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- Volcengine TTS：`relay/channel/volcengine/tts.go:146-184` base64 full decode；`protocols.go:274-350`, `:507-528` 每帧 buffer/reader/marshal；改 offset + `binary.BigEndian` 零临时对象路径。
- task Vertex token：`relay/channel/task/vertex/adaptor.go:85-98`, `:106-115`, `:263-267` submit/fetch 重复解析 ADC JSON 与取 token；Init/runtime metadata 缓存 project/key，token 获取 singleflight。
- GPT image price：`service/tool_billing.go:71-72` → `setting/operation_setting/tools.go:163-188` 每次构造嵌套 map；改 switch/包级静态表。
- 风险：中等；provider auth token 失效、TTS 协议字节、计费表语义需覆盖。
- 验证：Volcengine protocol golden/alloc、Vertex token-exchange count、GPT image price helper benchmark。

### P1-20：Realtime WebSocket、task polling、主动 fetch、管理同步与 channel cache rebuild

- WebSocket realtime：
  - 证据：`relay/channel/openai/relay-openai.go:346-347` 创建 `sendChan/receiveChan`，但只在 `:407`, `:513` 非阻塞写入，未见消费者；`:400`, `:506` `[]byte`→`string`，`relay/helper/common.go:365-371` 再 `[]byte` 写 WS；`:394`, `:467`, `:499`, `:482-484` 每事件 info/debug 字符串构造与输出。
  - 建议：删除无人消费 channel；新增 `WssBytes` 直接写 text message；每事件日志 debug gate / sample / aggregate。计费相关 `CountTokenRealtime`、`PreWssConsumeQuota` 不可跳过。
- task polling：
  - 证据：`service/task_polling.go:253`, `:274-282` 对 old/new JSON bytes 排序比较；`:375`, `:383`, `:397`, `:504-520` debug string 与 video redaction unmarshal/marshal。
  - 建议：保存 provider raw data hash / canonical JSON / typed field diff；debug gate 后再构造大字符串；redaction 先 `bytes.Contains` fast path。
- 用户主动 realtime fetch：
  - 证据：`relay/relay_task.go:421-440` 每次 fetch 都 `GetChannelById` + `adaptor.FetchTask`；`:486-503` 再解析 body 探测格式。
  - 建议：同 task 短 TTL + singleflight 实时查询缓存，终态立即稳定。
- model / ratio sync：
  - 证据：`controller/model_sync.go:77-79`, `:133-210` 缓 raw body/etag 但每次仍 unmarshal；`controller/ratio_sync.go:195-222` 大量 upstream 起 goroutine；`:536-608` `buildDifferences` 多层循环反复 `valueMap(...)`。
  - 建议：按 `(url, etag)` 缓 decoded snapshot + singleflight；ratio sync 固定 worker pool；diff 前预归一化 field maps。
- channel cache rebuild：
  - 证据：`model/channel_cache.go:21-67` `InitChannelCache` 全量 DB.Find、split group/models、sort、写锁 swap；调用点如 `controller/channel.go:695,709,745,768,820,848,982`。
  - 建议：管理 API 批量操作合并一次 rebuild；单 channel 更新用 generation + copy-on-write delta 或短 debounce。
- 风险：中等；realtime 计费、task 实时性、channel 变更可见性必须保持。
- 验证：Realtime WS 10k event alloc/log benchmark；task polling 1k tasks diff/redaction benchmark；active fetch storm 上游 call count；model/ratio sync large config benchmark；channel batch update rebuild count。

### P1-21：Embedding / Rerank / audio 计费与响应大对象热点

> 透传裁剪：已删除 image URL → `b64_json` 等请求侧图片转换记录；保留 token/usage 计费、rerank 响应、realtime/audio duration 这类透传后仍可能执行的路径。

- rerank 大 documents 计费/响应：
  - 证据：`dto/rerank.go:25-38` 每次 `fmt.Sprintf` documents + `strings.Join`；`controller/relay.go:126-137`、`service/token_counter.go:222-225` 敏感词/计数消费整段 `CombineText`；`relay/common_handler/rerank.go:18-73`、`relay/channel/cohere/relay-cohere.go:217-245`、`relay/channel/siliconflow/relay-siliconflow.go:16-43` 响应 `ReadAll` / unmarshal / re-marshal。
  - 建议：token counting 增加 fragment/iterator 输入；rerank documents 对 string 走 type assert，非 string 才 fallback；响应侧用 `json.RawMessage` envelope，只解析 usage/meta 小字段，大 `results/documents` 原样拼回；debug body string 仅 debug gate 内限长构造。
- embedding token meta：
  - 证据：`dto/embedding.go:35-45` `ParseInput()` 后 append 到 `texts` 并 `strings.Join`；`:58-74` `[]any` 再建 `[]string`。
  - 建议：embedding/rerank token meta 一趟累加，避免构造完整 Join 字符串。
- realtime/audio duration：
  - 证据：`service/token_counter.go:300-323`, `:374-390` realtime audio delta 调 `parseAudio`；`service/audio.go:9-31` 全量 base64 decode 后只用 decoded length 算 duration；`relay/channel/openai/audio.go:80-110` TTS response 后读取 body 并解析 duration；`common/audio.go:21-48` duration 解析无条件 `SysLog(fmt.Sprintf(...))`；`:312-340` AAC 路径 `io.ReadAll` 后扫描。
  - 建议：pcm/g711 用清洗后的 base64 length + padding 推导 decoded byte length；必要校验时用流式 decoder/复用 buffer；duration debug log gate；AAC/MP3/WAV duration 尽量 header/stream scan。
- 风险：中到中高；token counting、敏感词、usage、duration 计费与响应 JSON 语义必须保持。
- 验证：`BenchmarkEmbeddingTokenMeta_10kInputs`、`BenchmarkRerankTokenCountMeta_LargeDocuments`、`BenchmarkRerankResponseHandler_LargeReturnDocuments`、Realtime audio delta 1KB/64KB/1MB benchmark、`BenchmarkGetAudioDuration_{MP3,AAC,WAV}_Parallel`。

### P1-22：provider credential parsing 预解析

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- provider 复合 API key 每请求解析：
  - 证据：`relay/channel/aws/adaptor.go:95`、`relay/channel/aws/relay-aws.go:64`、`relay/channel/baidu_v2/adaptor.go:65`、`relay/channel/xunfei/adaptor.go:84`、`relay/channel/volcengine/adaptor.go:54,291`、`relay/channel/volcengine/tts.go:200`、`relay/channel/codex/adaptor.go:156` + `relay/channel/codex/oauth_key.go:26`。
  - 建议：按 channel/key generation 预解析 credential struct，放入 channel runtime metadata；key 更新时失效。
- 风险：中等；credential/key 更新、provider auth 语义必须保留。
- 验证：provider credential parse benchmark + key rotation golden。

### P1-23：Batch job / notification / credential refresh 的周期性 CPU 放大点

- upstream model ignored regex：
  - 证据：`controller/channel_upstream_update.go:210-217` 在 `lo.Filter(upstreamModels)` 内对每个 `ignoredModel` 调 `regexp.MatchString`；`:652-679` 由定时 batch job 周期运行。
  - 建议：每个 channel/settings snapshot 预拆 ignored models 为 exact set + compiled regex list；按 settings generation 缓存。
- Codex credential refresh：
  - 证据：`service/codex_credential_refresh_task.go:69-80` 已选 `id/name/key/status/channel_info`；`:104-122` 已解析 key/expiry 后又调用 `RefreshCodexChannelCredential`；`service/codex_credential_refresh.go:42-55` 再 `GetChannelById` + 重解析 key；`service/codex_credential_refresh_task.go:133-142` 有刷新就 `InitChannelCache()` + `ResetProxyClientCache()`。
  - 建议：自动刷新路径传入已加载 channel/key snapshot；key-only 更新后定向更新 channel cache entry 或 debounce full rebuild；proxy cache 按实际影响定向失效。
- 上游模型更新通知 fan-out：
  - 证据：`service/user_notify.go:25-30` 每次查询所有启用管理员 setting；`:37-43` 对每个 user `GetSetting()`；`model/user.go:82-90` 每次 JSON unmarshal；`service/user_notify.go:108-115`, `:188-218` 重复占位符替换 / JSON marshal。
  - 建议：维护 watcher snapshot（userId/email/notifyType/endpoint/enabled）；用户 setting 变更时失效；通知内容按类型预渲染一次。
- 风险：中等；channel/key/setting 热更新与通知语义必须保持。
- 验证：`BenchmarkCollectPendingUpstreamModelChanges_RegexIgnored`、1k/10k Codex channel refresh benchmark、`BenchmarkNotifyUpstreamModelUpdateWatchers_NAdmins`。

### P1-24：API response gzip、日志/验证码/MemoryCache disabled 等边缘高并发入口

- `/api` response gzip：
  - 证据：`router/api-router.go:15-18` 整个 `/api` 组 `gzip.Gzip(gzip.DefaultCompression)`；日志/用量接口 `router/api-router.go:296-309`、任务列表 `:334-337` 也在该组下。
  - 建议：对高频 JSON 管理/列表接口增加 gzip 阈值、BestSpeed 或可配置禁用；小响应跳过压缩，snapshot 类接口缓存压缩结果。
- 用户日志读取：
  - 证据：`controller/log.go:36-54` user logs；`model/log.go:387-423` 查询后 `formatUserLogs`；`model/log.go:55-66` 每条 `StrToMap(log.Other)` 后删除字段再 `MapToJsonStr`；`common/str.go:37-48` 是 JSON marshal/unmarshal；`model/log.go:433-448` 每查询新建 `strings.NewReplacer`。
  - 建议：`Other == ""` 或不包含 `admin_info` / `stream_status` fast path 跳过 JSON；package-level replacer；中期写入 user-safe `Other`。
- task/video fetch response：
  - 证据：`relay/relay_task.go:293-299` 已有完整 `respBody []byte`；`:301-302` 又 `io.Copy(c.Writer, bytes.NewBuffer(respBody))`；`router/video-router.go:21-24` 是公开轮询路径。
  - 建议：直接 `c.Data` / `c.Writer.Write(respBody)`，空响应使用 package-level bytes 常量。
- subscription DB time：
  - 证据：`model/db_time.go:7-17` 每次 `GetDBTimestamp()` 发 SQL；subscription 调用点 `model/subscription.go:459,827,980,1104,1147`。
  - 建议：请求/事务/后台批处理轮次内 memoize DB timestamp，减少额外 DB roundtrip。
- email verification：
  - 证据：`middleware/email-verification-rate-limit.go:25-44` Redis `INCR` + 首次 `EXPIRE` + 超限 `TTL`；`common/verification.go:35-44` 注册验证码全局 mutex，超过 10 条锁内 `removeExpiredPairs()`；`:47-60` 校验/删除同锁。
  - 建议：Redis Lua 合并 `INCR+EXPIRE+TTL`；验证码 map 分片锁/TTL cache/Redis，过期清理移出请求临界区。
- MemoryCache disabled channel selection：
  - 证据：`model/channel_cache.go:88-91` MemoryCache disabled 时走 `GetChannel()`；`model/ability.go:91-143` 查 abilities、算权重、`DB.First`，retry 非 0 额外 `getPriority()`。
  - 建议：若保留无内存缓存模式，增加短 TTL read-through selection cache 或合并 ability+channel 查询；也可文档化生产必须开启 MemoryCache。
- 风险：低到中；gzip 带宽、日志脱敏、task fetch bytes、DB 时间语义、验证码防刷、MemoryCache disabled 兼容需保持。
- 验证：API gzip `Accept-Encoding` pprof；user logs 20/100/1000 `-benchmem`；task fetch byte-for-byte + alloc；subscription SQL/request；verification Redis command/mutex profile；MemoryCache disabled selection DB statements/request。

### P1-25：全局 middleware、公开 misc、Passkey/OAuth 登录链路快照化

- RequestId 全局中间件：
  - 证据：`main.go:174-177` 全站启用 `RequestId()/PoweredBy()/I18n()`；`middleware/request-id.go:18-28` 每请求拼接 `GetTimeString()` + `_bp` + `GetRandomString(8)`，并 `context.WithValue` / `Request.WithContext`；`common/utils.go:265-268` 每次 `time.Now().UTC()` + `Format` + `fmt.Sprintf`；`common/str.go:30-35` 随机字符串。
  - 建议：relay 高并发路径支持轻量 request id（atomic/雪花式预格式化前缀）或懒生成；保留外部可观测兼容。
- I18n 全局中间件与翻译：
  - 证据：`main.go:176` 全局安装；`middleware/i18n.go:14-16,23-36` 每请求探测 user setting / `Accept-Language`；`i18n/i18n.go:180-197` `strings.Split`；`:217-230` `SupportedLanguages()` 每次返回新 slice；`:64-107`, `:124-176` 翻译时 localizer map 锁/语言读取；`model/user_cache.go:233-238` session 用户语言 fallback。
  - 建议：无分配首段扫描 `Accept-Language`，`IsSupported` 改 switch/静态 set；middleware 写入规范化语言后翻译路径直接信任 context；session 用户语言做 request-scope cache。
- 公开 misc endpoints：
  - 证据：`router/api-router.go:21-32` 暴露 status/notice/about/home；`controller/misc.go:42-165,169-227` 每请求持 `OptionMapRWMutex`、拼大 `gin.H`、扫描 custom OAuth providers；`oauth/registry.go:59-71` registry RLock 遍历 providers；`common/constants.go:78-79` 状态字段。
  - 建议：按 option/config/custom-oauth generation 维护 immutable response snapshot；请求只返回快照。
- Passkey：
  - 证据：`controller/passkey.go:52,107,224,259` begin/finish 多处 `BuildWebAuthn`；`service/passkey/service.go:25-79` 每次构造 `webauthn.Config` / `webauthn.New`；`:82-128` 每次 split origins、`url.Parse`、`fmt.Sprintf`；`:159-176` 解析 proxy proto；`model/passkey.go:75-92` credential 转换 base64 decode + transports JSON unmarshal。
  - 建议：按 passkey settings generation + host/scheme 缓存 `WebAuthn` / resolved origins/RPID；credential 读取后缓存 decoded typed struct。
- Generic OAuth access policy：
  - 证据：`oauth/generic.go:266-280` 每次 trim/parse/evaluate access policy；`:333-341` JSON parse + validate；`:401-457` condition 逐个 `gjson.Get`；`:623-672` deny message 每次 `regexp.MustCompile` 模板正则。
  - 建议：custom provider 注册/更新时预编译 access policy、field path、deny-message 模板；运行时只 evaluate 已编译结构。
- 风险：中等；request id 可观测性、语言优先级、公开 status 内容、Passkey origin/RPID、OAuth allow/deny 语义需保持。
- 验证：`BenchmarkRequestIdMiddleware_Parallel`、`BenchmarkI18nMiddleware_AcceptLanguage_Parallel`、`BenchmarkPublicMiscEndpoints_StatusNoticeAboutHome_Parallel`、`BenchmarkPasskeyBuildWebAuthn_Parallel`、`BenchmarkGenericOAuthAccessPolicy_Parallel`，并配 response / language / passkey / OAuth golden。

### P1-26：Pricing / text quota / subscription reset / model meta 管理路径 CPU 峰值

- `/api/pricing` cache 过期同步重建：
  - 证据：`router/api-router.go:33` 暴露；`controller/pricing.go:37-74` 每请求取 pricing/group ratio/usable groups；`model/pricing.go:66-75` cache 过期时请求线程持 `updatePricingLock` 和 `modelSupportEndpointsLock` 调 `updatePricing()`；`:110-119` 查 abilities/model meta；`:141-167` rule × abilities 嵌套匹配；`:222-267` 同一 endpoints JSON 两轮 unmarshal。
  - 建议：atomic immutable pricing snapshot + stale-while-revalidate；请求读旧快照，后台/singleflight refresh；model meta endpoint JSON 写入/刷新时预解析。
- text quota settlement：
  - 证据：`service/text_quota.go:211-226` 每次 relay 完成后大量 `decimal.NewFromInt/NewFromFloat`；`:254-297` 多轮 decimal `Mul/Add/Div/Round`；`:350-362` 日志 extraContent 重复 decimal 计算；`:476-477` 每请求 `gopool.Go` 调 `perfmetrics.RecordRelaySample`。
  - 建议：预计算常用 ratio/scale，合并重复 decimal 计算；只在需要日志字段时构造 extra；perfmetrics 改 per-worker batch/local queue。
- subscription reset/expire 后台任务：
  - 证据：`service/subscription_reset_task.go:34-83` 每分钟循环 batch；`model/subscription.go:827-898` expire 按 user 逐个事务；`:1104-1134` reset 每条订阅查 plan 后单独事务锁行/`Save`。
  - 建议：按 plan/user 分组批处理；预加载 plan map；事务内只锁需变更行；expire downgrade 单独队列/分批，避免与高并发计费事务集中争锁。
- model meta enrich 管理端：
  - 证据：`router/api-router.go:351-359` admin models；`controller/model_meta.go:163-187` 每次拆 exact/rule + 查 bound channels；`:210-259` pricing × rule model 嵌套匹配；`:263-323` 聚合 matched models/channel set；`model/model_meta.go:112-128` join abilities/channels。
  - 建议：按 pricing/channel generation 缓存 rule model enrich 结果；列表分页后合并快照，模型/渠道/ability 更新失效。
- 风险：中到中高；pricing 可见性、计费金额、订阅状态/用户组、管理端绑定渠道必须保持。
- 验证：`BenchmarkGetPricing_ExpiredCache_Parallel`、`BenchmarkPostTextConsumeQuota`、10k due subscriptions benchmark + lock wait、1k pricing × 100 rule models 管理列表 benchmark。

### P1-27：剩余 provider response 局部分配热点

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- MokaAI embedding response：
  - 证据：`relay/channel/mokaai/relay-mokaai.go:55-83` `ReadAll` → unmarshal 大向量 `[]float64` → OpenAI DTO → marshal。
  - 建议：用 `json.RawMessage` envelope 只解析/改写 model/usage 小字段，`data[].embedding` 原样透传；兼容时 raw passthrough。
- OpenRouter enterprise wrapper：
  - 证据：`relay/channel/openai/relay-openai.go:207-222` enterprise envelope unmarshal 到 `json.RawMessage Data` 后再 unmarshal 成 `OpenAITextResponse`。
  - 建议：浅解析 envelope，成功时原样写 `data`，仅 usage 修正/format conversion 时完整 decode。
- 风险：中等；provider JSON 兼容、usage、force_format 字段需覆盖。
- 验证：`BenchmarkMokaEmbeddingHandler_LargeVectors`、`BenchmarkOpenRouterEnterpriseHandler_LargeData` + provider golden。

### P1-28：Web/dashboard gzip、未 gate stdlib log、inline base64 与视频代理大对象

- web / old dashboard 动态 gzip：
  - 证据：`router/web-router.go:29-32` 全站 `gzip.Gzip(gzip.DefaultCompression)` 后才 `Cache()`/static；`:33-44` SPA fallback 每请求动态压缩；`router/dashboard.go:10-16` old billing route 也用 default gzip；`middleware/cache.go:7-15` 未见预压缩 `.gz/.br` 或压缩结果缓存。
  - 建议：静态资源优先服务构建期 `.br/.gz` 或关闭动态 gzip；SPA index 按 `(theme, Accept-Encoding)` 缓存压缩 bytes；old dashboard 复用 P1-24 gzip 阈值/BestSpeed/可配置禁用策略。
- 未 gate stdlib logs：
  - 证据：`service/token_counter.go:99-114` 图片 token 计数无条件 `log.Printf`；`service/quota.go:112-116` realtime auto-group 预扣费无条件 `log.Printf`；`service/midjourney.go:233-239` `string(responseBody)` 后无条件打印整包响应。
  - 建议：统一 debug gate 后再格式化/转 string；MJ 响应限长打印。
- inline base64 file source：
  - 证据：`service/file_service.go:93-103` Base64Source；`:348-353` 无条件 `DecodeString(cleanBase64)`；`:356-369` 缓存仍保存 cleanBase64；`:372-373` 图片再 decode config；`:397-406` cache 无 config 时 `GetImageConfig` 再 decode。
  - 建议：base64 size 用长度/padding 推导；仅 decode MIME sniff/header 所需前缀；图片 config 用 streaming decoder 或缓存 raw header/config。
- video content proxy：
  - 证据：`router/video-router.go:12-16` `/v1/videos/:task_id/content`；`controller/video_proxy.go:161-190` data URL `strings.SplitN` + 整段 base64 decode 后一次性写出；`controller/video_proxy_gemini.go:42-56`, `:156-170` Gemini/Vertex fallback `io.ReadAll` 再解析 task result/map。
  - 建议：data URL 视频用 streaming base64 decoder 写 response；fallback fetch 复用 task result snapshot/typed parser，避免全量 map/ReadAll 可避免路径。
- 风险：低到中；gzip headers/cache、日志可观测性、data URL/MIME/尺寸、视频 bytes 必须保持。
- 验证：web/static/dashboard gzip pprof；`BenchmarkEstimateRequestToken_Image_DebugOff`、`BenchmarkMidjourneyResponseLog_100KB`；5MB/20MB inline base64 `LoadFileSource`；large data URL video proxy benchmark。

### P1-29：错误风暴、status mapping 与 bytes 写出路径降低分配

- `RelayErrorHandler` 大 body 错误路径：
  - 证据：`service/error.go:86-128` upstream 4xx/5xx 时 `io.ReadAll`、`json.Unmarshal`、`string(responseBody)` 与日志分支都会在错误风暴下放大 CPU/alloc。
  - 建议：错误体大小限长、按 channel/error format 做 shallow parse；debug/log 展示懒构造；保留 OpenAI/Claude/Gemini/非 JSON body 映射语义。
- violation-fee marker 检测触发脱敏热路径：
  - 证据：`controller/relay.go:228-231` 重试前调用 `NormalizeViolationFeeError`，`:170-177` defer 失败时再次归一化并计费；`service/violation_fee.go:30-38`, `:73-81` 在 `HasCSAMViolationMarker` / `shouldChargeViolationFee` 中调用 `err.ToOpenAIError().Message`；`types/error.go:180-205` 的 `ToOpenAIError()` 调 `common.MaskSensitiveInfo`；`common/str.go:188-253` 对错误文本跑多轮 regexp、`url.Parse`、`url.ParseQuery`、split/join。
  - 建议：marker 检测只读取 raw/typed error message，不走带脱敏副作用的 `ToOpenAIError()`；一次归一化后缓存检测结果或只让计费阶段识别已归一化 code；客户端响应/日志阶段仍保留 masking。
- status code mapping：
  - 证据：`service/error.go:131-153` + 多处 `relay/*_handler.go` 调 `ResetStatusCode(...)`；每次解析 mapping JSON。
  - 建议：channel runtime metadata 预解析 status-code mapping；请求仅做 map lookup。
- `IOCopyBytesGracefully`：
  - 证据：`service/http.go:44-50` 已有 `[]byte` 又 `bytes.NewBuffer` + `io.NopCloser`；`:65` `fmt.Sprintf("%d", len(data))`；`:74-78` 再 `io.Copy`；调用点如 `relay/channel/openai/relay-openai.go:297`、`relay/channel/claude/relay-claude.go:934`、`relay/channel/gemini/relay-gemini.go:1495,1541`。
  - 建议：直接 `c.Writer.Write(data)`，`Content-Length` 用 `strconv.Itoa`，保留 header/status/flush 语义。
- task/video fetch bytes：
  - 已在 P1-24 记录；同属已有 bytes 包装 copy 问题，验证可复用 byte-for-byte golden。
- 风险：中等；错误消息、retry/skip、violation fee marker/计费、status mapping、header 写入顺序、Content-Length 与 flush 行为必须保持。
- 验证：`BenchmarkRelayErrorHandler_LargeErrorBody_Parallel`、`BenchmarkNormalizeViolationFeeError_NonCSAMError_Parallel`、`BenchmarkResetStatusCode_Mapping_Parallel`、`BenchmarkIOCopyBytesGracefully_1KB_100KB_1MB`；OpenAI/Claude/Gemini 非流式与 error response golden；CSAM marker / ordinary error / already-normalized code golden。

### P1-30：全局 session、token usage、OAuth/payment/deployment 管理边角路径

- 全局 cookie session middleware：
  - 证据：`main.go:178-187` `sessions.Sessions("session", store)` 全局挂到 engine；`router/relay-router.go:69-106` 高并发 `/v1` token relay 也经过；`router/video-router.go:10-16` video content 用 `TokenOrUserAuth()`；`middleware/auth.go:194-206` 先 `sessions.Default(c)` 再 fallback token；依赖 `gin-contrib/sessions` 每请求创建 wrapper，cookie 存在时 gorilla securecookie decode。
  - 建议：session middleware 下沉到 dashboard/OAuth/passkey/2FA 等需要 session 的路由组；`TokenOrUserAuth` 对有 `Authorization` / `mj-api-secret` 的 API 请求先 token auth，缺失再尝试 session。
- `/api/usage/token` 重复查 token：
  - 证据：`router/api-router.go:275-281` 先 `TokenAuthReadOnly()`；`middleware/auth.go:214-232` 已 `GetTokenByKey`；`controller/token.go:118-139` 再解析 Authorization 并再次 `GetTokenByKey`。
  - 建议：middleware 将 `*model.Token` / normalized key 写入 context，controller 复用；顺便 `strings.Cut` 替换 `Split`。
- io.net deployment 管理路径：
  - 证据：`controller/deployment.go:42-55` 每请求 `ionet.NewClient/NewEnterpriseClient`；`pkg/ionet/client.go:25-29,89` 新建 `http.Client`；`pkg/ionet/jsonutil.go:13-25,50-90` flexible time decode 走 `Unmarshal(interface{}) → normalize → Marshal → Unmarshal`，并多次 `time.Parse`；`controller/deployment.go:208,245,297,521,541,565,601,647,707,762` 等入口触发。
  - 建议：按 API key/env generation 缓存 ionet client；flexible time 实现 typed custom unmarshal，避免全对象 normalize roundtrip。
- OAuth 登录 HTTP client：
  - 证据：`oauth/github.go:72,114`、`oauth/discord.go:74,119`、`oauth/oidc.go:76,123`、`oauth/linuxdo.go:79,122`、`oauth/generic.go:134,215` 每次 token/userinfo 请求新建 `http.Client`。
  - 建议：按 provider/proxy/config generation 缓存 `http.Client`/transport；与 Generic OAuth access policy 预编译合并验证。
- Waffo / Waffo Pancake：
  - 证据：`controller/topup_waffo.go:25-48`, `:225`, `:333` 每次构建 SDK/config；依赖 SDK config build 会校验 RSA key；`service/waffo_pancake.go:103-109,228-258,355-394` 每次 normalize/pem decode/x509 parse/RSA sign/verify。
  - 建议：按 payment settings generation / sandbox 缓存 SDK、`*rsa.PrivateKey`、`*rsa.PublicKey`；配置更新失效。
- 风险：中等；session/auth 路由覆盖、token usage 权限、OAuth/payment key rotation、deployment config 更新必须保持。
- 验证：`BenchmarkRelay_NoSessionMiddleware_Parallel`、`BenchmarkVideoContent_TokenWithSessionCookie`、`/api/usage/token` SQL counter、deployment client/flexible-time benchmark、OAuth client reuse benchmark、Waffo sign/verify benchmark + key rotation golden。

### P1-31：OpenAI Responses、Gemini/Ollama/MokaAI embedding 响应与流式分配热点

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- OpenAI Responses native：
  - 证据：`relay/channel/openai/relay_responses.go:23-44` 非流式完整 `ReadAll` + `Unmarshal` 到 `dto.OpenAIResponsesResponse` 后原 bytes 写回；`:82-91`, `:93-117`, `:133-139` 流式每个 SSE data 都 unmarshal 成完整 `dto.ResponsesStreamResponse`，但多数只需 `type`、usage、少量 image/tool 字段和 text delta。
  - 建议：非流式 shallow DTO / `json.RawMessage` 只解析 error、usage、tools/image 必需字段；流式先轻量事件类型探测，仅关键事件局部解析；completion fallback 只在缺 usage 时累计。
- Ollama / MokaAI / Gemini embedding 大向量响应：
  - 证据：`relay/channel/ollama/relay-ollama.go:257-277` 完整 unmarshal embeddings 到 `[]float64` 再 marshal；`relay/channel/mokaai/relay-mokaai.go:55-83` 同类 ReadAll/unmarshal/DTO/marshal；`relay/channel/gemini/relay-gemini.go:1503-1541` `io.ReadAll` → `dto.GeminiBatchEmbeddingResponse` → 循环复制 `embedding.Values` → `common.Marshal`。
  - 建议：`json.RawMessage` envelope 只解析 usage/error/index/prompt count，小向量数组尽量原样拼 OpenAI response 或延迟 decode。
- Ollama stream/non-stream 转换分配：
  - 证据：`relay/channel/ollama/stream.go:50-58` 每 chunk `created_at` 先后 `time.Parse(time.RFC3339Nano)` / `time.Parse(time.RFC3339)`；`:88` `scanner.Text()` 分配 string；`:198` 非流式先 `string(body)` 再 `strings.Split(raw, "
")` 全量切分。
  - 建议：timestamp 解析失败结果缓存或只解析一次；流式用 `scanner.Bytes()` / byte pipeline；非流式按行扫描 bytes，避免 body→string→split 全量分配。
- Claude `message_delta` usage patch 低优先级 JSON 重写：
  - 证据：`relay/channel/claude/relay-claude.go:814` 每个 `message_delta` 调 patch；`:680-691` 最多 5 次 `setMessageDeltaUsageInt`；`:697-707` 每次 `gjson.Get` 后 `sjson.Set` 重写整段 data。
  - 建议：在单次 patch 中解析/构建 usage 字段，或仅在字段缺失时一次性写入，避免多次全段 JSON copy。
- 风险：中等；Responses SSE bytes/usage、embedding index/usage、Claude usage patch 语义必须保持。
- 验证：`BenchmarkOaiResponsesHandler_LargeOutput`、`BenchmarkOaiResponsesStreamHandler_10kEvents`、`BenchmarkOllamaEmbeddingHandler_LargeVectors`、`BenchmarkMokaEmbeddingHandler_LargeVectors`、`BenchmarkGeminiEmbeddingHandler_LargeVectors`、`BenchmarkOllamaStream_10kChunks` + golden。

### P1-32：StreamScanner string 分配与 disabled debug log 证据扩展

- StreamScanner 每 chunk string 分配：
  - 证据：`relay/helper/stream_scanner.go:240` 每行 `scanner.Text()` 分配 string；`:252` 再 `strings.TrimSpace`；`:187` `dataChan := make(chan string, 10)` 固化 string 传递。
  - 建议：改用 `scanner.Bytes()` / byte-slice pipeline；仅在 handler 需要 string 时转换；data channel 可传 `[]byte`/pooled buffer，谨慎处理 scanner buffer 生命周期。
- disabled debug logging allocation 证据扩展：
  - 证据：报告 P1-9 已列部分调用点；补充 `relay/image_handler.go:80`、`controller/task_video.go:99,105,119`、`service/task_polling.go:375,383,397`、`relay/channel/ali/image.go:325,327`、`relay/channel/openai/adaptor.go:380,389,401,421`，这些在 `logger.LogDebug` 内部检查 `DebugEnabled` 前已经 `fmt.Sprintf` 或 `string(responseBody/originRespBody/jsonData)`。
  - 建议：统一 lazy debug API 或 `if common.DebugEnabled` 包裹所有大字符串/bytes→string 构造。
- stdlib log 补充：
  - P1-28 已列 token/image/quota/MJ `log.Printf`；与本项一起做 debug-off benchmark。
- 风险：中等；scanner buffer 生命周期、handler API、debug log 可观测性需保持。
- 验证：`BenchmarkStreamScannerHandler_10kChunks_BytesPipeline`、debug-off benchmarks，pprof 确认 `scanner.Text` / `fmt.Sprintf` / bytes→string 分配下降。


### P1-33：支付、签到、充值配置与退款调度热路径

- 签到状态/动作 DB roundtrip：
  - 证据：`controller/checkin.go:16-27` GET 调 `model.GetUserCheckinStats`；`model/checkin.go:149` 查当月 records，`:164`→`:43-49` 再 COUNT 今日，`:169-170` 再分别 COUNT(total)/SUM(quota)；`model/checkin.go:61-63` POST 插入前先 `HasCheckedInToday`，而 `model/checkin.go:16-17` 已有 `user_id, checkin_date` 唯一索引。
  - 建议：状态用一次聚合查询返回 total/today/quota，或从当月 records 推导 today；写路径 insert-first，唯一约束冲突映射为“今日已签到”。
- Epay client 与订单锁：
  - 证据：`controller/topup.go:134-145` `GetEpayClient()` 每次 `epay.NewClient`，调用点 `controller/topup.go:222,343`、`controller/subscription_payment_epay.go:81,144,199`；`controller/topup.go:267-306` `LockOrder/UnlockOrder` 所有订单回调都经过全局 `createLock` 创建/引用计数/删除；订阅回调 `controller/subscription_payment_epay.go:160-163,210-212` 复用该锁。
  - 建议：按 `(PayAddress,EpayId,EpayKey,config generation)` 缓存 Epay client；订单锁改固定分片锁或 `sync.Map.LoadOrStore + per-entry mutex + atomic refcount`，同时优先依赖 DB 幂等事务减少应用层锁路径。
- 支付方式 / TopUpInfo immutable snapshot：
  - 证据：`controller/topup.go:24-119` 每请求组装支付方式，`:108` 调 `setting.GetWaffoPayMethods`；`setting/payment_waffo.go:27-39` 每次 RLock `OptionMap` 后 JSON unmarshal；`setting/operation_setting/payment_setting_old.go:52-58` `ContainsPayMethod` 线性扫描 `[]map[string]string`；`controller/topup_waffo.go:158-172` Waffo 请求再次解析 methods 并线性查找。
  - 建议：按配置 generation 构建 payment/topup immutable snapshot，包含 enabled flags、pay method set、Waffo method list、amount options/discount；请求只读快照，必要时浅拷贝返回。
- 支付 webhook / 支付请求 body 字符串化与重复解析：
  - 证据：`controller/topup_stripe.go:155-164` Stripe webhook `io.ReadAll` 后 `string(payload)` 写 info 日志再验签；`controller/topup_creem.go:237-268` Creem webhook 多次 `string(bodyBytes)` 进入日志/验签/错误日志并重置 `Request.Body` 后 `ShouldBindJSON`；`controller/topup_creem.go:147-160`、`controller/subscription_payment_creem.go:30-40` Creem 支付请求先 `ReadAll` 再 `bytes.NewReader` 供 bind；`controller/topup.go:325-336,352-355,377-405` Epay webhook 用 `lo.Keys + lo.Reduce` 复制参数，多处 `common.GetJsonString(...)` 进入日志；`controller/topup_waffo.go:326,341-347,353-364` Waffo webhook `ReadAll` 后 `string(bodyBytes)` 进入日志/验签，并对同一 body 先后 unmarshal 成 `core.WebhookEvent` 与扩展 payload。
  - 建议：常规 info 日志只记录 len/hash/event_type/trade_no/status，body 全量内容 debug gate + 限长；Creem/Waffo HMAC/验签尽量复用 bytes 或单次 string；JSON 用 envelope + `json.RawMessage` / typed payload 避免整包二次 unmarshal；Epay 参数复制改普通 for-loop，`GetJsonString` 仅 debug/异常限长构造。
- Creem products 配置每请求解析：
  - 证据：`controller/topup_creem.go:77-90` 每次 `/api/user/creem/pay` 都 `json.Unmarshal([]byte(setting.CreemProducts), &products)` 后线性遍历查 `ProductId`。
  - 建议：纳入 payment/topup generation snapshot，预编译 `product_id -> product` map；请求路径只做 map lookup。
- 兑换码充值 per-user lock 创建全局串行：
  - 证据：`controller/user.go:1047-1085` `topUpLocks` + `topUpCreateLock` 创建锁，`:1094-1100` 每次 TopUp 获取并 TryLock，`:1101-1107` 锁内 bind + redeem。
  - 建议：`sync.Map.LoadOrStore` 去掉全局创建锁，或分片锁 / TTL 回收 per-user lock；保持同用户串行。
- 预扣后上游失败退款调度风暴：
  - 证据：`service/billing_session.go:81-122` 与 `service/pre_consume_quota.go:17-27` 已预扣后失败退款会在高并发错误风暴下放大 gopool 调度、DB/Redis 命令与 block profile。
  - 建议：失败退款路径做有界队列/批量化/幂等去重，避免每个失败请求无界 `gopool.Go` 风暴；保持 wallet/token/subscription 余额与幂等退款语义。
- 风险：中高；支付幂等、订单锁同 `tradeNo` 串行、同用户兑换串行、配置热更新、签到唯一约束、退款计费余额必须保持。
- 验证：`BenchmarkCheckinStats_SQLCount_Parallel`、`BenchmarkUserCheckin_InsertFirst`、`BenchmarkGetEpayClient_Parallel`、`BenchmarkEpayNotify_1kDistinctOrders_MutexProfile`、`BenchmarkLockOrder_UniqueTradeNo_Parallel`、`BenchmarkLockOrder_SameTradeNo_Parallel`、`BenchmarkTopUpLock_UniqueUsersParallel`、`BenchmarkGetTopUpInfo_Parallel`、`BenchmarkRequestWaffo_MethodResolve_Parallel`、`BenchmarkStripeWebhook_LargePayload_LogInfo`、`BenchmarkCreemWebhook_LargePayload_VerifyAndBind`、`BenchmarkWaffoWebhook_LargePayload_Parallel`、`BenchmarkCreemProductResolve_Parallel_NProducts`、`BenchmarkEpayNotify_Params_Parallel`、`BenchmarkRefundAfterPreconsume_ErrorStorm`；同订单只充值一次、同用户重复拉起/兑换、配置更新失效、退款余额 golden。

### P1-34：诊断/性能端点与 profiling 采样在高压时的反向 CPU/syscall 开销

- pprof 自动采样：
  - 证据：`main.go:148-152` `ENABLE_PPROF=true` 时启动 pprof server 和 `common.Monitor()`；`common/pprof.go:13-44` 循环 `cpu.Percent(time.Second)`，CPU 超阈值时立即 `pprof.StartCPUProfile` 持续 10s，正好在高并发高 CPU 时增加采样开销。
  - 建议：自动采样增加冷却时间、最大并发保护和显式开关；高压默认只保留低成本状态快照，不自动 CPU profile。
- Pyroscope profile types / mutex/block rate：
  - 证据：`common/pyro.go:21-25,38-50` 配置 Pyroscope 时默认开启 mutex/block profile rate，并采集 CPU/alloc/inuse/goroutine/mutex/block 多类 profile。
  - 建议：mutex/block profile rate 默认 0，需显式配置才启用；profile types 可配置，生产高压仅保留必要类型。
- `/api/performance/*` dashboard 轮询：
  - 证据：`controller/performance.go:90,103,120,356-379` `/api/performance/stats` 每请求 `runtime.ReadMemStats`、`IsRunningInContainer`、`GetDiskSpaceInfo`、扫描磁盘缓存目录；`controller/performance.go:197-225,232-258` 日志列表每请求扫描并排序日志文件。
  - 建议：返回后台定时刷新快照；容器检测启动时缓存，磁盘/缓存目录/日志列表低频刷新，避免 dashboard 轮询触发 syscall/GC 压力。
- 风险：低到中；诊断可观测性和手动排障能力要保留，采样开关/冷却时间需可配置。
- 验证：`BenchmarkPerformanceStats_Polling`、`BenchmarkGetLogFiles_100_1000Files`；压测 `ENABLE_PPROF/PYROSCOPE_URL` 开关组合，对比 CPU、alloc、mutex/block profile 开销。


### P1-35：配置、选项、性能指标与日志统计的后台/控制台 CPU 热路径

- `SyncOptions` 无变化也重建全局配置：
  - 证据：`model/option.go:191-206` 周期性 `AllOption()`；`model/option.go:225-550` 对每个 option 逐个加 `OptionMapRWMutex`，并重复解析 JSON/ratio/payment/sensitive 等配置。
  - 建议：批量加载为 map，先按 key/value diff；仅变化项在锁外解析并发布 immutable snapshot。
- `/api/option` 只读路径使用写锁并重建 ratio meta：
  - 证据：`router/api-router.go:179-182` 暴露 `/api/option`；`controller/option.go:87-112` 在只读 `GetOptions` 中 `OptionMapRWMutex.Lock()` 遍历；`controller/option.go:69-84` 每次解析多个 ratio JSON 并 marshal `CompletionRatioMeta`。
  - 建议：按 option generation 构建 immutable admin-options snapshot，请求路径 atomic read；敏感字段过滤与 `CompletionRatioMeta` 预计算。
- perf metrics summary 每请求聚合/遍历：
  - 证据：`controller/perf_metrics.go:13-33`；`pkg/perf_metrics/metrics.go:125-176`；`model/perf_metric.go:71-79` `/api/perf-metrics/summary` 每次 DB `GROUP BY model_name`，再全量遍历 `hotBuckets` 构造/排序结果。
  - 建议：按 `(hours,bucket generation)` 做短 TTL summary snapshot；`hotBuckets` 维护 model 维度增量索引，避免每次全量 `sync.Map.Range`。
- 日志统计两条聚合 SQL 可合并：
  - 证据：`controller/log.go:98-121`, `:125-149`；`model/log.go:451-490` 一次统计请求分别查询总 quota 与最近 60 秒 rpm/tpm。
  - 建议：合并为单条条件聚合 SQL，例如 `SUM(quota)` + `SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END)` + token 条件和。
- 风险：中等；配置热更新、敏感字段过滤、dashboard 指标新鲜度、日志过滤条件必须保持。
- 验证：`BenchmarkSyncOptions_NoChange_100_1000Options`、`BenchmarkGetOptions_Parallel_1000Models`、`BenchmarkPerfMetricsQuerySummaryAll_10kBuckets_Parallel`、`BenchmarkSumUsedQuota_Parallel_WithFilters`；SQL count、mutex profile、pprof 对比 `encoding/json` / `sync.Map.Range` / DB CPU。

### P1-36：relay retry、错误日志、token cache HMAC 与退款/任务计费调度

- 自动禁用渠道关键词扫描在每次 retry 错误中重复执行：
  - 证据：`controller/relay.go:356-363`；`service/channel.go:45-64`；`service/str.go:132-148` 每次失败重试都会 `err.Error()`、`strings.ToLower`、`[]rune(findText)`、AC 搜索并分配命中词。
  - 建议：对同一 `NewAPIError` / retry attempt 缓存 disable 判定；`stopImmediately=true` 时避免构造 words；大错误体只扫描截断摘要。
- channel error 日志每个失败 retry 都构造/脱敏/DB 写入：
  - 证据：`controller/relay.go:366-398`；`model/log.go:147-189` 中间 retry 也会 `MaskSensitiveErrorWithStatusCode()`、构造嵌套 map、`MapToJsonStr`、`GetUserSetting`、`LOG_DB.Create`。
  - 建议：默认只在 retry 耗尽后记录完整 error log；中间 channel 失败写轻量计数/采样；复用已脱敏错误文本和 context 中的 `RecordIpLog`。
- token Redis key HMAC 同一请求链重复计算：
  - 证据：`common/crypto.go:17-20`；`model/token_cache.go:11-14,30-32,43-45`；`middleware/auth.go:332`；`model/token.go:405-412` 每次 token auth/cache hit、quota Redis 增减、cache set/delete 都可能重新 `hmac.New(sha256.New)` + `hex.EncodeToString`。
  - 建议：保留 Redis key 不存明文语义，但在 auth 阶段计算一次 `tokenCacheKey` / hashed key，写入 context 或 Token runtime 字段，后续 quota/cache update 复用。
- 订阅退款失败路径 gopool 内 sleep 重试并嵌套/串联事务锁：
  - 证据：`service/billing_session.go:106-118`；`service/funding_source.go:111-117,126-136`；`model/subscription.go:1078-1095,1189-1205`。
  - 建议：改为有界延迟重试队列；退款使用 tx-scoped helper 在同一事务内更新 record 与 subscription delta。
- 异步任务失败退款重复查询 token：
  - 证据：`service/task_billing.go:152-165`；`:73-79,100-112,171-180`；`model/log.go:273-281`；`model/token.go:238-251` 每个失败任务退款先 `GetTokenById` 调 quota，日志路径再查 token name。
  - 建议：`RefundTaskQuota` 内一次性取 token snapshot 并传给 quota/log；或提交任务时保存不可变 `tokenName`，quota 更新走 id-based cache path。
- 风险：中高；自动封禁、错误日志可观测性、HMAC 安全语义、requestId 幂等、退款只执行一次、锁顺序和 token 删除/重命名语义必须保持。
- 验证：`BenchmarkShouldDisableChannel_LargeError_RetryStorm`、`BenchmarkRelayErrorLog_RetryStorm`、`BenchmarkValidateUserToken_RedisHit_WithQuotaSettle`、`BenchmarkSubscriptionRefund_ErrorStorm`、`BenchmarkRefundTaskQuota_Parallel_FailedTasks`；mask 次数、SQL/request、HMAC alloc/op、DB lock wait、重复退款/token deleted golden。

### P1-37：剩余 provider 响应大对象与 debug-off 分配热点

> 透传裁剪：本节已移除请求体转换/请求侧重建类记录，仅保留透传后仍会执行的热点。

- Native Gemini passthrough 无用完整 unmarshal：
  - 证据：`relay/channel/gemini/relay-gemini-native.go:24,35,55,66-80` 写回原 body，却完整 unmarshal text/embedding；embedding 大向量被全量解码但结果不用。
  - 建议：只 shallow parse `usageMetadata` / block reason / error；embedding passthrough 跳过向量 unmarshal。
- OpenAI STT 非 JSON 响应无效 `ReadAll`/unmarshal：
  - 证据：`relay/channel/openai/audio.go:120,125,130` 不论 `responseFormat` 都 `ReadAll` 后尝试 JSON unmarshal。
  - 建议：按 `responseFormat` / Content-Type gate；text/srt/vtt 等非 JSON 直接 copy/stream，JSON/verbose_json 才 shallow parse usage。
- MiniMax TTS hex audio 全量 decode：
  - 证据：`relay/channel/minimax/tts.go:109,121,151,163` 全量读 JSON、unmarshal 巨大 hex audio，再 `hex.DecodeString` 分配完整音频后写出。
  - 建议：hex audio 用 `hex.NewDecoder(strings.NewReader(audio))` 流式写，或 shallow 提取 audio/meta，避免额外 decoded slice。
- AWS Claude debug-off 仍无条件 marshal：
  - 证据：`relay/channel/aws/dto.go:47-54` debug-off 仍调用 `logger.LogJson`，会 marshal 整个请求；大 messages/tools 下放大 JSON CPU/alloc。
  - 建议：`if common.DebugEnabled` gate 或 lazy logger；`anthropic-beta` 用 typed slice / `RawMessage` 避免小 JSON marshal。
- 风险：中等；provider usage/error/block reason、响应 bytes、debug 输出格式必须保持。
- 验证：`BenchmarkNativeGeminiEmbeddingPassThrough_LargeVectors`、`BenchmarkOpenAISTT_NonJSONTranscript_CopyVsReadAll`、`BenchmarkMiniMaxTTS_HexAudio_5MB`、`BenchmarkAWSClaudeRequestConvert_DebugOff_LargeMessages`；bytes unchanged / audio bytes / debug format golden。

### P2-2：其他低成本入口与运行时热点

- `/v1/models` 重复 user setting/group：`router/relay-router.go:19-21`、`middleware/auth.go:367-380`、`model/user_cache.go:27-33`、`controller/model.go:115-123`, `:155-180`；优先用 context。
- model request memory limiter “check key” 会 mutate：`middleware/model-rate-limit.go:147-162` + `common/rate-limit.go:45-68`；增加 non-mutating peek 或合并 check+record。
- active connection stats：`router/relay-router.go:17`、`middleware/stats.go:17-27`；可配置关闭或 sharded counter。
- specific-channel auth：`middleware/auth.go:314-330`, `:429-437`、`model/user.go:723-733`；`strings.Cut` 替代 `Split`，role 从 user cache/context 读取或缓存。
- Midjourney 无条件 stdout：`controller/relay.go:403-429` `log.Println(mjErr)`；移除或 debug gate。
- stream flush batch 全局创建锁：`relay/helper/common.go:26`, `:36-63`；stream setup 时初始化 batch，避免所有 stream 首帧共用全局 mutex。

### P2：provider 局部低风险分配优化

- OpenAI/Gemini cross-format：`relay/channel/gemini/relay-gemini.go` 先 Gemini unmarshal，再 Gemini→OpenAI marshal string，再 `openai.HandleStreamFormat` 重新 unmarshal；可增加 object-based format path。
- Ollama：`relay/channel/ollama/stream.go` tool call arguments 用 `interface{}` 后 re-marshal；可改 `json.RawMessage`。
- Coze/Cohere/Cloudflare：多处 `responseText += ...`；改 `strings.Builder`。
- Cohere scanner：`strings.Index(string(data), "\n")`；改 `bytes.IndexByte`。
- Zhipu：`ScanLines` 后再 `strings.Split(data, "\n")`；可移除内层 split。
- Gemini：每 candidate 构造 `[]string` + `strings.Join`；单 part fast path 或 builder。

## 建议验证矩阵

1. 基准：
   - `BenchmarkRelayChatCompletions_LargeBody_DistributeToRelay`：小请求、100 messages、1k messages、tools-heavy；证明重复 body parse 改动。
   - `GetRandomSatisfiedChannel`：10/100/1000 channels。
   - `InMemoryRateLimiter.Request`：多 key / 单 hot key `b.RunParallel`。
   - `StreamScannerHandler`：1k/10k chunks、ping on/off、并发 streams。
   - `BenchmarkStringData_FirstFrame_ParallelStreams`：观察 `streamFlushBatchCreateMu` 首帧全局锁。
   - `StringData/ObjectData`：10k SSE frames `-benchmem`。
   - `BenchmarkDirectProviderStreamPaths`：Cohere/Zhipu/PaLM/Xunfei 直写 SSE 路径迁移前后 flush 次数、allocs 和 CPU 对比。
   - `OpenAI stream processTokens`：final usage present/absent。
   - `UnmarshalBodyReusable`：大 messages/tools 请求。
   - `BenchmarkNewProxyHttpClient_SameProxyColdStart_Parallel` / `ManyProxyURLs_Parallel`：证明 proxy client cold-start singleflight。
   - `BenchmarkModelPriceHelper`：hot model/group pricing snapshot。
   - `BenchmarkBillingExpr_LargeBody_NoParam` / `WithParam`：证明 UsedVars 跳过 request capture。
   - provider 局部：Baidu/Vertex token refresh singleflight、fallback text builder、Ali polling shared client。
   - body/decompress：`BodyStorage` memory/disk replay、gzip/br decompress parallel alloc。
   - cache/stampede：rankings cold miss SQL count、subscription plan/info DB count、`HybridCache` Redis command/alloc、proxy client transport creation count。
   - auth/config：token IP allowlist 100 CIDR、user usable groups/auto group N=10/100/1000、model-rate-limit group snapshot。
   - realtime/task/admin：Realtime WS 10k events alloc/log、task polling diff/redaction、active fetch storm upstream call count、model/ratio sync large config、channel batch update rebuild count。
   - media/rerank：embedding/rerank token meta、rerank large response raw envelope、realtime audio base64 delta、audio duration MP3/AAC/WAV。
   - provider credential：provider composite key parsing / channel-key generation cache。
   - batch jobs/API edges：upstream ignored regex, Codex credential refresh, notification watcher fan-out, user logs pagination, task fetch `[]byte` write, subscription DB timestamp, email verification limiter, MemoryCache disabled selection。
   - middleware/public/login：RequestId、I18n、public misc/status snapshot、Passkey WebAuthn build、Generic OAuth access policy。
   - pricing/billing/admin：pricing expired refresh、PostTextConsumeQuota decimal/perfmetrics、subscription reset/expire、subscription/task refund storm、model meta enrich、SyncOptions diff、GetOptions snapshot、perf-metrics summary snapshot、log stats conditional aggregate。
   - remaining providers/media: MokaAI embeddings, OpenRouter enterprise wrapper, inline base64 file source, video content proxy, OpenAI STT non-JSON, MiniMax TTS hex, AWS Claude debug-off LogJson。
   - error/session/responses：RelayErrorHandler 大错误体、violation-fee masking、auto-disable keyword scan、retry error log sampling、token cache HMAC reuse、status-code mapping、IOCopyBytesGracefully、session middleware/token-first、OpenAI Responses 原生流/非流、Ollama/Gemini/MokaAI embedding、Native Gemini passthrough、Ollama stream timestamp/string、DeepSeek suffix、Ali image edit、DTO flexible values。
   - deployment/OAuth/payment/scanner：io.net deployment client/time decode、OAuth HTTP client reuse、Waffo/Epay SDK/client/RSA cache、payment/topup snapshot、Creem product snapshot、checkin SQL count、payment webhook body/logging、Waffo webhook body、order/topup lock mutex profile、refund error-storm gopool、StreamScanner bytes pipeline、disabled debug allocation。
2. pprof：
   - 非流式高 RPS chat completion。
   - 流式 1k/10k chunks。
   - Redis enabled rate limit + metrics。
   - 多 channel auto-group selection。
   - DataExport enabled consume log：gopool/goroutine/mutex/block profile。
   - `/api` response gzip：gzip vs identity，`compress/flate` CPU 与响应大小/p95。
   - web/dashboard gzip、RequestId/I18n 全局中间件、pricing/model-meta locks、subscription reset lock waits。
   - upstream error storm、violation-fee masking、auto-disable keyword scan、retry error logging、subscription/task refund storm、session middleware on relay、OpenAI Responses 10k events、Ollama/Gemini conversion large payload、Native Gemini passthrough、StreamScanner string allocation、OAuth/login/payment burst、payment webhook/body logging、option/perf/log dashboard polling、performance dashboard polling、pprof/Pyroscope enabled。
3. 回归：
   - quota/token billing golden tests。
   - sensitive-word / token count golden tests。
   - SSE byte-for-byte tests。
   - Redis/DB command count instrumentation：go-redis hook 统计 commands/request；GORM custom logger 统计 SQL statements/request。
   - goroutine/timer leak benchmark：N 并发短流/长流，记录 `runtime.NumGoroutine()` before/after、block profile、client cancel cleanup。
   - Flush batching 回归：现有覆盖基础上补 `FlushWriter` 导出 API 字节级/语义测试、首帧 10ms 可见、`Done`/`PingData` 立即可见。
   - task/video、multipart、realtime billing、channel update generation 的 golden / invalidation tests。
   - rerank/embedding token estimate、image MIME/response_format、audio duration/token usage、provider credential invalidation、日志脱敏、验证码防刷、MemoryCache disabled 兼容的 golden tests。
   - request id observable format、language priority、public misc/status JSON、Passkey origin/RPID、OAuth allow/deny、pricing snapshot、subscription reset/expire、model meta enrich、video proxy bytes 的 golden tests。
   - error mapping/status mapping、violation-fee marker/masking、auto-disable keyword/retry log semantics、token HMAC key reuse without plaintext, non-stream response bytes, session route coverage, OpenAI Responses bytes/SSE, embedding usage/index, Native Gemini passthrough bytes unchanged, MiniMax TTS audio bytes, AWS Claude debug format, Waffo/Epay key rotation, payment snapshot invalidation, Creem product invalidation, checkin unique-index conflict, payment idempotency/refund balance 的 golden tests。

## 禁止的伪优化

- 不可通过关闭 token counting、sensitive check、quota pre-consume、日志 DB 记录来“优化”CPU；这会改变功能/安全/计费语义。
- 不可删除 stream ping/flush，除非有兼容性测试证明代理与客户端行为一致。
- 不可为了减少 WebSocket realtime CPU 跳过 `CountTokenRealtime`、`PreWssConsumeQuota` 或必要计费路径。
- 不可缓存可变 channel/token/user 指针而没有失效和 race 覆盖。
- 不可全局替换 JSON 库或 unsafe 转换，除非先用 profile 锁定热点并有兼容测试。

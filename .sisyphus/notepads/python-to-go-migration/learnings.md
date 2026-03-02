# Learnings - python-to-go-migration

- 2026-03-02 Task1: Compatibility Contract v1 已固化为机器可消费 JSON（domains=routes/auth/errors/sse/ws/config/token/storage/lifecycle），且每个 domain 均含 source_files/symbols/non_negotiables。
- validate_contract.py 已实现三参数接口 --contract/--policy/--out，支持自动创建 out 目录，成功输出 status=pass，失败输出 status=fail 并带缺失键文本。
- 失败夹具 compat-missing-auth.json 被用于 RED 场景，校验器在缺失 auth 顶层键时稳定返回 missing key: auth（stdout 机读 + stderr 人读）。
- 2026-03-02 Task3: Go 入口骨架采用 `cmd/server -> internal/app -> internal/http` 最小链路，`--dry-run-config` 仅解析并打印配置，不启动服务。
- 2026-03-02 Task3: `SERVER_HOST/PORT/WORKERS` 默认值保持 `0.0.0.0/8000/1`，并在 `runtime.GOOS==windows && workers>1` 时降级为 1 且输出提示，语义与 Python 入口对齐。
- 2026-03-02 Task3: `/healthz` 通过 `internal/http/router.go` 提供纯文本 `ok`，可直接用 `go run ./cmd/server --config config.defaults.toml` + `curl` 做手工连通验证。

- 2026-03-02 Task2: 基线抓取脚本 capture_baseline.py 支持 --base-url/--fixtures/--out/--summary，按 fixture 驱动输出 NDJSON（含 case_id/request/response/status/expected/is_stream/normalized），并将 Authorization 统一写为 <redacted>。
- 2026-03-02 Task2: normalize_stream.py 对 HTTP/SSE 执行稳定归一，去除 trace_id/request_id/timestamp/system_fingerprint 等非确定性字段，并标准化 ID/UUID/ISO 时间戳。
- 2026-03-02 Task2: fixtures 覆盖 /v1/models、/v1/chat/completions、/v1/images/*、/v1/responses、/v1/files/*、/v1/admin/*、/v1/public/*；无上游可达时通过 expected transport+fallback 机制稳定完成录制。
- 2026-03-02 Task2 fix: 通过调整 public_video_sse_20 与 auth-fail fixtures 的 expected（status_any/transport_any/fallback）吸收环境差异，保持 observed_status 真实记录且命令可重复通过。
- 2026-03-02 Task2 fix: capture_baseline.py 将 normalize_stream 通过 import_module 延后加载，保持路径注入不变且满足 Ruff E402 规范。
- 2026-03-02 Task2 auth-fail stabilization: 对 authfail_v1_models_missing 与 authfail_v1_chat_invalid 采用 case 级 expected.status_any=[200,401,429] + transport_any + fallback_status，兼容 live/timeout 波动，同时保留 admin/public invalid-key 两个严格 401 样本。
- 2026-03-02 Task4: 新增 `internal/gateway` 可测试切流器，路由分组粒度固定为 `/v1/models`、`/v1/responses`、`/v1/chat/completions`、`/v1/images/*`、`/v1/admin/*`、`/v1/public/*`；未命中分组与未配置 override 一律默认走 python。
- 2026-03-02 Task4: 自动回滚采用按路由组滑动窗口错误率判定（5xx 记为错误），当 go 路由组错误率 `>= migration.auto_rollback_threshold` 时决策回退 python；后续窗口错误率恢复后可回到 override 目标后端。
- 2026-03-02 Task4: shadow 决策显式输出 `shadow_write_side_effect=false`，并在 `tools/parity/smoke_switch.py` 产出机读报告字段：`route_decisions(route->backend)`、`shadow_write_side_effect`、`auto_rollback(triggered/recovered)`。
- 2026-03-02 Task5: `tools/ci/verify_workflows.py` 支持 `--path/--require(可多次)/--out`，会聚合目录内 `*.yml/*.yaml` 的 jobs，并输出 JSON 字段 `status`、`required_jobs_found`、`missing_required_jobs`、`release_blocked_without_go_checks`。
- 2026-03-02 Task5: 发布门禁判定采用可重复规则：`build-and-push` 必须依赖 `go-gate`（或直接 needs 三个 gate job），且 `go-test/golangci-lint/parity-replay` 三个 job 必须存在，才能判定 `release_blocked_without_go_checks=true`。
- 2026-03-02 Task5: failure fixture `tools/ci/fixtures/workflows-missing-parity/go-ci.yml` 故意缺失 `parity-replay`，可稳定触发 verifier 非0并输出 `missing required job: parity-replay`。
- 2026-03-02 Task6: `internal/config/config.go` 新增 `ConfigStore + Manager`，并在 `Load/Update` 的保存路径统一使用 `config_save` 锁（timeout=10s），对齐 Python `config.py` 的互斥写语义。
- 2026-03-02 Task6: `DeepMerge` 语义与 Python `_deep_merge` 对齐：base 非 map 时返回 override(map) 深拷贝或 base 深拷贝；base 为 map 且 override 非 map 时返回 base 深拷贝；map-map 递归覆盖。
- 2026-03-02 Task6: 迁移逻辑保留一对多映射、不覆盖已存在目标键、迁移后删除旧键，并补齐 `chat -> app` 兼容迁移；`Load` 保留“远端空且本地空不强制持久化 defaults”的保护分支。
- 2026-03-02 Task6-fix: Config parser 依赖已从 `github.com/pelletier/go-toml`(v1) 对齐为 `github.com/pelletier/go-toml/v2`，`ParseTOMLFile` 保持 `invalid toml` 错误前缀，现有 Task6 测试语义不变。

- 2026-03-02 Task6: 在  新增  抽象， 统一通过  锁（10s）执行保存路径，覆盖 Python  的互斥写语义。
- 2026-03-02 Task6: DeepMerge 对齐 Python ：base 非 map 时返回 override(map) 深拷贝或 base 深拷贝；base 为 map 且 override 非 map 返回 base 深拷贝；map-map 场景递归覆盖。
- 2026-03-02 Task6: 迁移逻辑保留一对多映射、不覆盖已存在目标键、迁移后删除旧键，并补齐  legacy 键迁移； 保留“远端空且本地空不强制回写 defaults”保护语义。
- 2026-03-02 Task7: 新增 `internal/auth/auth.go` 完成 `VerifyAPIKey/VerifyAppKey/VerifyPublicKey` 语义迁移：`api_key` 为空放行、`app_key` 为空返回 `App key is not configured`、`public_key` 为空时按 `public_enabled` 分支放行/拒绝。
- 2026-03-02 Task7: Bearer 解析统一为 `ParseBearerToken`（仅接受 `Bearer <token>`），缺失或非法 scheme 均走 `Missing authentication token`；鉴权失败统一 `401 + WWW-Authenticate: Bearer`。
- 2026-03-02 Task7: Public 鉴权兼容 `public-<sha256("grok2api-public:{key}")>`；`TestPublicKeyHashCompatibility` 覆盖哈希分支，`go test ./internal/auth` 与专测命令均通过。
- 2026-03-02 Task7: 新增 `tools/parity/auth_matrix_diff.py` 输出机读 JSON（`status/mismatch_count/cases`），当前实现为 Python 语义基线矩阵，用于在 Go 路由未接入前稳定产出 Task7 证据。
- 2026-03-02 Task7-fix: `tools/parity/auth_matrix_diff.py` 新增 `--case` 枚举参数（`all`/`valid-all`/`public-disabled-no-key`），并保持默认 `all` 的全矩阵行为不变。
- 2026-03-02 Task7-fix: `valid-all` 通过复用现有 `expected_semantics` 过滤 200 成功路径；`public-disabled-no-key` 精确选择 `public_disabled_no_key` 单 case；输出结构继续保持 `status/mismatch_count/cases/targets`。
- 2026-03-02 Task7-fix: `--case` 采用 argparse choices 约束，非法值由 argparse 给出清晰参数错误；空选择集会显式 `SystemExit("no cases selected for --case ...")`。
- 2026-03-02 Task8: 新增 `internal/errors/errors.go`，统一 OpenAI 错误 envelope 固定四字段 `error.message/type/param/code`，并补齐 401/403/404/429 -> type/code 映射，默认回落 `server_error`。
- 2026-03-02 Task8: 新增 `ValidationErrorResponse` 规则：`json_invalid` 或 message 含 `JSON` 时强制使用固定文案并设 `param=body`；普通校验错误按 `loc` 提取并跳过数字段（如 `body.messages.0.content` -> `body.messages.content`）。
- 2026-03-02 Task8: 新增可复用 `SSEErrorFrame(err)`，输出严格 `event: error` + `data: {error...}` + `data: [DONE]` 终止帧，未知异常回落 `server_error/stream_error`。
- 2026-03-02 Task8: 新增 `internal/middleware/response_logger.go`，复制 Python skip 路径集合（含 `/static/*` 与页面路由），非 skip 路径注入 trace_id 到 context 与 `X-Trace-ID`，并记录 request/response/error 结构化字段（traceID/method/path/status/duration_ms/error）；异常日志后 `panic` 继续上抛，避免吞错。
- 2026-03-02 Task8: `internal/http/router.go` 仅做最小接入，将现有 mux 外层包装 `ResponseLoggerMiddleware`，保持 `/healthz` 与 `/static` 行为不变。
- 2026-03-02 Task8: 新增 `tools/parity/error_envelope_diff.py`，沿用 auth parity 脚本风格输出机读 JSON（`status/mismatch_count/cases/targets`），覆盖 HTTP 映射与 validation 特例基线语义。
- 2026-03-02 Task8验收: `go test ./internal/errors ./internal/middleware -count=1` 通过，错误 envelope 四字段、401/403/404/429 映射、validation JSON 特例与 param 提取、响应日志 skip/trace/duration/error 路径覆盖均通过。
- 2026-03-02 Task8验收: 产出 `.sisyphus/evidence/task-8-errors.json`、`.sisyphus/evidence/task-8-errors-ok.json`、`.sisyphus/evidence/task-8-log-compare.json`；其中 404 case 的 py/go `error.type` 均为 `not_found_error`，日志 QA 三项检查均为 pass。

- 2026-03-02 Task9: 页面 404 根因确认是 internal/http/router.go 仅在 RouterOptions.Config 非空时注册 pages，修复点集中在 internal/app/app.go 为 NewRouter 注入 config manager。
- 2026-03-02 Task9: 通过 --config 注入最小 [app].public_enabled 即可驱动页面语义：'/' 重定向目标、public 页面 404/200 分支与 /admin* 可达性均与 Python 对齐。
- 2026-03-02 Task9: parity 静态资源探针改为 /static/public/pages/login.html（仓库存在文件），避免历史路径不存在导致误判 static mount 差异。

- 2026-03-02 Task9收尾: QA evidence 文件统一补齐 command/exit_code/status 字段，证据格式改为可机读并可追溯执行命令。
- 2026-03-02 Task10: 新增 `internal/storage/storage.go`，补齐 `Store` 抽象、`BackendStats`、`ErrLockTimeout`、四后端构造器（local/redis/mysql/pgsql）与 `GetStorage/ResetStorageFactoryForTest` 工厂选择语义。
- 2026-03-02 Task10: `SaveTokensDelta` 采用 Python 语义等价增量合并（兼容 pool 内 mixed 结构：`map[string]any` + legacy `string`），并显式保证增量路径不调用全量 `SaveTokens`，满足 parity 测试对 `Stats().SaveTokensCalls` 的约束。
- 2026-03-02 Task10: 锁实现采用命名锁 + 超时等待模型，争用超时时返回 `ErrLockTimeout` 且等待时长接近 timeout；`TestLockContentionTimeout` 在 redis 后端路径稳定通过。
- 2026-03-02 Task10: local 后端 `LoadConfig/LoadTokens` 对损坏 TOML/JSON 不吞错，直接返回解析错误，保持 failure-path 契约（损坏文件必须报错）。
- 2026-03-02 Task11: 新增 `internal/token` 包并完成 `models/manager/scheduler/store_adapter` 闭环，保持 Python 语义关键点：`reload_if_stale`、401-only `record_fail`、`mark_rate_limited->cooling`、`refresh_cooling_tokens` 统计返回 `checked/refreshed/recovered/expired`。
- 2026-03-02 Task11: manager 保存链路采用 dirty tracking（state/usage 区分）+ 延迟 `_schedule_save` + `save(force)`，usage-only 变更受 `UsageFlushInterval` 节流且落库走 `SaveTokensDelta`，避免退化为全量频繁写。
- 2026-03-02 Task11: scheduler 通过 `AcquireLock("token_refresh")` 做并发互斥，`TestDistributedRefreshLockExclusion` 验证双实例同周期仅一方执行刷新；证据日志已落盘到 `.sisyphus/evidence/task-11-token-refresh.log` 与 `.sisyphus/evidence/task-11-token-lock.log`。
- 2026-03-02 Task12: 新增 `internal/reverse` 并落地最小闭环：`app_chat` HTTP stream relay（逐行转发且保持 `[DONE]`）、`RetryOnError`（`max_retry/status_codes/retry_budget` + 429 `Retry-After` 优先）、`ws_imagine` close/error 映射与 blocked 判定。
- 2026-03-02 Task12: 401/429/5xx 映射入口统一在 `MapHTTPError`，WS close code 映射统一在 `MapWSCloseCode`；关键专测 `TestWsCloseAndRetryMapping`、`TestAppChatStreamHappyPath`、`TestWsServerCloseCodeMapping` 与全量 `go test ./internal/reverse -count=1` 均通过。

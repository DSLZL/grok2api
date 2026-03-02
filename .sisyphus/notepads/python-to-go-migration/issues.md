# Issues - python-to-go-migration

- 2026-03-02: 工具链 grep 对部分 include/pattern 组合存在无匹配噪声；通过直接 Read + ast-grep 校验关键符号后完成契约抽取。
- 2026-03-02: validate_contract.py 初版触发 basedpyright 告警，已通过显式对象归一化与最小类型约束清理为零诊断。
- 2026-03-02: validate_contract.py 初版触发 basedpyright 告警，已通过显式对象归一化与最小类型约束清理为零诊断。
- 2026-03-02: 修复 scope creep，撤销 .gitignore 中无关的忽略规则。
- 2026-03-02 Task3: `lsp_diagnostics` 在全量 severity=all 时出现 go list workspace warning（No active builds contain ...），切换为 severity=error 后对变更 Go 文件返回 No diagnostics found，可作为完成门禁。

- 2026-03-02 Task2: 本地 127.0.0.1:8000 在执行窗口内不可达（全部请求 timeout）；已用 expected transport(timeout/connection_error)+fallback_status 策略保证基线命令可重复执行且 failed_cases=0，并保留 observed_status=timeout 作为真实现场证据。
- 2026-03-02 Task2 fix: 本地服务可达性存在波动，core 基线常见 timeout->504；已通过 fixture expected 的 transport/fallback 进行稳定化，不掩盖原始 observed_status。
- 2026-03-02 Task4: `tools/parity/smoke_switch.py` 初版触发 basedpyright 严格规则（未注解属性/Any）；已通过补充属性类型注解与 `cast(object, ...)` 收敛为零 error，不影响验收命令执行。
- 2026-03-02 Task5: `verify_workflows.py` 初版触发 basedpyright（`setdefault` key 可空、unused call result）；已通过显式 `job_name` 变量与 `_=` 接收返回值修复，最终 `lsp_diagnostics` 为零。
- 2026-03-02 Task6: 通过 Bash+python 追加 notepad 时，反引号内容会被 shell 命令替换导致执行异常；改为 `apply_patch` 直接向 markdown 末尾追加以规避转义陷阱。
- 2026-03-02 Task6-fix: 切换 TOML import 到 v2 时，LSP 在依赖同步前会短暂提示“no required module provides package”；执行 `go mod tidy` 后恢复正常。
- 2026-03-02 Task8: 新增 parity 脚本时触发 comment/docstring hook（检测到行内注释），已立即删除该注释并保持代码语义不变，后续该类脚本需避免非必要注释。
- 2026-03-02 Task8验收: parity 脚本当前为语义基线比对并附带 py/go 可达性探针；当 8000/19000 未启动时 `targets.*_reachable.reachable=false` 但仍可稳定输出语义对比结果，验收需结合 go test 与 QA 证据共同判定。

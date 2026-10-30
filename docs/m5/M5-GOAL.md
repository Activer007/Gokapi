# 当前 Goal：M5-FIX-01 — 删除生命周期与回归测试稳定性

2026-10-30，按用户最新指示收敛。当前唯一目标是完成 **FIX-01**，仓库仅为 `Activer007/SupaShare`。此前 M5 总体路线图仅作背景，不作为本轮自动续作范围。

## 目标与范围

定位并解决 API `TestDeleteFile` 的不稳定失败，证明延迟删除、共享 Blob、metadata 清理与回收符合现有安全合同。历史 Actions run 41032587092 只是调查起点；以当前 master、PR #59 精确 SHA、源码与实际测试证据为准。

1. 核对 master、分支/工作树、remote、Go 工具链、相关 PR 与 Actions 状态，保护未提交文件和其他工作树。
2. 审查 Api_test.go、deletion_timer/cleanup/deletion_state/blob_reclaim、SQLite/Redis metadata provider，以及 ADR-022/024/025。
3. 复现并区分测试时序、夹具、状态不一致与生产缺陷；用确定性 RED/GREEN 证明最小修复，不通过任意加长 Sleep 掩盖竞态。
4. 验证普通立即删除、deadline 前保留、到期最终完成、共享引用保留、缺失/删除失败/未知探测保留锚点、timer/恢复交错、重复清理不误退休，以及失败后重启可重试。
5. 优先修复测试同步/夹具。未经生产故障实证，不修改 runtime；保持锁序、失败重试、引用检查、API、权限、schema、metadata 和加密格式。
6. 记录基线、目标/非目标、根因、RED/GREEN、命令、PASS/FAIL/NOT_RUN、残余风险、受测代码与最终 PR head。

## 必须完成的验证

```bash
go test ./internal/webserver/api -tags=test,noaws,integration -run '^TestDeleteFile$' -count=30
go test -race ./internal/webserver/api ./internal/storage -tags=test,noaws,integration -run 'TestDeleteFile|TestSEC040|TestSEC041' -count=1
go test ./... -tags=test,noaws,integration -count=1
```

按实际 build tags 补齐受影响的 noaws、awsmock、SEC040/041 与真实 SDK/loopback 合同，不能把未编入的测试写成通过。精确命令和既有结果见 [FIX-01 报告](FIX-01-REPORT.md)与[验收检查表](M5-ACCEPTANCE.md)；未变化的受测源码可复用已核实证据，不重复无必要测试。

## 当前完成标准

FIX-01 的八项不变量、确定性 RED/GREEN、30次目标、相关 race/integration 回归均通过；没有未解释回归或兼容破坏；报告与独立 [PR #59](https://github.com/Activer007/SupaShare/pull/59) 的最终 SHA 可追溯，最新 head 自动 review 完成且无未解决问题。PR 的合入状态另行记录，不以审查通过冒充已合并。

**FIX-02、FIX-03、REL-01 及发布准备不属于当前 Goal。** 保留其他阶段已产生的分支、未提交改动与证据；不继续开发、测试、安装、备份/恢复或跨包推进。FIX-01 交付完成后报告本包结果，后续阶段等待用户明确指示；不将 FIX-01 完成写成全部 M5 完成。

GitHub Actions 保持关闭，不启动新 CI；历史 CI 不代表当前源码。不自动合并 PR，不发布 Release/tag/镜像，不部署或修改上游。继续复用 [M3 冻结范围](../security/M3-GOAL.md)、[M4 审计](../security/M4-COMPLETION-AUDIT.md)与相关 ADR，不重开延期范围。

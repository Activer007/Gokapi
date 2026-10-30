# M5-FIX-01：删除生命周期与回归测试稳定性

更新：2026-10-30。状态：本地验收 PASS；[PR #59](https://github.com/Activer007/SupaShare/pull/59) 的远端 review 以其最新 head 摘要为准，不自动合并。

此前 FIX-01 收口时，用户指示将当轮 Goal 收敛为 **仅 FIX-01**，其他阶段的历史工作不作为本包完成条件。该轮只更新范围文档，Go 代码及原受测输入闭包保持，文档 head 的自动 review 已单独核对。后续授权与当前状态以 [M5-GOAL](M5-GOAL.md) 和[任务看板](M5-TASK-BOARD.md)为准。

## G0 与源码基线

- 仓库：`https://github.com/Activer007/SupaShare`，仅操作 origin；未修改上游或其他工作树。
- 最新 master / 本包 baseline：`77b8fa2c729db05a5e2ae3434b50f02eda127def`。#56、#57、#58 均已合并，开始时无开放 PR；提交前再次查询 master 未变。
- 起始工作树干净，原分支 `codex/collection-short-links` / `6ec6a636e459eae4bbf9c2979631b9dbe099a57a`；新建 `codex/m5-fix-01`，保留所有其他 Codex 工作目录。
- 已读取 README、CONTRIBUTING、SECURITY、M3/M4 goal 与相关安全基线/审计，以及 [ADR-022](../security/ADR-022-RETRYABLE-DELETION.md)、[ADR-024](../security/ADR-024-DELETION-STATE.md)、[ADR-025](../security/ADR-025-OLD-CONTENT-RECLAMATION.md)，审查 timer/state/cleanup/reclaim 与双 provider 条件退休实现及测试。
- 用户要求 Actions 保持关闭：`actions/permissions.enabled=false`。该 master SHA 的 workflow run 数为 0，不能引用 #57 历史成功 CI 证明当前 master；本包未触发 CI。
- Windows 仅编辑与 Git；测试在 WSL Ubuntu 24.04 原生 Linux 文件系统、非 root uid 105、Go **1.26.10 linux/amd64 / CGO_ENABLED=1**。宿主 `/usr/bin/go` 是带自动工具链选择的旧 launcher；最终 runner 显式使用 `go env GOROOT` 对应 1.26.10 的 bin，再设置 `GOTOOLCHAIN=local`。
- 最终受测代码提交：`a122c0179c87c697132309eb6612efe99b1d1215`。原生候选的 1515 项输入生成前后 SHA256 核对通过，三个修改的 Go 文件与该提交一致；随后文档/证据变更不改变 Go 构建输入。

## 已确认问题、分类及边界

历史 [run 41032587092](https://github.com/Activer007/SupaShare/actions/runs/41032587092) 的真实输入是 `94ba10fd57feec60e463f63bd11e5f41d20cae46`，不是当前 master。job `123985626150` 的 integration 步骤记录 API `TestDeleteFile` 在 1.22s 失败，`Api_test.go:1151: got true, want false`。storage 包也有同名测试，不能混淆。

当前未修改基线的目标测试 **30/30 PASS，34.412s**；未稳定复现自然随机失败。已确认的测试缺陷是：调度一秒延迟后，以固定 200ms + 900ms 把 metadata 存在判作故障。deadline 只表示 callback 可执行，取得 metadata/blob 锁、checked I/O 和条件退休仍需时间；该 100ms 余量不是接口合同。旧测试还复用其他测试的 blob，不能单凭 metadata 退休证明末引用源已删除。

这属于测试同步/夹具缺陷。没有证据证明当前 timer 丢回调，也没有观测到历史 runner 当时具体卡在哪个锁/I/O；不把受控 RED 写成该次 CI 的完整调度回放。未确认新的生产逻辑缺陷，因此 production 代码不改。

## 确定性 RED → GREEN

仓库保留 [test-only injector](tools/inject-deletion-observer.py)，只复制到不存在的独立目标目录，不编辑输入 checkout。双方均调度真实 API、核实 live pending 后持有该文件的 metadata gate；安装满 1.1s 后，completion observer **先捕获 pending 快照再释放锁**，消除释放与读取的竞争。

| 同一故障与观察器 | 直接结果 |
| --- | --- |
| baseline 原单次断言 | RED / exit 1；1102ms 捕获 `found=true`，原 `want false` 失败 |
| 最终 checked bounded waiter | GREEN / exit 0；1100ms 同样捕获 `found=true`，释放后继续核对直到退休 |
| 双方 teardown | `M5_SETUP_OK` / `M5_OBSERVER_CAPTURE` / `M5_DRAIN_OK` 均出现；checked 引用/source 与 metadata 退休一致，未在关库后遗留 callback 的数据库操作 |

injector SHA256：`8051f9041adaaf959a58414f57d65f5b58f338b52a24b71995e142f3c461d142`。
完整小日志：[RED](evidence/fix-01/controlled-red.log)、[GREEN](evidence/fix-01/controlled-green.log)。此 RED 的对象是固定完成时间假设，不是安全漏洞。

在 Linux / Go 1.26.10 的候选 checkout 中，可用以下命令重建；`$m5case` 是新的临时目录，两个 `--dest` 均必须不存在。RED 预期 exit 1，GREEN 预期 exit 0，并须核对上述三个 marker，而非只看退出码。

```bash
repo=$PWD
m5case=$(mktemp -d)
mkdir "$m5case/baseline"
git archive 77b8fa2c729db05a5e2ae3434b50f02eda127def | tar -x -C "$m5case/baseline"
python3 "$repo/docs/m5/tools/inject-deletion-observer.py" \
  --source "$m5case/baseline" --dest "$m5case/red" \
  --evidence "$m5case/evidence" --mode red --run
python3 "$repo/docs/m5/tools/inject-deletion-observer.py" \
  --source "$m5case/baseline" --dest "$m5case/green" \
  --evidence "$m5case/evidence" --mode green \
  --green-api "$repo/internal/webserver/api/Api_test.go" --run
```

每个副本内部实际执行：`go test ./internal/webserver/api -tags=test,noaws -run '^TestDeleteFile$' -count=1 -timeout=30s -v`。JSON manifest 记录输入、注入后文件和工具链；不使用错误夹具前提或环境失败作为 RED。

## 最小修改

1. [API TestDeleteFile](../../internal/webserver/api/Api_test.go)：每次重建测试独占 blob 及三份共享 metadata；同步确认前两份共享退休和权限边界，再调度最后一份，避免定时器抢先退休改变同步 `shared` 预期。以 checked read + 10s 有界轮询替代两次固定 Sleep，读错不能算删除成功；完成后核实末引用源缺失，失败 teardown 显式清理/drain 并反馈错误。
2. [生命周期合同](../../internal/storage/security_deletion_lifecycle_test.go)：真实 current token 在精确 deadline 前执行以及 checked GC 均保留 pending metadata/source/hotlink，计划只改 PendingDeletion；真实零延迟 Schedule → callback 已进入 → Cancel 成功 → 放行旧 callback，完整 metadata/source/hotlink 保持，排空 deferred finish 后再还原 registry/关闭数据库。事件同步，不延长 Sleep。
3. [SDK 故障合同](../../internal/test/securityfixture/deletion_postdelete_probe_test.go)：真实 loopback HTTP/SDK，SQLite/miniredis 两组，初始 HEAD 正常 → DELETE 真正成功 → 确认 HEAD 才 503。必须返回错误且完成计数为 0；metadata/hotlink、逻辑计量与 ledger 保留。故障解除后按 Missing 退休，第三次 GC 无重复 DELETE/退休/退款。已物理删除的字节不能凭保留锚点恢复。

新增两类合同是覆盖补齐，基线实现已满足，不把它们声称为生产 bug 的 RED。独立静态审查发现的“先启动一秒 timer、再固定断言另一份为 shared”残留窗口已修复并复审，无剩余阻断发现。

## 运行命令与验收

全部在上述受测代码的隔离副本执行；[完整命令及 wall time](evidence/fix-01/results.tsv)。目标重复测试最终代码重跑 30 次；纯文档收尾不重复同代码矩阵。

| 命令 / 范围 | 结果 |
| --- | --- |
| `go test ./internal/webserver/api -tags=test,noaws,integration -run '^TestDeleteFile$' -count=30 -v` | PASS 30/30，31.615s |
| `go test -race ./internal/webserver/api ./internal/storage -tags=test,noaws,integration -run 'TestDeleteFile\|TestSEC040\|TestSEC041' -count=1` | PASS；API 2.504s / storage 4.281s |
| `go test ./... -tags=test,noaws,integration -count=1` | PASS；wall 36s，所有包通过 |
| storage 新生命周期两合同，`test,noaws,security_deletion`，`-count=10` | PASS 20/20，0.841s |
| 扩展相关 race：API/storage/filerequest/database/SQLite/Redis，`test,noaws,integration,security_deletion,security_cleanup,security_reclaim`，`TestDeleteFile\|TestSEC040\|TestSEC041` | PASS，wall 10s |
| API/storage/SQLite/Redis，`test,awsmock,security_deletion,security_cleanup`，`TestDeleteFile\|TestRestoreFile\|TestCleanUp\|TestSEC040\|TestSEC041` | PASS，wall 10s |
| `go test -race ./internal/test/securityfixture -tags=test,security_cleanup -run '^TestSEC040' -count=1 -timeout=180s -v` | PASS 28.420s；含 SDK unknown/failure/restart/shared/local missing/目录/链接/真实非 root 权限及新增双 provider 合同 |
| `go generate ./...` + tracked diff guard；全部受测输入 SHA256 复核 | PASS，生成资产零漂移 |

说明：要求的原始 race 命令不能编入所有 SEC040/041 存储合同，因为它们有额外 build tags；因此保留原命令，再追加上述扩展 tags/package 范围，未把未编入的测试写成已运行。真实 SDK 合同排除 `noaws/awsmock`，单独执行；未访问真实云服务。前 deadline 的“有效待删”指管理锚点及源保持，不改变既有 pending 文件的公开访问规则。

第一次完整 runner 误把 `GOTOOLCHAIN=local` 用在系统 1.22.2 launcher，go.mod 拒绝执行：**SETUP_FAIL / 测试未启动**。随后显式选择已安装 Go 1.26.10 再全组通过。首次日志复制还提示忽略日志子目录，最终按文件重新复制并复核；不影响测试退出结果。原环境失败保留在本地证据目录，不改写为 PASS。

八项不变量及对应合同见 [验收映射](M5-ACCEPTANCE.md)。相关 SQLite/Redis checked scan/CAS、未知 EXEC 保留、blob 锁序、发布 ledger 门禁、共享/重新引用与重启合同均沿用并重跑；不复制旧 M4 全量日志。

## 兼容、回滚与残余风险

只有测试、复现工具和文档变更。无 runtime/API、schema、metadata 格式、密钥/密文、历史 ID、权限或配置变更；回退该 PR 只回退测试及证据，不涉及数据库迁移。物理删除依旧不可用代码回滚撤销，须从一致备份恢复，沿用 ADR-022/024/025。

自然随机基线 30 次通过不证明历史 CI 的具体阻塞根因；有界等待超时将继续明确失败，不会无限等待或重试真实清理错误。queued callback 合同验证 Cancel 先完成的交错，不冒称穷尽所有调度；其他结果受既有状态冲突合同约束。

真实 Redis 持久化、云 S3、断电、安装升级/整站备份恢复/回滚演练、RC 与生产发布均未作为 **FIX-01** 执行，不能记为本包 PASS。本包无 UI 变更，不新增浏览器矩阵；API 通过真实 handler 请求，远端 I/O 通过真实 loopback HTTP 验证。FIX-02/03/REL-01 不属于当前 Goal，保留历史工作但不自动续作；本包完成不表示全部 M5 完成。

## PR 与审查

独立 [PR #59](https://github.com/Activer007/SupaShare/pull/59)，最终受测代码为 `a122c0179c87c697132309eb6612efe99b1d1215`；此后的提交只交付本包文档/复现工具/证据。最终 PR head 和自动 review 对应 SHA 可在该 PR 的 Codex Review Summary 核对，未取得前不得推定通过。最终检查结论与精确 head 另记录在 PR 交付 comment，不用自引用文档提交冒充代码证据。当前不自动合并、不发布、不部署；GitHub Actions 保持关闭。

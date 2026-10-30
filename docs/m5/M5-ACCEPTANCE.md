# M5 验收检查表

状态与命令以每包报告和精确代码提交为准；尚未执行不记 PASS。

## FIX-01

| 不变量 / Gate | 直接合同 | 当前状态 |
| --- | --- | --- |
| 普通立即删除；明确 shared/deleted 与错误反馈 | `TestSEC040FileStateAPI`、API/storage `TestDeleteFile` | PASS（本地） |
| 截止前 pending metadata、源与 hotlink 保留；不修改原下载授权语义 | `TestSEC040DeletionBeforeDeadline`，真实 current token 与 checked GC | PASS（本地） |
| 到期后确认 metadata 退休与末引用源消失 | API `TestDeleteFile`，checked bounded poll，测试独占共享 blob | PASS（本地） |
| 多引用/跨 owner/过期/pending/FR 内容保留 | `TestSEC040CleanupSharedAcrossOwners`、`TestSEC041ReclaimReferenceAndSameTarget`、`TestSEC041ReclaimSharedPoliciesAndFailure` | PASS（本地） |
| 缺失、DELETE 失败、初始/删除后 HEAD unknown 保守处理 | `TestSEC040CleanupLocalBoundaries`、`TestSEC040CleanupS3UncertainResults`、`TestSEC040CleanupPostDeleteProbeUnknown` | PASS（本地） |
| timer 与恢复交错不退休恢复后的分享 | `TestSEC040DeletionQueuedCallbackAfterRestore`、既有 stale token 和 registry 合同 | PASS（本地） |
| 重复删除/清理/陈旧快照不误退休、不重复退款 | provider SEC040/041、SEC041 重复 candidate、post-delete 第三次 GC | PASS（本地） |
| 真实 I/O 失败保留锚点，恢复/重启可重试 | `TestSEC040CleanupRestart`、Redis retirement retry、post-delete 双 provider 恢复闭环 | PASS（本地） |
| RED/GREEN、目标 30 次、要求的 race 与全仓 integration、相关 noaws/awsmock/安全测试 | [FIX-01 报告](FIX-01-REPORT.md) | PASS（本地） |
| 无 production/API/schema/权限/密文变更；生成资产无漂移；独立 review | 差异与生成 guard，[PR #59](https://github.com/Activer007/SupaShare/pull/59) 最新 head review | 本地 PASS；远端结果以 PR 对应 SHA 摘要为准 |

## FIX-02

| 不变量 / Gate | 直接证据 | 当前状态 |
| --- | --- | --- |
| 创建额度不依赖可见收集；空库、过滤后为空及孤儿行都正确 | `TestCollectionInitialLimits`：实际 view → template，SQLite/miniredis × 四种上限 × 四种列表 × 三种角色，共 96 组 | PASS（本地）；旧实现 18 组 RED |
| 普通用户正确获得每项上限；0 独立关闭该项；管理员/超管仍豁免 guest cap | 同一矩阵、首次表单 Node 合同、真实 Chrome 三角色 | PASS（本地） |
| 首次普通用户创建成功，刷新后可见，五天/一次默认值保持 | 真实页面空列表 → 新建 → API 200，7 文件/13MB 配置；刷新仍 1 行 | PASS（Chrome/loopback） |
| 不能通过省略、0 或超 guest cap 的新建值绕过限制 | `TestCollectionInitialPolicyAPI`，27 个真实 handler 用例，拒绝不新增 FR/key；成功保持 owner/凭证绑定 | PASS（本地） |
| 生成资源一致；相关回归/race/awsmock、全仓 noaws、默认/noaws build | [FIX-02 报告](FIX-02-REPORT.md) 与结果清单 | PASS（本地） |
| 独立 PR 与最新 head 自动 review | 报告中记录实际 PR，不借用 PR #59 的审查 | 待取得远端结果 |

## 后续工作包及总收口

FIX-03、REL-01 的实际验收尚未执行，均 NOT_RUN。后续每包开始时需建立自己的基线、RED/GREEN、测试命令、风险、PR 和最终 SHA；不得借 FIX-01/02 的结果完成它们。

最新 master 必要完整测试、安装/升级、实际备份恢复、回滚演练与 RC 清单目前均未取得 M5 收口证据。真实 Redis/S3 持久化、断电和生产环境也未从 miniredis/loopback/进程退出推定通过。发布仍需另行明确批准。

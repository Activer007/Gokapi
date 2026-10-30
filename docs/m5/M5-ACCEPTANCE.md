# M5 验收检查表

当前 Goal 仅为 FIX-01。状态与命令以本包报告和精确代码提交为准；尚未执行不记 PASS，其他阶段不作为当前完成条件。

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

## 当前收口边界

当前仅核对上表的 FIX-01 八项不变量、指定回归、报告及 PR #59 最新 head 的自动 review。FIX-02、FIX-03、REL-01 的历史分支/证据保留，但不继续执行，也不借用其结果证明 FIX-01。

真实 Redis/S3 持久化、断电、安装升级、整站备份恢复/回滚及发布候选准备均不属于本包验证范围；不从 miniredis/loopback/进程退出推定这些项目通过。FIX-01 交付完成后停止自动续作，后续阶段等待用户明确指示；本包通过不表示全部 M5 完成或已经合并。Actions 保持关闭，不自动合并、发布或部署。

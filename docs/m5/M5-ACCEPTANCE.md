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
| 无 production/API/schema/权限/密文变更；生成资产无漂移；独立 review | 差异与生成 guard，PR 最新 head review | 本地 PASS；远端 REVIEW_PENDING |

## 后续工作包及总收口

FIX-02、FIX-03、REL-01 的实际验收尚未执行，均 NOT_RUN。后续每包开始时需建立自己的基线、RED/GREEN、测试命令、风险、PR 和最终 SHA；不得借 FIX-01 的结果完成它们。

最新 master 必要完整测试、安装/升级、实际备份恢复、回滚演练与 RC 清单目前均未取得 M5 收口证据。真实 Redis/S3 持久化、断电和生产环境也未从 miniredis/loopback/进程退出推定通过。发布仍需另行明确批准。

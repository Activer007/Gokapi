# SEC-030 最小恢复：本地证据

实现基线c3058ed9b02727a78bf200eb550e28428ba152f5；2026-10-29，Windows/amd64，Go1.26.10，CGO=0。
只使用父测试拥有的临时SQLite/local副本及64KiB payload；每次crash/recover都是新进程、新CWD。

- [RED](red.log.txt)：空入口NEW_CONTRACT/STUB_RED，退出1/6.493s。A/B/C严格核对真实生产边界exit97后才验恢复；不称历史漏洞。
- [GREEN](green.log.txt)：相关命令退出0；三窗口、同长canonical损坏、whole异常状态、namespace/cleanup/runtime guard通过。
- [metadata读取](metadata.log.txt)：双provider小合同退出0，完整模型/missing/cancel/nil/坏gob通过；Redis为miniredis，不证明持久化。
- [同期身份迁移适配](identity-merge.log.txt)：PR36改变module/import后，新组合相关合同全部PASS；原输入CI40831437902成功但不能替代迁移后的最终CI。
- 命令及每包用时见[TEST-REPORT](../../TEST-REPORT.md)。Green后审查另补checked metadata读取、取消/whole scope/ScanComplete守卫；最终代码由PR CI核验。

A清理过期未Claim输入，B发布后未Commit保额/保文件/无metadata补造，C提交后先保留未知whole链接，
隔离副本离线备份并same-inode+SHA256确认移除私有work链接后，fresh恢复known input并释放stage，再fresh幂等。
共享canonical控制及metadata全量字段/内容保持；metadata只有一份upload新增。

日志只有固定测试消息、合成配置提示及结果，不含部署凭据/私密链接/数据路径。
本地race BLOCKED（无CGO/gcc）；Linux CI结果另见PR审查。断电、真实Redis/S3、生产、浏览器和新大文件矩阵NOT_RUN。

最终代码db4dc8a，[CI40833366371](https://github.com/Activer007/Gokapi/actions/runs/40833366371) SUCCESS，
实际候选a78f7be5（base32fe5c1），tree89276c43；三窗口race6.29s、negative2.78s。
[CI选取摘要](ci-summary.txt)仅含固定测试/结果/版本与合并输入，不是完整日志；完整命令见TEST-REPORT/PR35审查。
后续安全Markdown/文本不改安全代码、Go module和运行器；不重复同安全代码全仓CI。
同期PR37页脚2文件增量单独通过[五项模板合同](footer.log.txt)，全应用树包含此差异，不声称与旧CI整树完全相同。

复现新合同RED：在隔离checkout保留最终测试、错误值及Report类型，仅将RecoverUploads函数体临时替换成
`return RecoveryReport{Version: 1}, nil`，运行TEST-REPORT第一条命令；必须先核对crash子进程退出97，
随后出现RECOVERY_COMPLETE/RECOVERY_MANUAL断言。不要把编译失败、非97退出或超时标成RED。

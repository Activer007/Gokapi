# ADR-021：启动恢复只处理可证明的私有输入

状态：2026-10-29，SEC-030；实现基线c3058ed，范围以[M3-GOAL](M3-GOAL.md)为准。

## 安全不变量与最小失败合同

- 恢复先于常规GC、HTTP和新上传准入；单进程启动期间不与上传并发。
- 未Claim且过期的自有输入，或metadata与提交摘要一致、local明文/E2E canonical流式hash匹配的已提交分片输入，确认清理后才释放预留。
- Completing、发布不明、无法映射的whole工作文件、非法私有目录：保留文件与额度，停止启动；不补提交、不删canonical。
- 实际子进程在写入后、发布后提交前、提交后清理前退出97；新进程重新打开SQLite/local验证。退出代码或环境错误不能充当业务RED。
- 已提交文件内容SHA256和单条metadata保持；恢复重放不重复退款。诊断只包含哈希引用、固定原因码及计数。

新RecoverUploads入口先用空实现取得NEW_CONTRACT/STUB_RED，属于新恢复合同失败，不证明历史漏洞。

## 决策与边界

复用RecoveryBatch（每页128、ledger最多4096）、chunk session清理和receiver标记目录；根目录扫描有上限和deadline。
canonical核对沿用既有SHA1 locator，不改变去重协议、不将其声称为新的强抗碰撞设计；验收另用SHA256读回。
服务器加密locator不能直接核对ciphertext摘要，远端也不在本地证明范围，两者保留为manual，不自动清理/释放。
Items最多128条，Truncated可指诊断条目截断；ScanComplete表示扫描完整，根扫描超界则ScanComplete=false且阻止启动。
预检发现不确定状态先停止，不提前执行可能破坏人工核对现场的私有清理。
whole工作只有内存路径，无持久身份映射；即使Committed也不能凭文件名前缀判定归属。
因此提交后遗留whole硬链接需停机备份后人工确认，再让恢复器清理已确认的chunk输入。
不新增journal/schema、自动重放、canonical孤儿GC、管理员UI或配置开关。
恢复专用metadata读取返回error并接受context，固定字段有界读取；坏gob/连接错误进入固定manual诊断，不走旧getter的panic日志。

## 兼容、迁移与回滚

不改公开API、配额默认值、账本格式、加密格式或数据库schema。正常启动会提前校验现有私有命名空间。
不确定状态由此前可继续启动变为停止启动，需要人工处置；这是有意选择的可用性取舍。
回滚前停机并备份DB、DataDir和加密配置；代码可退回c3058ed，但旧启动路径会运行常规GC，
必须先离线处理不确定现场，不能用旧版本绕过恢复门禁。已确认删除的过期私有输入只能从备份找回。
SQLite/local进程崩溃验收不证明断电、Redis持久化或S3发布原子性。

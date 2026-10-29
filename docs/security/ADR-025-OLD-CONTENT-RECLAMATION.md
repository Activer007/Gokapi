# ADR-025：替换旧内容的持久回收候选

状态：ACCEPTED / LOCAL_VALIDATED / CI_PENDING，2026-10-30，SEC-041；基线65dd7ef6c7be7d1f08c00e41318b6da132aa89af。
面向单个Go进程与可信小团队；不改变分享权限、配额计量、加密格式或公开API。

## 安全不变量与RED

- 替换覆盖旧locator之前，同一事务/CAS必须登记其候选；登记失败不允许丢失旧锚点。
- 候选不是删除授权。拿对应blob发布锁后复读当前候选，再完整检查所有metadata引用，包含原target再次指回旧内容、跨owner、FR、过期和pending行。
- 检查不完整、损坏、超时、未知存在性或删除失败均保留候选；已引用内容不能被删除。
- 物理删除后再次确认缺失，才条件退休候选；未知COMMIT/EXEC不声称回滚或成功。
- 候选不计作分享文件，不修改逻辑配额或暂存账本，不产生第二次退款。

隔离65dd7ef完整归档上的正常重启合同已得到业务RED：真实替换成功、子进程正常退出，fresh SQLite和成功GC后旧内容仍在，新内容与两份metadata正确。
首次SETUP_FAIL因child未启用verbose而缺阶段标记；修正后才取得业务断言，不将环境失败算漏洞。
该合同不证明恶意可利用性或断电持久性；提交后骤停、双provider、共享/重新引用及loopback S3本地合同已通过，并发race及最终CI待运行。

## 最小持久结构

SQLite 16→17增加BlobReclaims表；Redis 9→10增加私有format marker和候选hash。
每项只含精确bucket/SHA1 locator与16字节随机token，不含原文件ID、owner、名称、密码、密钥或URL。
长度前缀二进制版本1，单项最多1173字节；最多4096项。Key是长度前缀locator元组的SHA256，仅用于记录去重，不是内容完整性协议。
每次新的替换意图刷新token；旧、新locator完全一致时不登记。满时拒绝需要新增锚点的替换，不覆盖或截断旧项。

SQLite在既有BEGIN IMMEDIATE中先登记再patch内容字段。Redis沿用WATCH/CAS，窄Lua先检查结构/容量，再登记候选、patch内容及可选来源过期；登记命令失败不得继续覆盖旧locator。
Redis运行期错误仍不能承诺全批回滚；错误/未知回复保留现场，后续按持久事实复读，不假装未提交。

## GC与并发

复用启动/周期CleanUpContext，每次候选pass最多128项、10秒，单项5秒；完整有界读取后以内存cursor轮转，避免单个坏项长期饿死后续项。
锁顺序仅blob→DB；不在blob锁内反取metadata锁，不在替换持source锁时再随意取old锁。
worker拿blob锁后复读Key与完整token；陈旧扫描不做物理I/O。完整引用查询不能复用旧HasBlobReference的排除自身ID语义。
同锁下复读quota ledger；存在Completing或尚未释放暂存的Committed时先保留候选和内容。ledger没有持久locator，不能把无metadata当作没有歧义发布；正常在途会暂缓此pass，未知发布沿M3人工恢复，不猜映射或自动退款。
完整GC交互合同曾实证：只保护候选pass，普通过期metadata pass仍可删除同一未知发布内容。因此同一门禁也接在普通GC最后物理删除之前；shared/missing退休流程保持。显式删除遇到这类在途状态可能返回原有pending/503，稍后重试。
确认存在其他引用时保留内容，条件退休旧候选，由metadata承接后续删除锚点；若该引用同时被替换，其同事务新token使旧ack失效。
无引用时复用040受约束的local Root/S3探测、删除、再次探测；只按登记locator处理，不扫描磁盘、mtime或文件名前缀猜归属。
实际引用发布沿已有blob门禁到metadata提交；这里只覆盖单进程，外部写者/第二进程不在合同内。

## 兼容、迁库与回滚

现有权限、MaxSize、分享计数、配额ledger、密文及页面/WASM不变。历史已丢失locator的孤儿不自动回填。
新库/旧库升级只初始化新结构；普通读取不把缺失或损坏的当前结构修成空。升级前停写、备份一致DB、DataDir、配置和密钥，并先在副本验证。
离线迁库在复制任何数据前检查完整候选；原样导入空目标，不因没有metadata而丢弃旧内容锚点。旧格式确实不存在该结构才视为空；部分结构失败。源库不变，失败目标隔离后重建再试。
回滚须停机并恢复升级前配套备份；旧二进制拒绝更高schema，禁止手动降低版本号继续写。已有新数据优先向前恢复，不能删除候选绕过问题。
物理删除无法靠代码回退撤销，只能从备份恢复；不清除备份或S3历史版本，不承诺安全擦除。

## 验收边界

必须证明提交后骤停+fresh reopen可回收、共享/重新引用保留、删除失败重试、陈旧token不可退休新意图、新引用发布与GC互斥、坏记录/登记失败拒绝。
开发期只跑相关合同，冻结后一次必要CI及相关race；真实Redis持久化/S3、断电、生产恢复保持NOT_RUN，不用miniredis或进程退出替代其结论。

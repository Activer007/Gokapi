# ADR-014：单进程的持久上传预约（SEC-030）

日期：2026-10-29；代码起点 `357670c16f5a0d78fe32b91efb39ee7bc74ae90c`。状态：数据库基础实现；最终测试/审查/合并以 TEST-REPORT/PR 为准。M3 完整目标继续有效，HTTP/CLI 上传尚未接入此基础层。

## 决定与适用范围

面向个人 VPS / 可信小团队，使用一份有界预约账本，已用逻辑量直接从现有 metadata 的 Id/UserId/SizeBytes/UploadRequestId 投影计算。避免维护第二套 used 计数及其回填/漂移问题。每条 metadata 分别计费，包含过期/待删和共享 blob；请求归属与当前所有者分别计。实例逻辑总量与暂存预留是不同维度；暂存数字仍是服务端声明，实际磁盘余量和处理放大将在入口接入阶段验证。

两 provider 共用 Go 的精确 int64 检查和纯状态转换：`used + reserved + requested <= limit`。SQLite 和 Redis 各自负责原子读改写，不建立新服务、权限引擎或通用分布式事务。用户确认的 MaxSize 单文件语义保持；请求累计字段及有限默认额度仍待后续 PR 接入。

| 实现 | 原子边界及失败处理 |
| --- | --- |
| SQLite schema 16（原15） | `UploadQuota` 单行 BLOB；专用连接 `BEGIN IMMEDIATE` 后，在同一连接读取账本、metadata 投影、写可选完成 metadata 与账本，再 COMMIT。错误回滚；请求取消不阻止独立限时回滚，无法确认连接状态时丢弃连接。 |
| Redis schema 9（原8） | 同连接先 WATCH 账本和 metadata revision，再读投影、Go 校验并用 MULTI/EXEC 写入；新 metadata 目标键也被 WATCH。冲突最多256次、1–10ms随机退避，Context可中断。原始 Save/Delete 用很小的 revision-first Lua 同步通知；配额不经过 Lua 浮点运算。 |

这替代 ADR-013 初稿的“Lua 计算额度”选择，继续复用既有 Redis。依据：[SQLite 事务](https://www.sqlite.org/lang_transaction.html)、[Redis WATCH/MULTI](https://redis.io/docs/latest/develop/using-commands/transactions/)。Redis 运行期命令错误不会回滚同批其他命令：实现先校验类型/数据并检查每个 EXEC 回复；错误明确返回。晚到 EXEC 结果可能不确定，调用者须用稳定 ID 查询/重试，不能把 Context 取消说成 Redis 已回滚。

## 有界状态与协议

- 账本版本1，最多4096条、每用户1024条、每请求256条，编码/读取最多4MiB，标识最多128字符。包含活动记录和终态回执；满时拒绝新增，已有取消/清理仍可执行。未知版本、重复/别名字段、损坏/负数/溢出拒绝，不重建成空账本。
- `reserved → uploading → committed`；清理确认后可变为 aborted/expired。最长预约24小时；终态回执至少保留24小时，只有已确认无暂存的终态才能在新预约时淘汰。过期活动记录仍占额度，不能靠 TTL 直接消失。
- 同 ID 绑定 owner/request 和首次声明。未声明长度与合法0字节明确区分；先占数量/在途槽，再 Bind 长度。原始未绑定 Reserve 在 Bind 后重放返回当前记录，不改变绑定。不同声明拒绝。
- 完成先 Claim 唯一最终文件 ID，同一上传重复 Claim 为 in-progress；不同预约不能 Claim 同一文件 ID。Commit 校验 owner/request/大小，并将 metadata 和状态一起提交。相同完成摘要重试不重复写 metadata、不重复计费，不覆盖后来修改/删除的 metadata；provider仍可更新账本和revision。
- committed 后逻辑量来自 metadata；物理暂存确认释放前仍占暂存和在途额度。`CleanupConfirmed` 是可信内部调用合同，只有 I/O 停止、暂存已清理后才能设置；数据库无法证明文件已删除。ReleaseStaging 不释放逻辑量。
- RecoveryBatch 以稳定 ID 排序，每批1–128条，返回活动或待清理记录。关闭重连保留预约；这里提供恢复所需状态，不执行启动扫描或物理删除。重启入口接入前还须处理已移动 blob 的目标登记、共享引用及删除失败，不能仅凭缺少 chunk 就释放额度。

幂等是有界窗口承诺，不是永久 exactly-once。客户端应为新上传生成新 UUID；回执淘汰后的旧 ID/迟到请求及区间完整性须在入口接入与重放回归中完成验证。

## 兼容、升级与回滚

仅添加账本表/键，不改 User/FileRequest/metadata 列或 MaxSize 旧值；现有 API、页面/WASM、本地/S3 调用仍走旧路径。新 API 是内部事务原语，尚未强制生产额度。metadata 的 Redis 变更通知增加一次原子 revision 写；连接失败返回 error，启动层仍拒绝失败连接，旧内部 panic 断言改为 error 断言，不取消服务器校验。

升级前停止两个端点的应用写入，保留配置/密钥/文件及一致数据库备份。SQLite 可使用原生备份机制或 [VACUUM INTO](https://www.sqlite.org/lang_vacuum.html)，不能随意只拷贝活跃 WAL 主文件；Redis 使用其已配置的 RDB/AOF 备份。先在隔离副本恢复、核对版本和记录，再升级。新库与旧库升级共用幂等初始化，不覆盖已有账本；新版库缺失/部分缺失状态拒绝，不能自动当作0。

离线迁库预检：原源数据有效，拒绝尚有活动/待清理预约的源；目标必须没有任何应用数据，包括 session/E2E/统计。对完整旧格式缺失的账本可按空预约处理，部分升级留下的状态仍必须检查。复制干净终态回执，保留源不变；失败的部分目标应隔离后重新准备空目标再试。既有迁移 CLI 以退出码3报告预检拒绝。该过程要求所有写入者已停止，不声称跨数据库原子迁移。

回滚使用升级前的配套数据库/配置备份；旧二进制会拒绝更高 schema，禁止手动降低版本号继续写。新数据产生后优先向前恢复，恢复旧快照会遗漏快照后的文件/记录。合成 SQLite 备份/升级/恢复、双 provider 复制与旧格式不升级源库有测试；真实 Redis 持久化、旧二进制和生产恢复 NOT_RUN。

## 验收与仍待完成

基础层测试覆盖独立连接10/50争最后字节/数量/在途/暂存、幂等/冲突/零字节、Claim/Commit、取消确认/过期保持、重连、满账本/分页、精确大整数和损坏数据；Redis 原始写入使 WATCH 重算、目标类型竞态及非致命 EXEC 错误有隔离回归。新 API 开发桩 RED 不称为现存漏洞。

M3 尚缺：普通/整文件/浏览器/File Request 接入；当前请求策略/累计默认值及迁移；复制/替换/转移等正向 metadata 变更的准入；实际 I/O 与取消的互斥、启动对账及物理清理；未知请求长度/恶意大小/重复与重叠块；实际1–2GB正确性、吞吐、内存/暂存峰值。现算 metadata 为 O(n)、全局 ledger 提交会串行化，适合小团队但仍需上述真实基准，不能把基础测试当作 M3 完成。

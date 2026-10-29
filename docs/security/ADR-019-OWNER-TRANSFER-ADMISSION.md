# ADR-019：单文件所有者转移的原子准入

状态：ACCEPTED，2026-10-29；SEC-030，基线c3244f008214a851404ecfcf5367e3c1878b58fa。
单进程、个人/可信小团队；只覆盖 `/api/files/changeOwner`，不扩大为批量迁移或文件替换。

安全不变量：

- 保留现有AdminOnly、API Edit及跨所有者编辑授权；数据库内复核原owner、源逻辑投影及目标用户仍存在。
- 异owner接收后，目标的已存逻辑bytes/files + 全部活动预约 + 当前文件不得超过用户上限。
- 实例及原File Request的总量不增加，不重复计费、不套公开请求的expiry/单文件门槛。
- 同owner是零增量幂等操作；允许从超额source转出，不要求未增长的instance/request先降至上限。
- 成功只patch UserId，保留当前blob/hotlink/request/encryption/下载计数及其他字段；失败不改任何元数据或账本。
- 不新增预约、active/staging、schema或物理文件操作；请求取消/截止时间传至事务。

防御性合同先在隔离基线执行RED：双provider各10个bytes/50个files竞争一个目标余额；
bytes案例含真实chunk预约竞争和已有预约，复用现有临时数据库/1B共享blob夹具。
核心及provider另以少量边界证明同owner、满instance/request、源变化、目标消失及只patch owner。
不因metadata变更重跑大文件/慢速或浏览器矩阵；最终代码仅一次必要CI，发生失败仅修复后补所需验证。

实现复用现有UploadQuota事务：SQLite BEGIN IMMEDIATE内条件UPDATE，Redis WATCH源/目标user/
ledger/revision后HSET UserId并递增revision；不通过全量SaveMetaData回写旧快照。
Redis任意EXEC基础设施故障不承诺回滚。目标用户删除后的完整批量迁移一致性仍需独立设计，
本轮只证明目标存在的事务边界；不能宣称所有owner转移/用户生命周期路径已完成。
其他全量metadata写者仍可能覆盖归属或参数；replace/bulk入口后续分别接入，不能以本入口的
patch-owner合同替代全仓并发一致性证明。当前核心、双provider整包及4组HTTP目标均已PASS，CI待最终head。

兼容与回滚：现有metadata无需迁移，File Request归属不变，E2E不迁移密钥；新增拒绝使用已有
413/21或409响应。同owner及移出超额owner仍可进行。若需回退，先限制此管理入口，再回退代码；
回退会恢复目标用户额度旁路，不通过删除文件/账本回退。无部署、无上游写入。

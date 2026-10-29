# ADR-018：复制元数据的原子准入

状态：ACCEPTED，2026-10-29；基线c657d547。仅SEC-030复制入口，不改schema、物理blob或UI；测试/CI状态见TEST-REPORT及关联PR。

复制一条metadata要为目标owner、实例及保留的UploadRequestId计一整份逻辑bytes/files，
共享hash不抵扣。与已有预约使用同一事务/CAS快照，不能先检查再在外部SaveMetaData。
拒绝不得留下metadata或hotlink。源的owner/request/size/blob/encryption必须在事务中仍匹配
授权快照，且未被删除、过期或耗尽下载次数；不将一次读权限快照延伸成无限后台任务。

实现复用ApplyUploadQuota的瞬时duplicate-metadata动作：不新增持久预约、active或staging占用。
SQLite沿用BEGIN IMMEDIATE；Redis沿用WATCH/MULTI并WATCH源key。hotlink只生成候选，与新
metadata一起提交。保留现有blob锁→数据库的锁顺序、普通API和管理员跨owner复制语义。
源投影读取有严格类型/长度边界，不读取Name/PasswordHash等无关字段；沿用现有gob加密格式。

保留原UploadRequestId。对已有请求的管理复制仍受当前MaxFiles及累计字节限制；允许既有
跨owner副本/转移文件再次复制，不要求当前source/目标owner等于parent owner。公开request expiry/per-file门槛控制
新的公开上传，不额外套到已授权的管理复制；不擅自清空request归属或改变MaxSize语义。
parent已删除或数值投影损坏时拒绝新增；source归属必须仍匹配授权快照。其他replace/transfer/bulk迁移仍单独推进。

目标ID冲突、源变化、quota拒绝均不发布hotlink；数据库错误使用固定错误响应。
不增加物理I/O、不重复1–2GiB性能测试。必要验证为双provider少量10/50配额竞争（含复制与
真实上传预约争同一余额）、共享blob、来源归属、已有预约及无部分发布；先隔离RED后定向GREEN。
同一代码只做一次必要CI，文档结果通过PR审查记录，不为补证据再重复全仓测试。
Redis WATCH竞争会重试，准入/源检查拒绝无写入；不把它扩大为Redis任意EXEC基础设施故障均能回滚的承诺。

兼容/回滚：默认5GiB/20GiB及已有数据不变，零schema迁移；新错误为413/21或409。
回滚该代码会恢复复制旁路，因此优先修复或临时限制复制入口，不删除已有文件/账本来回退。
本轮只操作隔离数据，无部署。完整M3的其他正向入口、物理容量协调与崩溃恢复继续OPEN。

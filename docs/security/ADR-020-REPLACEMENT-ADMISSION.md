# ADR-020：文件替换的原子增长准入

状态：ACCEPTED，2026-10-29；SEC-030，基线79f2577b3827140db8de65e90bfc41e8a17685ff。
只覆盖文件替换入口；单进程/个人及可信小团队，不新增schema、session或物理stage。

安全不变量与兼容：

- 保留Replace、来源Read和可选Delete权限；禁止FR作为target及任一端E2E，FR作为来源仍允许。
- 两份授权快照的owner/request/size/blob/encryption/live状态在事务中复核；来源文本有界比较。
- 仅正delta=max(source.SizeBytes-target.SizeBytes,0)计目标owner/instance bytes，含所有活动预约。
  files/request/active/staging不增加；相等/缩小允许已有超额。来源删除不预支退款，metadata实际回收前仍计费。
- 目标仅patch Name/Size/SHA1/ContentType/AwsBucket/SizeBytes/Encryption，保留当前owner、password、
  expiry、下载计数、request和hotlink映射，不回写旧整份File。
- deleteNewFile将来源ExpireAt=0/UnlimitedTime=false与替换同事务提交，之后才终止来源下载并触发既有GC。
  不再调用会重新读取并全量写入来源的DeleteFile；拒绝不得部分替换/过期来源。
- 窄兼容差异：同ID且deleteNewFile=true由旧隐式自删除改为409/19且零写；false仍幂等。

SQLite沿用BEGIN IMMEDIATE；Redis沿用WATCH/CAS并WATCH两端，只HSET所需字段。Redis任意EXEC
基础设施故障不承诺回滚。API先取target metadata锁；需删除来源时同时取source锁，按固定stripe
索引去重排序，避免不同ID碰撞自锁。顺序metadata→source blob→DB，旧target blob不额外锁或物理删除。
blob探测后刷新准入时间。旧blob孤儿回收（上游开放PR419）及其他全量metadata写者仍独立OPEN。

先以隔离79f运行时执行4容量竞争+self edge获得业务RED。不同来源blob避免锁串行化假证明，
保留两个同hash来源及共享旧target blob；仅双provider×user10/instance50，含既有hold与真实chunk竞争。
小型核心/provider/锁合同覆盖delta、overcap、当前字段保留、双端WATCH及原子来源过期。
不重复未变的1–2GiB流式/浏览器矩阵；最终代码一次必要CI，结果保留于关联PR审查。

迁移/回滚：metadata与ledger格式不变，无数据迁移或密钥迁移；新拒绝为413/21或409。
可选来源删除的物理回收沿既有流程，只在隔离副本验证；没有生产数据或部署。回滚代码前限制替换入口，
避免恢复准入旁路/自删除行为；已被GC删除的数据必须从备份恢复，不能靠回退代码恢复或删除账本抵扣。

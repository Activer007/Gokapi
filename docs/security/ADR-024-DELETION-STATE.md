# ADR-024：删除状态协调与兼容反馈

状态：ACCEPTED / CI_PENDING，2026-10-30；SEC-040-B，初始基线dee4382f，代码冻结1d25225。未通过最终CI，不表示可合并。

## 决策

沿用现有metadata与Unix秒PendingDeletion，不增加schema或通用任务表。
删除、安排、取消、密码哈希升级使用双provider完整快照CAS，仅更新指定字段；缺失记录不创建。
所有运行时metadata写者应遵守现有metadata stripe；GC与恢复共用metadata→blob→短DB事务顺序。
全局stripe通道在package init创建，避免首次测试调用把全局资源关联至某个synctest bubble；不改变索引或互斥语义。
源探测/物理删除在blob锁内、DB事务外；状态CAS不能撤回已经执行的物理删除。
Redis元数据退休仅对明确EXEC nil（WATCH中止）重试一次，最多两次；每次重读完整expected与hotlink，真实行冲突或未知EXEC回复不重试。
不重跑源探测/物理删除，不移动上传私有input清理的锁序；连续冲突仍保留锚点并反馈pending。

进程内使用最多16384项timer registry，区分最新token与精确deadline。取消/重排使旧token失效，
已排队/运行的回调仍占容量直到返回；回调取得metadata锁后再次核对token与当前身份，不能只在触发时检查。
数据库提交回复不确定时不报成功，并抑制旧实时timer；持久pending状态交checked GC复读。
重启后旧goroutine消失，既有GC按持久Unix秒意图恢复；不承诺重启后毫秒级延迟任务精度。

API成功删除保留HTTP200与空body，新增X-SupaShare-Deletion-Status：accepted/pending/deleted/shared。
accepted仅表示意图受理；pending配合503表示回收未完成；deleted表示分享已退休且源确认不存在；shared表示分享退休但共享源保留。
冲突409、未找到404、无权限401保持明确；对外错误固定脱敏。恢复仍返回原Result/FileInfo格式。
CLI保留400/401/404旧sentinel，其他非2xx返回固定HTTP状态错误，不把失败body当成功。

批量管理操作复用同一删除原语；逐项失败可以部分完成，但不能丢弃失败锚点或据此删除待重试的用户。
保留文件的owner转移复用既有准入事务，不能重放旧完整metadata或绕过预算。此处不引入跨全批次数据库事务。
File Request删除、保存与owner迁移共用独立的有界stripe域；删除先复读当前owner/APIkey，再把现有Expiry写为1并撤销上传key，随后才枚举文件。
上传Commit仍在provider事务内检查请求policy：过期前完成的提交被枚举覆盖，过期后拒绝新发布；失败保留过期请求供管理重试。
该域与文件metadata锁分开，允许删除请求时逐文件加锁；不新增schema。请求失败后显示为过期，原上传key不自动恢复。
用户删除先撤销已有session/key，逐文件失败保留user并反馈pending；与全部在途或新认证上传的原子隔离仍VALIDATE。

## 兼容、迁移与回滚

无schema、密文、locator或正常下载格式迁移。旧bool Go包装仍表示受理，不能用它证明物理删除成功。
错误路径新增409/503是纠正虚假成功；客户端应检查状态，延迟删除不能提前保证未来物理回收完成。

升级/回滚前停止写入，按现有runbook一致备份配置、数据库、blob及密钥；先保留不确定记录与源，再核对重试。
旧版本能读取现有字段，但重新暴露旧状态竞争；回滚不能恢复已经物理删除的内容，恢复需使用一致备份。
不得把删除成功解释为备份或S3历史版本已清除。

## Gate

两项私密候选在冻结1d25225上GREEN：timer保留原断言；恢复候选改为串行门禁结果判定，非原先强制成功前提的同夹具回放。
双provider字段保留/冲突/缺失不复活，旧timer、FR Claim/Commit及跨owner、API/CLI和既有兼容局部GREEN。
相关Linux race与最终完整CI仍OPEN，不借用040-A或UI PR45的PASS。

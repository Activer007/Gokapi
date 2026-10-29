# ADR-017：分片与 File Request 的持久准入

状态：IN_PROGRESS，2026-10-29。工作基线57e5116；未预报测试通过、PR或合并。
延续ADR-013—016；保留单个Go进程、SQLite/Redis、本地/S3、原客户端字段和响应结构。

## 本轮不变量

1. 普通API/浏览器与File Request使用同一持久会话。客户端UUID不变；内部以现有
   owner/request作用域生成稳定ID。Reserve/Bind固定总声明和暂存量，不能按每块重复占额。
2. File Request的MaxSize仍是单文件限制。新增严格正数环境配置
   GOKAPI_UPLOAD_REQUEST_TOTAL_MB（默认20480 MiB）及UPLOAD_REQUEST_ACTIVE（默认2），
   提供统一的每请求累计逻辑字节/在途上限，不新增数据库字段或管理UI。
   旧MaxFiles=0显式落到有限实例文件数上限；正数取旧上限与实例上限的较小值。
3. 当前request owner、MaxFiles、MaxSize及Expiry在provider事务/CAS内重读，
   Reserve/Bind/Claim/Commit不能只信任HTTP早先的展示快照。累计计量包含仍存的pending/expired
   metadata，保留原UploadRequestId归属；不是请求终身接收流量。Abort/Release不被新增容量拒绝挡住。
4. 真实接收长度受receiver约束；在分配持久payload之前原子预约总声明。
   同一上传的写盘、完成、取消和清理使用有限会话gate。正文仍可并行接收，合法非重叠和乱序块
   必须成功，不能靠拒绝并发来满足安全测试。
5. 预分配长度不证明完整。私有、有界的进度文件记录已Sync的范围及SHA256回执；
   完全相同的重试不改数据或重复收费，不同内容重试/部分重叠/总声明变化拒绝。
   不完整覆盖不能进入Claim和发布。至少支持现有最小块设置下的1–2GiB文件。
6. 完成先冻结会话、验证覆盖并持久Claim固定FileID，然后才可创建异步任务。
   活动任务受已预约的owner/request/instance额度约束；重复完成不重复创建任务或文件。
   同步使用请求Context，已接纳的异步处理须有有限期限。
7. 复用整文件准备/发布/原子Commit流程，但不创建第二个随机whole预约。
   已确认停止I/O、关闭并删除自有chunk/work/receiver资源后才还staging。
   发布/Commit不确定保额，不盲删canonical或用“文件不见了”充当清理确认。

## 兼容与期限选择

用户确认保留MaxSize单文件语义并另加累计限制，并于本轮明确选择更小的累计默认值：
每用户5GiB（5120 MiB）/1000文件/4在途，全实例20GiB（20480 MiB）/5000文件/8在途，
持久staging仍12GiB；取代PR21的20GiB/100GiB字节默认。File Request仍默认20GiB/2在途，
同时受owner5GiB和实例20GiB包围，不产生额外额度。所有旧0只取消对应旧层，不能取消有限包围预算。

只改变未显式设置时的默认值，不改写已有env配置或metadata；已有文件超额时仅拒绝新增，不删除数据。
需要保留旧容量的实例可显式设置原字节env并重启，不必回滚安全实现或迁移schema。
本轮尚未部署，其他具体新增请求限额仍是可配置工程选择，不冒称用户逐项批准。

新持久会话采用创建后绝对24小时期限；不沿用旧内存预约“4分钟未上传、每块再续23小时”的
无限滑动模式，也不增加续期原语。file-first慢首块接收不能因新的4分钟后台扫描被提前清理。
未绑定预约也受有限在途槽和绝对期限约束；失败/取消及到期仅在确认清理后释放。
File Request自己的Expiry仍生效，保留非0且小于当前Unix秒即过期的旧边界。

新进度文件位于独占、版本化的DataDir私有目录，避开旧chunk-/upload-前缀mtime盲删。
旧平面chunk没有可信范围证据，不按Stat长度导入或完成；升级前须停写并处理既有上传，
或保留备份后由客户端重传。schema仍为SQLite16/Redis9；新本地进度格式需要独立版本及损坏拒绝。
回滚须停写并核对数据库、DataDir及配置/密钥的配套快照，不能直接换回忽略活动账本的旧程序。

完成复用整文件处理边界：客户端E2E只按真实密文字节计量，RealSize仅显示；一份EncryptionInfo
不能表示必需服务器加密与客户端E2E同时存在，故该组合在Claim前拒绝，不能以客户端标志关闭
服务器策略。可选Content-Type继续按扩展名推导并回退application/octet-stream。
处理前空闲空间检查包含明文与实际密文长度、固定余量及MinFreeSpace，使用溢出检查；
它不是对其他进程或全部并发写者的原子磁盘分配器。共享receiver和持久staging另有有限预算。

停服先拒绝新任务并取消服务Context，HTTP与上传worker分别排空；票据覆盖日志和响应尾部。
总体30秒超时不视为I/O完成，不退款；进程以失败状态退出，跳过主动DB.Close及额外关闭DB写入，
避免仍活动的HTTP/上传worker与主动Close竞争。该排空不声称追踪全部历史定时任务。

## 验收与仍开放部分

先在隔离57e5116加合同取得真实RED，再接线。必须验证双provider普通/公开request
的10/50 bytes/files/active争抢、总声明而非单块收费、同ID重试与重连、共享blob、未知长度、
正常并行乱序/完整覆盖、同步/异步重复完成、写入与取消/清理交错、过期和策略编辑。
随后生成、四组全仓、相关race及当前1–2GiB真实HTTP读回/峰值Gate；不能删断言或取消服务端校验。

全M3继续包含其他metadata正向准入、完整PrepareTarget/发布→Commit崩溃对账、
真实Redis/S3及全部路径的大文件与慢速矩阵。本轮进度不等于这些最终Gate已经通过。

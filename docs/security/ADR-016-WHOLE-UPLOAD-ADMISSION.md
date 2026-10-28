# ADR-016：整文件 API 的持久配额准入

状态：IMPLEMENTED（本增量Linux Gate通过），2026-10-29。工作基线 `710f4bf32ae80ac110bb5cd24e8d92848ac22bd8`，已验证代码 `9db2542`，[PR #21](https://github.com/Activer007/Gokapi/pull/21)；最终文档head及合并另核实，没有发布或部署。适用个人 VPS / 可信小团队、单个 Go 进程及可信 DataDir，沿用 [ADR-013](ADR-013-UPLOAD-BUDGETS.md)、[ADR-014](ADR-014-DURABLE-UPLOAD-RESERVATIONS.md) 和 [ADR-015](ADR-015-BOUNDED-UPLOAD-RECEPTION.md)。

## 本轮范围与证据

本增量仅将整文件 `/api/files/add` 接到已有持久账本。源码路径为 `apiUploadFile → ProcessCompleteFile → NewFileReserved`；先由共用 receiver 有界接收完整 multipart，再以真实 file payload 长度预约，随后同步准备、发布和提交。兼容旧客户端的 file-first / fields-first、未知 Content-Length；本轮不要求新增客户端预约请求。

CONFIRMED_CODE：原子预约原语已存在，本增量已实施新接线、严格配置解析、错误映射和存储处理。隔离历史源码加防御合同得到真实断言 RED，当前代码的目标及Linux完整Gate已通过，结果分开记录于 [TEST-REPORT](TEST-REPORT.md)。这不是新的未知漏洞认定。

证据更正：旧SQLite并发夹具的Key缺少唯一PublicId，后写覆盖前写并导致401，不能用于证明配额争抢。修订夹具为每个Key生成PublicId并在竞争前回读确认；冻结基线重跑双provider bytes/files×10/50均超额接受，真实RED。当前相同API合同已PASS，存储含故障/锁取消/GC交错/active与staging拒绝及旧路径回归已PASS；新增blob加密metadata读取合同也曾真实失败后修正。完整全仓/race/大文件Gate仍单列于TEST-REPORT。

**范围限制：这不等于所有上传 API 已强制累计额度。** 普通/API/浏览器分片、File Request、复制/替换/所有者转移及删除用户时的文件转移仍有旧 metadata 写入入口；它们不因本轮整文件调用而自动获得准入。已有 metadata 都进入整文件准入的用量快照，但其他入口的正向增加仍可能超过这些额度。完整 M3 保持 OPEN。

## 配置合同及有限默认值

用户已确认 MaxSize/maxsize 保持每文件语义，并另增累计预算。本增量按个人 VPS / 可信小团队选择以下可配置的有限默认值；没有将这些具体数值记为用户逐项批准。配置及大文件兼容仍需通过本增量的实际 Gate。

| 环境变量 | 默认值 | 计量范围 |
| --- | --- | --- |
| `GOKAPI_UPLOAD_USER_TOTAL_MB` | 20480 MiB（20 GiB） | 当前 owner 的累计逻辑字节 |
| `GOKAPI_UPLOAD_USER_MAX_FILES` | 1000 | 当前 owner 的 metadata 文件数 |
| `GOKAPI_UPLOAD_USER_ACTIVE` | 4 | owner 持久活动/待清理预约，跨其所有 Key 共用 |
| `GOKAPI_UPLOAD_INSTANCE_TOTAL_MB` | 102400 MiB（100 GiB） | 实例累计逻辑字节 |
| `GOKAPI_UPLOAD_INSTANCE_MAX_FILES` | 5000 | 实例 metadata 文件数 |
| `GOKAPI_UPLOAD_INSTANCE_ACTIVE` | 8 | 实例持久活动/待清理预约 |
| `GOKAPI_UPLOAD_STAGING_MB` | 12288 MiB（12 GiB） | 账本中尚未确认释放的暂存预留 |

所有新容量是字面正整数；显式空值、0、负数、非法数值和 MiB 转字节溢出均应失败关闭，错误不回显所给值。它们不重解释旧 guest/MaxSize/MinFreeSpace 的0语义。配置重启生效，不新增 User/FileRequest 的数据库字段。

逻辑用量沿用 Measure：每条 metadata 按当前 UserId 计 SizeBytes/文件数，包含过期、待删及共享 blob。去重不能抵扣第二条 metadata；E2E 的 RealSize 只展示。新版本可能因旧 metadata 已超过有限上限而拒绝新整文件上传；不删除或重写旧文件。身份缺失、负数或溢出等异常数据应阻止准入，不能忽略后按零用量接收，须先在隔离备份中检查。

每次持久暂存声明为 `4 * 实际 file payload 字节 + 64KiB`，检查 int64 溢出。它是覆盖接收、明文/密文工作副本的保守声明，不是磁盘实测值。owner持久在途限制从接收后开始；receiver 另有默认12 GiB的独立接收预算及默认4正文槽，两层预算可能保守重复占额，不能把其中一层余额当作另一层可用容量。MIN_FREE_SPACE仍在接收层检查，不由实例逻辑总量推导实际磁盘空闲。

这些容量同时生效，因此低于既有单文件MaxSize的文件也可能因剩余逻辑/暂存额度被拒绝。例如12GiB暂存候选值按上述公式不能承载完整3GiB payload；旧单文件配置并不是忽略累计或暂存预算的保证。1–2GiB兼容须在本轮代码上重新验收。

## 成功与失败状态

1. 验证当前认证 owner、配置和实际接收长度；整文件路径强制空 FileRequestId。创建仅属本次服务端调用的随机内部上传 ID，原子 Reserve，并 Claim 唯一 metadata ID。
2. 同步流式准备工作文件，核对实读长度、Context及加密状态；处理发生在可信 DataDir 的0600自有工作文件中，不整文件入堆。客户端 E2E 与必需服务器加密的混合声明拒绝，不能以客户端标记取消服务器加密。
3. 按 bucket/hash 的有限单进程锁协调发布与引用变更。已有 blob须验证类型/长度和可用加密 metadata；未知服务器密文不覆盖、不试删，也不猜测密钥。新本地 blob使用不覆盖既有目标的硬链接；不支持该文件系统操作时返回错误，不降级成覆盖写。S3 使用带 Context 的 SDK 上传/HEAD。
4. metadata和完成回执在同一 provider 原子操作提交；hotlink候选先不独立写库，映射加入同一次 Commit。SQLite使用同连接事务；Redis WATCH目标及hotlink并在同批 MULTI/EXEC写入、逐回复检查。Redis运行期错误仍不能解释为整批自动回滚。
5. I/O停止且自有工作文件、接收 spool关闭/删除均确认后，才 ReleaseStaging。Close或删除结果不确定时保留暂存额度；元数据已提交不能因此被删除。

发布之前明确失败且清理成功可 Abort。开始发布之后、Commit调用之后或收到不确定提交结果时，保留活动/待清理状态和预留；不能无条件 `defer Abort`、按超时退款或删除 canonical/shared blob。即使物理 work/spool已清理，也不足以证明未知的目标发布没有发生。保留资源可能造成后续拒绝，这是待对账状态，不应通过清空账本恢复容量。

内部稳定 ID只关联本次调用的各步骤和持久回执；HTTP重试会产生新 ID。本轮**没有跨 HTTP 请求的幂等键协议**，不声称 exactly-once。超时或5xx不证明提交未发生，客户端不能把盲目重发等同于安全回滚；结果查询仍受现有授权限制。

永久逻辑字节/文件数拒绝采用 HTTP413 / ErrorCode21。持久在途额度拒绝与 receiver429/20区分，暂存不足独立返回容量错误；新存储路径对外只返回结构化通用错误，不输出 provider异常、路径、内部ID或加密材料。完整状态码合同以源码及当前测试为准，不修改前端或公开 API字段结构。

## 兼容与回滚

SQLite schema16、Redis schema9及账本版本不变；无新增数据库字段、密码格式或密文格式迁移。hotlink沿用原映射结构，仅将发布时序纳入 Commit。共享 blob按每条 metadata计逻辑量，保留现有本地/S3驱动和页面/WASM/CLI；普通分片及 File Request 的每文件 MaxSize防线继续保留，新请求累计字段尚未实施。

升级、回滚前须停写并保留一致的数据库、DataDir、配置及密钥备份，在隔离副本验证。没有 schema变化不意味着可以直接换回旧程序：旧代码不理解新产生的活动 charge或发布后保持状态，会绕过预约并可能运行旧清理流程。先对账在途/已提交记录和目标，优先向前修复；必要时恢复配套快照，明确快照后的新文件/metadata可能丢失。禁止手动降低 schema、清空账本或为了释放容量试删 canonical文件。

## 仍开放的验收与下一步

- 本增量Linux10/50并发、共享blob、故障保持、hotlink事务、相关race及四组全仓已通过；Windows全仓失败/环境限制仍单列，不能扩大Linux结果。
- 旧超额/异常 metadata 的运维提示与隔离升级/回滚验收。
- 分片及 File Request reserve/首块绑定/完整区间/取消/同步与异步完成；请求新增累计字段、有限默认值及旧请求兼容。
- 所有 metadata正向增加的原子准入，及实际删除/共享引用的可靠恢复。
- 启动停止准入时对账 ledger、work/spool、已发布 canonical/S3目标；当前账本不持久化完整目标清单，缺少 chunk不能作为退款证据。关闭重连测试不等于进程崩溃恢复。
- 本轮Linux runner的SQLite/本地存储路径1GiB明文/2GiB服务器加密HTTP读回及采样峰值已通过；真实Redis持久化、真实S3及完整多路径性能仍OPEN。单进程锁不提供跨实例或外部S3写入者的协调。

# M3 上传语义调查（SEC-031）

调查日期 2026-10-28，工作基线 `e38b03f44a183db93c7df929f87e614e21244137`。个人 VPS / 单 Go 进程 / 可信小团队，公开上传者不可信。当前无部署；本轮只在源码临时副本和合成数据中操作。以下区分实测现状、建议合同和尚未完成的资源防护，不能将文档修正当作 M3 完成。

## 当前事实

| 边界 | 源码证据 | 分类 / 语义 |
| --- | --- | --- |
| 实例单文件 | `Environment.go` MaxFileSize、`Setup.go`、`storage.isAllowedFileSize` | CONFIRMED_CODE：`GOKAPI_MAX_FILESIZE` 默认 102400 MiB，持久化 `MaxFileSizeMB`。不是用户累计额度。 |
| File Request MaxSize | [模型](../../internal/models/FileRequest.go)、[API](../../internal/webserver/api/Api.go) 的 apiChunkUploadRequestAdd | CONFIRMED_CODE：每文件 MiB，取 server/request/非管理员 guest 环境限制的最小值；0 仅取消该层。管理员绕过 guest 环境层，仍受 server/request 限制。 |
| guest 配置 | [环境](../../internal/environment/Environment.go)、[advanced](../advanced.rst) | CONFIRMED_CODE：默认最多 100 文件、每文件 10240 MiB。`usage.rst` 原来声称整请求总额，与实现/模型/advanced 不符；本轮修正文档。 |
| 请求累计展示 | FileRequest.Populate / TotalFileSize | CONFIRMED_CODE：累计 metadata.SizeBytes，跳过 pending deletion，用于展示；上传入口没有将它当作总字节准入。未来新配额不能复用这个展示字段作为可靠账本。 |
| 文件数与预约 | apiChunkReserve、FilesRemaining、[ChunkReservation.go](../../internal/storage/chunking/chunkreservation/ChunkReservation.go) | CONFIRMED_CODE：已完成数量和内存预约数分别读取；新预约另行加锁。4 分钟未上传期限，上传后续至 23 小时，5 分钟清理 map。并发最后一额与重启恢复仍 VALIDATE，顺序测试不能证明原子性。 |
| 磁盘余量 | Chunking.isEnoughSpace / allocateFile | CONFIRMED_CODE：新 chunk 的瞬时 `free >= declared + GOKAPI_MIN_FREE_SPACE`，默认400 MiB；已有 chunk 提前返回。不是跨上传的物理预约，也不包含单独 TMPDIR 的预算。 |
| 请求体与内存 | API processNewChunk/apiUploadFile、Webserver.uploadChunk、[FileUpload.go](../../internal/webserver/fileupload/FileUpload.go) | CONFIRMED_CODE：MaxBytesReader 限整个 multipart body，不是仅 file part；ParseMultipartForm 在预约/声明验证前按 MaxMemory 阈值缓存/落盘。MaxMemory 默认50 MiB为每请求阈值，MaxParallelUploads 默认3为客户端并行数，都不是实例总并发额度。 |
| 未知长度 | isChunkMinChunkSize | CONFIRMED_CODE：依赖 ContentLength 判断最小块，-1 的正常分片当前被拒绝。整文件 API 不经过该检查，需单独测。 |
| 暂存 | Environment TMPDIR、FileServing.cleanOldTempFiles、`cmd/gokapi/Main.go` | CONFIRMED_CODE：multipart 走 OS temp；Docker 未指定 TMPDIR 时使用 DataDir。chunk/加密临时文件在 DataDir；启动和每小时清理 mtime 超24小时的 `upload*`/`chunk-*`，与预约过期不是同一生命周期。 |
| 去重/加密 | FileServing.createNewMetaData/getChunkFileHash/hashFile/encryptChunkFile | CONFIRMED_CODE：普通 SHA1 内容指纹去重；服务端加密加入现有 salt，E2E 用随机 blob 名称。SizeBytes 是上传长度，E2E RealSize 只影响展示字符串。加密处理可能同时保留原 chunk 和新密文。共享 blob 不合并用户逻辑所有权。 |

## 隔离实测与不变量

[TestSEC031UploadSizeSemantics](../../internal/webserver/api/security_upload_semantics_test.go) 通过真实 API 解析/认证和 SQLite/miniredis，在独立子进程、临时数据库和存储驱动执行。每个请求使用随机合成 Key/ID，不打印这些值。测试保持现有大小、预约、权限和磁盘校验。

- MaxSize=1 MiB、MaxFiles=2：两个独立 UUID 各上传/完成768 KiB，metadata 共2个、1572864字节，逐 blob 校验合成内容；第三次预约因数量上限拒绝。
- 超过单文件上限的 body、恰好1 MiB payload 加 multipart 开销、未知长度分片均拒绝，且没有创建 chunk。
- 合成超大最低空闲阈值使3字节上传拒绝，未通过填满实际磁盘制造条件；过期请求不能预约。以上只证明对应顺序行为。
- 本测试有意记录当前未知长度/协议开销限制；以后改善这些行为时要一起更新合同与兼容测试，不能临时取消 MaxBytesReader 等服务端校验。

最初运行 `cosvj9` 的生成通过、语义测试失败，原因是夹具更换 DataDir 后未重建 filesystem 驱动，完成文件落入隔离旧路径；不是生产漏洞 RED。修正夹具后 `4xtxNX` 目标通过；新增磁盘/过期控制后的最终结果见 TEST-REPORT。原始失败证据保存在忽略目录，不覆盖。

## 产品契约与阶段划分

用户于2026-10-28确认：保留现有 `MaxSize/maxsize` 的单文件语义，新增名称明确的累计总字节限制，不重新解释持久化旧值。本轮未实施 schema/准入迁移；当前文档只纠正现状，新增累计额度尚未生效。后续设计见 [ADR-013](ADR-013-UPLOAD-BUDGETS.md)。

M3 完整目标继续有效：用户逻辑额度与实例物理容量分开；请求文件数/单文件/累计字节/在途额度均有限；稳定上传 ID 幂等，provider 原子检验 used+reserved+requested；SQLite/Redis各自实现；取消/超时/失败/重启释放或恢复；兼容普通/公开/API/CLI，维持1–2GB流式上传。

逐 PR 顺序：SEC-031 语义和现状测试 → SEC-030 纯计算 → SEC-030 预约事务与恢复 → 普通/File Request 接入 → SEC-032 统一异常流。各 PR 单一 SEC-ID，后续账本、入口和临时清理互为验收依赖，不能仅合并计算函数即宣称防护完成。

仍缺证据：10/50 并发抢1额、恶意声明/大 offset/重复块/重放、整文件未知长度、客户端断线、加密和共享 blob 配额、进程重启、正确性/吞吐/暂存峰值以及真实1–2GB流式测试。这些 Gate 继续开放，详见任务看板。

## 上游只读复核

2026-10-28 核实 upstream master 仍为 `a9c25d453aab0c25f2d6e6926ec7312628638844`。[#431](https://github.com/Forceu/Gokapi/issues/431) 和 [#54](https://github.com/Forceu/Gokapi/issues/54) 为开放配额需求；已读相关评论。[#436](https://github.com/Forceu/Gokapi/pull/436) head `98e43fd1ca4173795f271c9d4a34683cb6237edb` 未合并，仅将页面默认 guest 限额赋值移出列表循环，不是服务器预算。[#419](https://github.com/Forceu/Gokapi/pull/419) 是替换旧 blob 回收，归SEC-041。检索全部quota PR和开放upload/chunk/size/disk/memory PR，未见可直接复用、覆盖本阶段原子预算的修复；这不证明所有历史分支均无实现。

[GHSA-45vh-rpc8-hxpp / CVE-2026-30961](https://github.com/Forceu/Gokapi/security/advisories/GHSA-45vh-rpc8-hxpp) 是 <=2.2.3 的分片单文件 MaxSize 绕过，2.2.4已修，分类 HISTORICAL_FIXED；不能改名为累计额度新问题。本轮没有向上游写入或发布安全公告。

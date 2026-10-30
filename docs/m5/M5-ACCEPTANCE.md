# M5 验收检查表

保留各包已完成的验证与原始受测提交；FIX-01/02/03 已合并，本轮仅补 REL-01 Lite 安装与恢复路径，不重复运行未受影响的完整测试。

## FIX-01

| 不变量 / Gate | 直接合同 | 当前状态 |
| --- | --- | --- |
| 普通立即删除；明确 shared/deleted 与错误反馈 | `TestSEC040FileStateAPI`、API/storage `TestDeleteFile` | PASS（本地） |
| 截止前 pending metadata、源与 hotlink 保留；不修改原下载授权语义 | `TestSEC040DeletionBeforeDeadline`，真实 current token 与 checked GC | PASS（本地） |
| 到期后确认 metadata 退休与末引用源消失 | API `TestDeleteFile`，checked bounded poll，测试独占共享 blob | PASS（本地） |
| 多引用/跨 owner/过期/pending/FR 内容保留 | `TestSEC040CleanupSharedAcrossOwners`、`TestSEC041ReclaimReferenceAndSameTarget`、`TestSEC041ReclaimSharedPoliciesAndFailure` | PASS（本地） |
| 缺失、DELETE 失败、初始/删除后 HEAD unknown 保守处理 | `TestSEC040CleanupLocalBoundaries`、`TestSEC040CleanupS3UncertainResults`、`TestSEC040CleanupPostDeleteProbeUnknown` | PASS（本地） |
| timer 与恢复交错不退休恢复后的分享 | `TestSEC040DeletionQueuedCallbackAfterRestore`、既有 stale token 和 registry 合同 | PASS（本地） |
| 重复删除/清理/陈旧快照不误退休、不重复退款 | provider SEC040/041、SEC041 重复 candidate、post-delete 第三次 GC | PASS（本地） |
| 真实 I/O 失败保留锚点，恢复/重启可重试 | `TestSEC040CleanupRestart`、Redis retirement retry、post-delete 双 provider 恢复闭环 | PASS（本地） |
| RED/GREEN、目标 30 次、要求的 race 与全仓 integration、相关 noaws/awsmock/安全测试 | [FIX-01 报告](FIX-01-REPORT.md) | PASS（本地） |
| 无 production/API/schema/权限/密文变更；生成资产无漂移；独立 review | 差异与生成 guard，[PR #59](https://github.com/Activer007/SupaShare/pull/59) 最新 head review | PASS；PR #59 已合并 |

## FIX-02

| 不变量 / Gate | 直接证据 | 当前状态 |
| --- | --- | --- |
| 创建额度不依赖可见收集；空库、过滤后为空及孤儿行都正确 | `TestCollectionInitialLimits`：实际 view → template，SQLite/miniredis × 四种上限 × 四种列表 × 三种角色，共 96 组 | PASS（本地）；旧实现 18 组 RED |
| 普通用户正确获得每项上限；0 独立关闭该项；管理员/超管仍豁免 guest cap | 同一矩阵、首次表单 Node 合同、真实 Chrome 三角色 | PASS（本地） |
| 首次普通用户创建成功，刷新后可见，五天/一次默认值保持 | Node 默认值合同验证五天/一次；Chrome 空列表 → 新建 → API 200，7 文件/13MB、一次；刷新仍 1 行 | PASS（Node + Chrome/loopback） |
| 不能通过省略、0 或超 guest cap 的新建值绕过限制 | `TestCollectionInitialPolicyAPI`，27 个真实 handler 用例，拒绝不新增 FR/key；成功保持 owner/凭证绑定 | PASS（本地） |
| 生成资源一致；相关回归/race/awsmock、全仓 noaws、默认/noaws build | [FIX-02 报告](FIX-02-REPORT.md) 与结果清单 | PASS（本地） |
| 独立 PR 与最新 head 自动 review | [PR #60](https://github.com/Activer007/SupaShare/pull/60)，不借用 PR #59 的审查 | 以该 PR 最新 head 的实际摘要为准 |

## FIX-03

| 不变量 / Gate | 直接证据 | 当前状态 |
| --- | --- | --- |
| 本地源缺失、raw/E2E 已知长度错误在附件前失败；读取/写入/认证失败产生真实传输错误 | `TestFIX03LocalDownloadTerminal` 与 SEC052 合同 | PASS（本地/race/相关回归） |
| S3 上游状态/长度校验，代理/强制解密/ZIP 的后置错误不正常完成 | `TestM5SDKDownload*`、`TestFIX03RemoteDownloadTerminal`；实际 SDK → loopback origin → net/http client | PASS（目标/race） |
| ZIP 成员失败不补封档、不提前完成先前成员；最终传输失败中断并保留源/metadata | 本地与远端双成员 archive 故障、writer/flush、加密已知长度负例 | PASS（目标/race） |
| 真实客户端取消向 S3 传播；健康空文件/Range/HEAD/304/416、E2E 单文件边界保持 | 终态合同对照及 StreamContext；E2E ZIP 仍拒绝 | PASS（目标/race） |
| 版本源、API、页脚、Swagger、README、changelog 与生成资产同步为 2.13.1/21301 | 生成 guard、CLI、真实 Chrome/API/Swagger 与下载正负例 | PASS；源码更新，未发布 |
| 完整回归、默认/noaws 构建、精确 SHA、独立 PR 与最新 head review | [FIX-03 报告](FIX-03-REPORT.md) | 本地 PASS；远端结果以 PR 对应最新 SHA 摘要为准 |

## REL-01 Lite（2026-10-30～31）

受测源码：2.12.0 / API 21200 为 `94ba10fd57feec60e463f63bd11e5f41d20cae46`，当前 2.13.1 / API 21301 为 `54e65f45b63978823c9eee8155dc19d81ba51442`。两者由源码归档在 WSL Ubuntu 24.04 / Go 1.26.10 运行 `go generate ./...`、`CGO_ENABLED=0 go build ./cmd/supashare`；均使用新建合成目录，未修改真实数据。

| 路径 | 实际验证 | 结果 |
| --- | --- | --- |
| Docker Compose 全新安装及初始化 | Docker 29.1.3 / Compose 2.40.3；独立项目和全新 config/data 挂载，回环端口 58141；setup 初始化、CSRF 登录、上传、匿名分享下载及收集专用分片接口完成后管理下载，SHA-256 一致 | PASS |
| 原生 SQLite＋本地全新安装 | 私有 setup 凭据初始化、CSRF 登录、普通上传、匿名分享下载 SHA-256、专用分片接口完成文件收集及管理下载 SHA-256 | PASS（原生，不代表 Compose） |
| 重启后数据保持 | 同一数据目录正常停止/启动，登录、文件 hash、用户及收集链接/设置复核 | PASS（原生） |
| Compose 重启后数据保持 | `docker compose restart` 后再次核对登录、普通/收集文件 hash、用户和收集设置；停止后 SQLite schema 18 / integrity_check=ok，清理仅限自有 project，保留挂载数据 | PASS（Compose） |
| 2.12.0 → 2.13.1 | 旧版真实创建合成文件、额外用户和收集；冷备副本启动新版；schema 17→18、`UploadRequests.allowedDownloads` 新增且旧策略为 0，原 ID/key/expiry/限额、用户登录及文件 hash 保持 | PASS（原生） |
| 新版备份恢复 | 升级后再写入文件，停止应用后备份 config/data；独立空目录恢复，用 2.13.1 验证全部文件 hash、用户和收集 | PASS（原生） |
| 旧版快照回滚 | 另一个空目录恢复升级前快照，用 2.12.0 验证 schema 17、旧文件 hash、用户和收集；从未将 schema 18 数据交给旧版 | PASS（原生） |

原生流程使用保留 stash 中的 HTTP 辅助函数，并按真实 2.12.0 API 和 schema 调整；未应用旧 stash 或提交额外验收框架。旧版冷备 SHA-256 为 `e03e22f1f9eb5e73695179f61dde12531c349a7a6b95259e80e343cbb9926226`，恢复/回滚后原备份 hash 保持；新版冷备 SHA-256 为 `c596776e8c3533cda2a51b4376e0b066aa97a643273c50e808f206180aa8aa34`。本地原始材料另行保留，不重复提交日志。

Compose 使用本地镜像 `supashare:rel01-lite-2.13.1`。Docker Hub token 拉取超时，因此复用本机已有的 Alpine 3.23.7 运行层（tini/curl/ca-certificates/su-exec/tzdata），装入上述精确源码构建的静态二进制与项目 entrypoint；容器内版本 2.13.1，二进制 SHA-256 `584c31751386b09f120f114e9c7c349b2d0b535972b46cb12d2df318b1af2be6` 与原生受测文件一致。独立 Compose 执行 `up -d --pull never`、`restart`、`down`；全量联网重建镜像未验证，不将缓存运行层验证写成冷环境拉取通过。此前 Docker Desktop 启动阻断复核时已解除，未删除临时对象或重置任何 Docker 数据。

## 当前收口边界

FIX-01/02/03 分别通过 PR #59/#60/#61 合并（`9478e55` / `bed031f` / `54e65f4`），既有代码验证与审查复用。REL-01 Lite 必需的安装、核心流程、升级、恢复与回滚均已通过；建议 PR #62 审查合入后关闭 M5，不自动进入下一里程碑。

真实 Redis/云 S3、HA、断电及生产部署为 NOT_RUN（本轮范围外）。GitHub Actions 保持关闭，不发布 Release/tag/镜像，不自动合并本轮 PR。

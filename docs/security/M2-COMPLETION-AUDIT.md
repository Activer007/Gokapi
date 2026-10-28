# M2 授权收尾审计

日期：2026-10-28。代码验收基线 `3a9627c3ede21728688ea13f69937f2c87420ef4`，文件树与已测试发布提交 `64de3b70d2f9cc17ec89d92a3396a233de53903b` 一致。工作区 E:\LHA\Gokapi；Windows PowerShell / WSL Ubuntu-24.04，Go 1.26.0 linux/amd64。只写 Activer007/Gokapi；未部署，未操作生产数据，用户原计划目录保持未跟踪且未修改。

## 结论与适用范围

按用户在 [M2-GOAL](M2-GOAL.md) 确认的“单进程、个人 VPS / 可信小团队、内置登录参考部署”范围，M2 的代码与测试验收通过。此审计保存于独立文档 PR；合并记录与 checks 是文档落库的最终凭据。该结论不等于完整安全计划完成、OIDC 已安全迁移或已经发布/部署。

仍沿用三种角色、9 个用户权限位、10 个 API Key 权限位及小型纯策略。少量可信成员不是忽略账号/Key 泄漏的理由；公开下载者和 File Request 上传者仍按不可信主体验证。允许已授权任务结束，不承诺分布式撤销或抢占已开始的传输。

## 逐项要求与直接证据

| 要求 / 分类 | 当前源码与测试 | 验收结果及限制 |
| --- | --- | --- |
| A：角色、凭据、对象矩阵；CONFIRMED_CODE | [管理矩阵](../../internal/test/securityfixture/authorization_matrix_test.go) 的 TestSEC020ManagementMatrix / MissingKeyPermission / CurrentPermissionIntersection；[真实撤销](../../internal/test/securityfixture/revocation_test.go)；[逐路由能力矩阵](../../internal/webserver/api/security_capability_test.go) | 双 provider，超级管理员/管理员/U1/U2、长期/临时/session 换 Token、匿名/请求/过期/撤销/删除主体；覆盖列表、元数据、下载、ZIP、presign、修改、替换、复制、恢复、删除、用户和日志。检查负例的元数据与文件不变。矩阵跳过允许的异步删除，正例由既有 Api_test.go 的 TestDeleteFile 覆盖，不能算作新生命周期可靠性证明。 |
| B：deny-by-default 与当前交集；CONFIRMED_CODE | [Policy.go](../../internal/authorization/Policy.go) 的 Authorize/CanUseAPI/CanGrantAPI；Policy_test.go；Api.go 的 isValidApiKey/isAuthorisedForApi；TokenGeneration.go | 无效用户、未知动作/权限、缺失所有者拒绝；入口每次读当前 Key/User，并核对凭据种类；对象判断复用纯策略。没有新数据库表、策略引擎或授权缓存。 |
| C：替换三重边界、ZIP；HISTORICAL_FIXED / CONFIRMED_CODE | Api.go 的 apiReplaceFile：目标替换、源读取、可选删除源的 Key Delete 位及对象权；TestSEC020ReplaceBoundaries；TestSEC021MixedZIP | 双 provider 的自有/非自有/同 blob 不同所有者、明确委托与可选删除负例；ZIP 逐成员整体拒绝。历史替换越权、临时 Token 签发和降权撤权防线保留；不将旧公告当作本轮重新发现。 |
| C：后续请求撤权、单进程续期；CONFIRMED_CODE | TestSEC021RevocationAPI；SQLite/Redis UpdateTimeApiKey；Database.RotateSession 与会话变更锁；security_session_test.go、security_rotation_contract_test.go | 真实 API 降权、删用户、删 Key 后重新请求拒绝；Redis 使用时间更新不复活/覆写权限或 TTL。续期公开合同含双 provider 14 个状态、4 个撤销顺序用例，私密 HTTP 正例及 3 个撤销负例通过。SessionManager.go/Database.go 与已验证合并版本 a65ecfe 相同。不承诺已完成轮换后的旧 ID 登出能撤销整个会话家族，也不声称覆盖完整密码验证至新会话签发的事务。 |
| 公开能力与 presign；CONFIRMED_CODE | [presign 消费](../../internal/webserver/security_presign_test.go)、presign 原子领取测试、[公开下载矩阵](../../internal/webserver/security_public_capability_test.go)、Api.go/Webserver.go/ServeFile 路径 | 40 路由的 872 个凭据拒绝用例、70 个公开下载状态用例；合法 Key/公开能力正例保留。presign 消费复查当前签发 Key/User、消费者与每个对象，一次性领取；公开下载检查密码绑定、过期、次数、待删、缺失及 File Request 文件。S3 已签发外部 URL 的回收不在承诺内。 |
| File Request、普通上传及后台任务；CONFIRMED_CODE | ChunkReservation.go 的 Cancel/IsValid/SetUploading；Api.go 的上传路由与完成前置检查；Chunking.StorageID、FileServing.NewFileFromChunk、PStatusDb；公开回归及私密真实 API 合同 | 请求的取消/add/complete 均受预约域限制；普通上传按当前用户隔离，同用户有效 Key 可互换，其他域字节/元数据/预约保持不变。56 个普通上传、34 个 File Request 请求用例通过，覆盖同步/异步入口和合法普通异步完成。错误清理 mutation 触发预期 6 个失败。删除任务在授权后标记具体 ID/时间戳，取消与执行检查既有标记；物理删除失败恢复仍属 SEC-040。 |
| D：OIDC 模拟与迁移评估；CONFIRMED_CODE / VALIDATE | 私密 SEC-022 双签名 IdP HTTP fixture、现状 characterization、独立目标合同和 ADR-008 | 原始 D 要求的身份情景已在私密评估中执行；现状测试通过，目标合同仍保留 RED，不能称为已修复。ADR 明确 issuer+sub、原 User.Id 保留、备份/幂等/唯一性/恢复与回滚方案。按用户决定延期实施，不能将评估通过称为 OIDC 安全绑定完成；启用该配置须先完成身份方案验证。详情不进入公开主线。 |
| E：跨浏览器 CSRF 结论及兼容；CONFIRMED_CODE | 浏览器表单使用 GenerateBound/IsValidBound，HttpOnly 上下文 Cookie；[绑定测试](../../internal/webserver/authentication/csrftoken/security_binding_test.go)、表单 Cookie/OIDC/改密测试；ADR-009 | 私密真实 Chrome 对照记录了修复前的跨站 session 创建及修复后的拒绝，并保留同源登录正例；公开测试验证用途、5 分钟期限、单次消费、32 并发、HTTP/HTTPS 属性和多标签页。OIDC 正常回调、TLS 代理及改密注销保留。未泛化到所有浏览器/部署组合。 |

原始目标 A–E 均在上表有对应证据；其中 D 的实施延期来自用户明确修订，不是本审计自行缩减验收。完整合同见 [ACCESS-CONTROL-MATRIX](ACCESS-CONTROL-MATRIX.md)。

## 实跑与合并证据

- 最新代码的完整隔离验证：[DKXgX6](evidence/sec032-managed-DKXgX6/results.tsv)，生成、目标、awsmock、noaws、integration、相关 race 全部 PASS。公开提取 [smTgmQ 摘要](evidence/sec032-managed-DKXgX6/release-target-results.tsv) PASS；14 个公开 Go 输入与全量/race 哈希一致。完整命令与失败历史见 [TEST-REPORT](TEST-REPORT.md)。
- 当前主线 [CI 40697431982](https://github.com/Activer007/Gokapi/actions/runs/40697431982) 已核实 success，包含生成及 awsmock/noaws/integration/awstest 四组。继承镜像发布 job 的 skipped 不是测试失败，也不能当作已经构建/发布镜像。
- 已逐项重读最终结果：SEC-020 BS2VtP/LVQVYR；SEC-021 e8JIMf/Nfh4YZ/y3nYBb；SEC-023 yZKclk；SEC-032 BnrTRT/tW7WdK/DKXgX6。各最终表记录均为退出 0；原始 RED、夹具失败、启动失败及 mutation 单独保留，没有用全绿结果覆盖失败历史。
- 祖先检查确认 bf5918e、d599aab、8377de4、50965ba、0d918a1、79a0480、aff864a、a65ecfe、3d93cdb 均已进入当前主线；最后普通分片修复合并为 3a9627c。私密 OIDC/会话/上传评估提交未进入公开祖先链，安全公告仍为 draft，SEC-032 公告下没有开放 PR。
- 本审计仅更新文档，执行相对链接、引用文件/测试名、证据表、源码差异、祖先与有限秘密模式检查；本地 Go 全量/race 本轮 NOT_RUN，复用已核实代码基线的结果。文档 PR 的 CI 按仓库配置执行。自审不冒称独立审批。

## 兼容、未覆盖范围与后续阶段

[ADR-006](ADR-006-PRESIGN-AUTHORITY.md)、[ADR-007](ADR-007-REQUEST-CANCELLATION.md)、[ADR-009](ADR-009-BROWSER-FORM-CONTEXT.md)、[ADR-010](ADR-010-SESSION-RENEWAL.md)、[ADR-011](ADR-011-REQUEST-COMPLETION.md)、[ADR-012](ADR-012-MANAGED-CHUNKS.md) 是对应行为与回滚依据。旧表单需刷新，旧无绑定 presign 失效；进程重启的在途请求上传重新预约，新版不认领旧无主普通分片。已完成文件和 SQLite/Redis schema 不迁移。回滚会撤销相应防护，不等于保留当前安全合同。

以下仍明确未完成：OIDC 实施/真实 IdP 认证；多实例撤销/会话家族；所有同用户在途竞态与异步存储失败组合；真实 Redis/S3 与多浏览器配置；上传累计资源预算（SEC-030/031）；可靠删除及恢复（SEC-040/041）；加密完整性/审计/依赖和可复现发布、冷恢复（SEC-052/060/061/062/063）。真实生产迁移与部署均 NOT_RUN。它们继续留在任务看板，不能因 M2 通过而关闭。

下一阶段 P0：先明确 File Request 总额/数量与上传预算合同，或按既有优先级处理失败删除可恢复，各用单独 SEC-ID 完成 RED→GREEN。P1：构建固定清单/扫描、恢复演练；OIDC 在确需启用时进入独立兼容评审。保持现有 Go 单体、SQLite/Redis、本地/S3 和 UI/WASM。

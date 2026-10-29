# M0 信任边界与安全不变量

> 当前 M2 范围以用户于 2026-10-28 确认的 [简化目标](M2-GOAL.md) 为准：单进程、可信小团队，保留关键授权边界。下列历史矩阵/待办不再全部构成 M2 阻塞 Gate；延期不等于已修复。

基线 a9c25d453aab0c25f2d6e6926ec7312628638844，证据及分类见 [SECURITY-BASELINE](SECURITY-BASELINE.md)。模型面向个人 VPS / 小团队；不改变单体 Go、SQLite/Redis、本地/S3 或现有 API 架构。

## M4可靠删除边界

040-B待验收的不变量：旧授权快照不改变新owner/locator；旧timer不覆盖新计划；成功恢复必须排除正在回收的同一源；
仅修改当前意图需要的字段，缺失记录不复活；API/CLI遇到回收未知或失败不能报告物理删除成功。
041待验收的不变量：替换与旧locator候选原子提交；删除前完整引用检查包含原target当前引用；未知/共享/重新引用保留内容。
两组是本轮计划与合同要求，未取得各自最终证据前保持OPEN。
040-B候选的FR撤销边界：持独立请求门禁复读scope，先持久过期/撤key再完整枚举；保存及owner迁移同门禁。
Claim不等于永久发布许可，Commit须按当前provider policy拒绝过期FR；清理失败不得重新开放请求或丢失锚点。
普通用户删除与全部进行中/新认证上传的原子隔离仍VALIDATE，不从本轮逐文件条件更新推出该广义保证。

metadata是删除重试锚点；未确认探测/删除/引用/退休时不报告完成，不把unknown当NotExist。
保留过期锚点不恢复公开下载/hotlink访问；logical用量保守保留到实际metadata退休，不另造退款计数。
完整有界引用投影包含所有者之间、过期及pending行；本地别名/不规范名称或大小写歧义保留现场，S3 key不归一化。
持locator锁复读、探测、完整引用检查、物理删除、再次确认缺失、条件退休；物理I/O不跨DB事务。
本地Root锚定受信配置DataDir，拒对象symlink/目录/越界；允许配置根可信symlink，不扫描磁盘猜归属。
单项失败保留记录并继续其他项各一次；完整扫描失败停止，超时保留未确认项；COMMIT/EXEC未知须下一轮复读。
这个合同不能撤回已执行删除：040-B恢复/timer门禁及041旧locator持久回收继续OPEN，不能用CAS声称它们已完成。
040-A由PR44交付，最终CI40887732123与实际输入树核对成功；相关race及Linux权限/链接合同PASS。
040-B状态协调已有独立私密RED，仍需修复与兼容验证，不从A的通过推定其安全。

## M4下载失败边界

受支持加密下载只有完整流认证并正确结束后，才能正常完成；不能把密钥初检或HTTP 200当作完整性证据。
前置密钥错误返回500/无附件，流已开始后的错误必须中断，不把错误文案拼入附件；FD关闭、失败时源文件及metadata保留，
不把失败下载标为SetComplete，不额外退款/回收；计数/流量按原有受理尝试记账。
052-A同时补齐WASM初始化/读取错误的controller.error与页面输入取消/输出abort，finally清busy；
失败提示不能代替真实下载终态。一个Chromium+HTTPS正负例独立取证，其他路径继续VALIDATE。
本地及该浏览器窄合同GREEN，最终CI40870398244/新增小合同race通过；详见TEST-REPORT，不推定主动取消或其他路径也已完成。
精确负面合同留私密审查，当前结论不证明外部攻击者可制造该前提；格式/加密协议不变。
M4完整范围见M4-GOAL，删除/旧内容引用与浏览器边界待后续证明，不能从此窄修复推出全部完成。

## M3恢复/收口范围（历史冻结）

后续只按[M3-GOAL](M3-GOAL.md)实现最小恢复。只有进程已停止、对象归属及允许清理的状态可证明、
清理确实完成后才释放预留；不确定Claim/发布状态保额保文件，并交人工核对。
已发布/共享canonical及未知路径不因超时、单一路径不存在或ledger状态推测而删除。
恢复门禁先于常规GC/HTTP；ledger每页128、最多4096，DataDir根扫描最多16384项、诊断最多128条，启动deadline30秒。
所有namespace先校验归属/marker/文件类型，再清理；已提交chunk要求metadata完整提交摘要及local明文/E2E canonical类型/大小/流式hash一致。
无法持久映射的whole工作不因Committed而可删除；已Claim、远端/metadata不明、foreign entries保持现场并阻止启动。
仅在确认私有清理后Abort/ReleaseStaging，不创建第二条metadata、不触canonical；重放不重复退款。
三类SQLite/local真实进程退出及2个negative在最终CI40833366371的race步骤PASS（代码db4dc8a，候选a78f7be5）；
确认后清理与重放不退款的Gate完成。详细证据以TEST-REPORT及PR35为准；不证明断电或真实远端恢复。
下列历史不变量与未完成限制保留，不将延期解释为通过；具体回滚和人工步骤见ADR-021及UPLOAD-RECOVERY-RUNBOOK。

## SEC-030 文件替换不变量

两端授权快照在TX/CAS重读；Replace/Read/可选Delete权限及既有FR/E2E门槛不变。
仅正增长计target owner/instance bytes，含活动预约；不增files/request/active/staging，不提前抵扣来源删除。
目标内容字段与可选source过期原子提交，保留当前target policy/counters/owner/hotlink。
metadata锁按stripe去重排序，再source blob锁、再DB；同ID删除明确拒绝，避免隐式删除目标。
物理回收沿既有GC，不增加旧blob删除策略；其他全量写者/批量迁移和孤儿回收仍须单独证明。

## SEC-030 单文件所有者转移不变量

只允许原有admin/Edit管理边界；同事务重读source逻辑投影、源pending状态及目标用户存在性。
只为目标owner新增一份完整logical bytes/files，并计入其全部活动预约；source扣减同一份，
instance/request总量不变。同owner零增量幂等，未增长的层允许已有超额；不套公开FR上传门槛。
成功仅patch UserId，不覆盖当前下载计数、blob、加密、hotlink、request或其他参数；无新增预约/暂存。
拒绝不变更metadata/ledger/hotlink。删除用户时的批量迁移及其他全量metadata写者仍为独立OPEN。

## SEC-030 复制准入不变量

复制增量不变量：每份metadata按完整逻辑大小计数，含源request归属；与已有预约同事务争额。
源权限相关身份/blob/encryption状态须仍匹配，拒绝不发布metadata/hotlink；不增加上传会话或物理暂存。
源pending/过期/下载耗尽均拒绝，Redis同时WATCH源key，SQLite在BEGIN IMMEDIATE内复核。
跨owner复制/转移后仍按原request计量；管理复制不要求parent owner等于当前文件owner，公开上传的owner检查不变。
当前request计数/累计上限仍生效，parent缺失/损坏拒绝；管理复制不新增公开入口的expiry/单文件门槛。

## SEC-030 分片/请求接线：本增量已验证

以57e5116为基线，ADR-017要求按owner/request稳定ID先冻结总声明，再使用有界持久范围回执；
现行累计字节默认按用户选择为owner5GiB、instance20GiB；旧20/100GiB是PR21历史选择。
Truncate长度不能作为完整覆盖证明。当前request策略在事务/CAS内重读，Claim必须在创建任务之前。
同步保留请求取消；异步有服务代际和有限截止时间，取消不代表I/O已经停止或可以退款。
新进度、失败关闭、范围、重复完成和确认证据、兼容目标、E2E真实writer分段及真实HTTP停服通过；
143f041的Linux四组全仓、相关race及whole/chunk大文件也已实际通过，具体输入/命令见TEST-REPORT。
这些结果不证明完整M3、实际浏览器或发布后恢复。
既有ledger staging及独立receiver预算有限，但检查当前free不是跨所有写者的原子物理分配器；
完整物理协调仍是后续Gate。未知发布/Claim后重启保额，不能将超时、cancel或无文件路径当作退款证明。

## SEC-030 整文件持久准入：本增量已验证

起点710f4bf，已验证代码9db2542；Linux四组全仓、相关race及1–2GiB实传通过，具体证据/Windows限制以 [TEST-REPORT](TEST-REPORT.md)及[ADR-016](ADR-016-WHOLE-UPLOAD-ADMISSION.md)为准。只覆盖 `/api/files/add` 整文件接线，不扩大成所有上传路径已强制额度的结论。

- 认证在外层完成；整文件强制空FileRequestId，owner来自当前授权主体，不从multipart覆盖。多个Key共享owner计量，去重和RealSize不能抵扣逻辑量；现存metadata仍进入快照。
- 共用receiver先以有限正文槽/字节预算接收，再以实际file长度在provider原子Reserve/Claim；不能以receiver429当作累计额度拒绝证据。永久字节/数量拒绝为413/21，receiver429/20保持可区分。
- 并发夹具必须先确认所有不同Key实际持久化且认证成功；SQLite唯一PublicId覆盖导致的401不证明任何quota边界。原错误夹具结果保留为FAIL；修订后的双provider竞争已在PR21重新运行通过，详见TEST-REPORT。
- metadata、hotlink和完成回执应在同一provider提交边界发布；拒绝不得提前创建可用hotlink或覆盖现有映射。Redis EXEC运行期错误必须明确返回，不能宣称整批自动回滚。
- 处理放大采用有溢出检查的 `4*实际payload+64KiB` 持久预留，receiver预算独立。I/O停止、自有work/spool关闭并清理确认后才能释放；已发布或Commit不确定的目标继续占charge，不自动删除canonical、不按超时退款。
- 已有共享blob只在长度/加密metadata验证后复用，未知服务器密文拒绝覆盖；E2E声明不能绕过必需服务器加密，无法表达的双层组合拒绝。新S3调用传递Context并严格传播存在性检查错误。
- 加密metadata读取必须拒绝损坏编码及尾字节，以字节长度限制投影，数据库不可用须返回错误而非panic。新增getter合同曾真实失败，修复后双provider目标已PASS；完整Gate仍单列，不能用目标绿色代替全部路径验证。
- 内部上传ID仅在本次服务端调用内稳定；没有跨HTTP重试幂等协议。数据库关闭重连证明持久记录保存，不证明崩溃后文件目标已安全对账。

本增量选择可配置的有限默认owner20GiB/1000/4、instance100GiB/5000/8、staging12GiB；现有MaxSize的单文件语义保持。owner在途限制从接收后开始，正文阶段仍由实例receiver约束。分片、File Request新增累计限制、复制/替换/所有者转移等正向metadata变更、异步完成与启动恢复仍OPEN，因此其他入口仍可能增加用量而不经过本轮准入。单进程与可信DataDir假设不变，不建立多租户权限引擎或分布式配额。

## 参与者与资产

| 主体 | 权限来源与不能推定的权力 |
| --- | --- |
| VPS 管理者 / 初装者 | 控制进程、卷和反代；可读取服务端密钥，不自动等于每个浏览器 E2E 私钥持有人 |
| super-admin / admin / 普通用户 U1、U2 | rank、permission、当前账号状态；角色与每个 API Key 授权是不同维度 |
| Session 持有人 | 当前 User + 会话有效期；轮换/撤销后不可继续继承旧权力 |
| 普通 API Key / 临时系统 Token | Key scope/expiry、当前 User、对象所有者；临时 Token 不等于全权 admin |
| File Request 邀请持有人 | 对特定 request 的有限上传能力；不能进入管理 API 或其他 request |
| 匿名下载者 / 密码 token 持有人 | 某分享 capability、有效期、下载次数和密码要求；知道 blob hash 不授予跨用户管理能力 |
| 代理、OIDC IdP、S3 | 只在各自配置和验证范围内受信；不能由请求 Header 自称代理/IdP |
| 后台清理/异步删除任务 | 继承明确对象与状态；不能借后台身份绕开最初授权、引用检查和撤销条件 |

资产包括文件内容与名称、元数据/所有者、去重引用、用户密码哈希、session/Key/request/presign capability、服务器加密材料、客户端 E2E 密钥、磁盘/内存/连接/CPU、审计记录与备份。日志中的完整分享 URL 本身也可能是访问能力。

## 数据流与边界

1. 互联网→反代→Go HTTP：TLS 终止、来源 IP、可信 Header、初装入口、请求体/连接限制。仅 localhost 绑定的示例不能证明所有实际部署都无暴露。
2. 浏览器→session/CSRF→路由：Cookie 属性与浏览器 Origin/SameSite 规则；API Key 路由单独认证，前端按钮不作为授权。
3. OIDC→回调→本地账号：state、issuer、subject、claims 与账户关联。当前按邮箱关联的事实与可利用性分开验证。
4. 上传→chunk reservation→临时文件→blob→DB：每一步需要限额和失败回收，声明长度不能替代实收字节；同 hash 不代表同所有者。
5. DB→文件生命周期→本地/S3：元数据与物理存储非同一个事务；故障时保留可重试的管理状态。公开分享过期和物理销毁是两件事。
6. 公共下载→密码/次数/过期检查→加密与响应头：服务端加密能访问明文，E2E 服务端仅应看到密文；File Request 不自动获得 E2E 保护。
7. 源码→go generate→二进制→镜像→备份恢复：必须记录相同 SHA、tags、工具链和 digest；开发分支功能不推定已进入 stable。

## 不变量与最小反例设计

每个 P0 行给出前提、资产和可实现的失败用例。P0 表示先处理，不表示该攻击已经实证；测试状态以 TEST-REPORT 为准。

| SEC-ID / 不变量 | 前提与受影响主体 | 最小 RED / 验收设计 |
| --- | --- | --- |
| 010 初装必须由已授权部署者完成 | 配置不存在且 setup 可被非部署者访问；管理员身份/配置 | httptest 调 basicAuth，匿名请求不得触达 next；本轮 opt-in TestSEC010；随后覆盖首次成功、重启、已有配置、重配 |
| 011 部署基线最小权限 | 使用默认 root 镜像/旧卷；主机隔离与文件权限 | 隔离容器断言 uid≠0、capabilities/resource limits；新/旧 UID 卷可写且不递归误改非目标目录；Docker 未具备则 BLOCKED |
| 012 HTTPS Cookie 安全且旧哈希迁移可回退 | HTTPS 或 TLS 终止；session/密码 | TestSEC012 检查 Secure；新增登录/退出/password/OIDC Cookie 矩阵；legacy 成功登录升级哈希，错误密码不迁移；旧备份恢复 |
| 013 只有可信 immediate peer 能提供身份/IP | Header Auth/Cloudflare 开启且能直连源站；账号身份/审计/限流 | httptest 设置非可信 RemoteAddr + 伪造 Header，期待不授予身份/不改变来源；可信代理正例、多层和 IPv6；部署网络隔离另验 |
| 014 取消请求释放资源，慢头受限 | 慢连接、等待队列、大上传/SSE；可用性 | 限流耗尽后取消 Context，应有界返回；本地短窗口慢头测试，上传/SSE 正例；不以 12h 截止期替代连接限额 |
| 020 同一 actor/action/object 一致授权 | U1/U2/admin，各类凭证；跨所有者内容 | 已有 M0ActorBoundaries 只覆盖读取/过期/撤销/类型；后续展开 list/read/download/ZIP/edit/replace/restore/delete、公开 URL、异步队列；先 RED 再抽取策略 |
| 021 降权/禁用后旧凭证不保留权限 | 已签发 Key/临时 token/session；账户管理权 | 签发→降权/删除/重置→敏感 API；同时验证新签发凭证；已有历史修复回归，不把函数存在当完整矩阵通过 |
| 022 OIDC 账号使用稳定且受验证的身份 | IdP 允许邮件变更/跨主体同邮箱等；本地账号关联 | 两 issuer/两 sub 同邮箱、同 sub 改邮箱、email_verified=false、错误 aud/issuer；先明确 IdP 保证和预期，再测完整回调；当前 VALIDATE |
| 023 登录 CSRF 与发起浏览器绑定 | 跨站表单、双浏览器、Cookie/Origin 条件；受害者登录会话 | A 取 token，B 发起登录；必须在真实浏览器与反代设置下检查能否被迫切换账号；当前 VALIDATE，旧公告已修 |
| 030 used+reserved≤quota，失败可回收 | 同一用户/实例 10+ 并发上传；资源可用性 | 小额度屏障并发预约，逐次断言预算；取消/重启不泄漏；逻辑字节和物理去重字节分别计量；当前无完整额度实现 |
| 031 File Request 限额语义一致 | 同 request 连续/并发多文件；邀请上传资源 | MaxSize=1MiB，两个分别小于1MiB但总和超限；按最终明确的合同断言，不能把历史单文件 CVE 改名重复报告；先测后确定兼容方案 |
| 032 所有上传路径实收字节/偏移/临时文件有界 | 已授权上传者、未知长度、UUID 重用；磁盘/内存 | 受限 httptest body、超限 offset、重复块/跨 owner UUID；检查磁盘峰值、拒绝与清理；保留当前 MaxBytesReader/长度防线 |
| 040 删除失败仍可管理且可幂等重试 | 本地 EPERM、S3 5xx/超时；文件与删除记录 | TestSEC040：HEAD 正常、DELETE=503，blob 存在时 metadata 不得消失；后续加重启、重复删除、本地 EPERM、SQLite/Redis |
| 041 替换不遗留无引用 blob，不删除共享 blob | 两所有者共享内容、替换+deleteNewFile；完整性 | A-old/B-new/C-shared，多步故障注入；引用>0 不删，引用=0 最终回收；先审 #419，拒绝用 metadata 覆盖当物理删除成功 |

P1 不变量：042 有界 GC 扫描与可重建索引；050/051 响应头、MIME/文件名不允许上传内容获得管理域脚本权限；052 E2E 支持组合须字节完整、截断显式失败；060 审计结果明确且无秘密；061 S3 失败可观察、旧凭据兼容；062 构建来源一致；063 配套备份可恢复。严重完整性问题经验证后将 052 升为 P0。

## 测试隔离与残余风险

新 Scene 只在 t.TempDir 写合成内容、动态生成凭证；SQLite 临时文件、miniredis 随机回环端口、httptest S3/IdP。每个测试独立建库；使用配置/数据库包级状态的测试不可 t.Parallel。S3 mock 不能代表 IAM、真实存储持久化或供应商兼容；IdP 协议 fixture 不能替代浏览器登录试验。

完整 API 授权矩阵、真实 Redis 恢复、Docker 非 root/卷升级、浏览器 CSRF/E2E、大规模并发和 VPS 配置均不由 M0 绿色单测推导。M1 起每个功能 PR 必须给出其自身不变量、RED、修复、GREEN、兼容与回滚；重大哈希/身份/E2E/schema/删除设计另建 ADR。

SEC-003 审查补充：凭据类型边界必须独立于权限位。让 File Request Key 拥有与普通 Key 相同的权限后，管理路由仍应返回 401；普通 Key 进入 File Request 路由也应在参数解析前返回 401。在一次性源码副本中禁用类型判断，SQLite/Redis 上两项均必须失败；仓库生产实现保持完整。证据脚本遇到失败必须返回非零，同时保留每个命令的真实退出码，不能把缺失日志或编译失败视作目标 RED。

SEC-010/M1：读取配置目录中进程私有证明文件的部署者是首次安装信任主体；只读取容器日志不授予权限。首次安装、重配都要求证明，提交成功即失效；所有排队保存操作再次检查失效状态。原生只监听回环，Docker只允许受限发布/受信代理传输证明。浏览器跨站写请求和使用GET保存配置均被拒绝。非浏览器缺少Origin可以发送请求，但仍须认证。跨站单测只验证服务器防御，不声称已完成浏览器CSRF利用验证。

SEC-012/M1：对外 URL 为 HTTPS 时，新发/续期/清空的会话 Cookie、下载密码 Cookie 和 OAuth state Cookie 均必须 Secure，并保留既有 HttpOnly/SameSite；反代后端 HTTP 或冲突转发头不能降级该属性。明确的本地 HTTP 配置须正常登录。网页改密成功撤销该用户已有会话，错误表单和其他所有者会话不受影响。legacy 成功登录才升级哈希，失败登录不得改写，升级后重连数据库仍可登录；旧版本回滚要求配套快照。旧浏览器 Cookie、绝对会话期限、并发注销、OIDC 稳定身份绑定及完整冷恢复不由此测试推定安全，边界见 ADR-002。

SEC-013/M1：身份头与来源IP头使用相同的直接peer信任列表；未授权peer不能借自报的XFF/CF地址获得信任，且被拒绝的Header请求不得触发自动注册。Docker整个网段默认不授信，显式CIDR意味着信任其全部成员。XFF只沿可信链向左解析，畸形跳点不得跳过；CF/X-Real-IP必须是单一合法IP。代理必须认证并覆盖身份头，来源头必须正确清理/追加；可信代理本身被攻陷或错误转发不能由该peer检查修复。配置迁移与网络限制见ADR-003，容器实测仍归SEC-011。

SEC-014/M1：未完成HTTP头不能占用完整传输超时；请求取消必须释放五类限流等待且不得继续认证。每个限流store的查询次数不能决定常驻清理协程数量。请求头和空闲连接的短期限不得截断已建立SSE与正常文件正文；真实大于multipart内存阈值的上传/下载必须字节一致。统一管理handler执行时限、来源数量配额和已经启动的存储任务取消不在该不变量证明范围；见ADR-004。

SEC-011/M1：选择非root Compose时，应用与entrypoint以显式非零UID/GID运行；不匹配的持久化卷权限应拒绝启动且不能自动改属主。部署者预备可写卷后，初始化证明仅属主可读、未授权安装不能写配置，正常登录/上传/匿名下载及重启后的文件和数据库持久化必须通过。只测试新建标记卷，禁止迁移或递归chown真实卷；镜像默认USER不在本项改变。

SEC-014补充不变量：管理路由的网络读写与可取消工作采用独立30秒预算，不能继承上传/下载的小时级预算；文件、分片、File Request上传、SSE、WASM和静态资源保持原传输预算。超时/断线必须传递到OIDC兑换和UserInfo请求，不改变身份关联。已启动且不响应Context的本地CPU/数据库操作无法被Go强制中断，不承诺事务回滚。

SEC-020/M2：管理操作必须满足当前有效用户、有效且种类匹配的Key、Key动作位、当前用户动作位、对象所有权或明确委托的交集。超级管理员也不能绕过缺失的Key位；未知动作/无效用户/缺失对象所有者默认拒绝。替换分别检查目标、源读取、可选源删除，去重共享blob不合并所有者权限。拒绝操作不能修改文件元数据或内容。签发Token与使用Token复用同一用户权限映射，管理网页复用对象策略。

最小RED使用隔离SQLite/miniredis直接保留旧Key权限并撤销用户位，验证四类入口拒绝；其威胁前提是中断更新或旧记录不一致，不推定攻击者能够制造该状态。完整合同见[ACCESS-CONTROL-MATRIX](ACCESS-CONTROL-MATRIX.md)。顺序请求的当前权限检查不保证撤销与在途请求的原子线性化；公开分享、presign消费、分片、File Request上传和异步删除仍须单独测试。禁止用此矩阵推导浏览器CSRF或OIDC身份绑定安全。

SEC-021：LastUsed/LastOnline只属于使用记录，不授予创建用户/Key、恢复旧权限或改变TTL的能力。更新时间与删除在同一provider内并发时，无论先后，最终删除必须保持；更新已有记录只能触碰指定时间字段。测试在两个隔离provider中保留旧快照并更新/删除记录，再交付延迟使用记录；另用同步屏障覆盖并发删除。该局部原子性不能被扩大为整个HTTP请求/管理员操作的事务保证。

真实API撤销后，新请求必须按当前授权拒绝；普通Key撤销不应无故吊销另一把独立临时Key或会话。管理员降为普通用户后，列表过滤且所有未授权跨所有者文件动作拒绝，不能仅验证日志入口。ZIP与ZIP presign须在输出字节/签发前验证全部成员；一项无权读取就整体拒绝。

SEC-021 presign：管理grant不能替代当前主体、Key和对象授权。消费时签发者与下载者都必须具有每个文件的读取权；签发Key有效且具备Download位，File Request Key不能充当管理签发者。未知/缺失绑定默认拒绝。只有授权检查通过后才能原子领取一次，失败调用者不能销毁另一合法下载者的grant；不得记录完整grant URL或Key。此合同与公开分享链接的有限授权分开，兼容和在途撤销边界见ADR-006。

## SEC-032 取消预约合同

取消权由当前请求的有效预约决定。未知或无效预约不触碰文件；成功删除才释放预约；失败保留重试且不暴露内部路径。并发取消最多执行一次成功删除，重复和上传前取消保持幂等。此锁不覆盖已获准的在途写入/完成，不代表整个上传生命周期已线性化。
## SEC-023 表单上下文不变量

内部表单 Token 只能由取得该表单的浏览器上下文消费，登录和改密用途隔离；到期/重放/缺失绑定拒绝。
同浏览器多标签页可独立提交，32 个并发消费者最多一个成功。服务端实施这些检查，不依赖前端。
Token 与 Cookie 不替代凭据、当前会话和对象授权；详细私密对照、公开验证及兼容边界见测试报告和 ADR-009。

## SEC-020 公开能力不变量补充

公开文件 ID、hotlink、密码下载 Token、presign、session ID 与 API Key 是不同权限域。把它们换到 apikey Header，或仅发送 session/文件密码 Cookie，不能授予管理 API 权限；File Request Key 只通过公开上传域认证。下载能力不是资源所有权，合法公开分享可读取另一所有者文件；仍必须遵守当前过期、次数、待删、File Request 私有文件和密码绑定约束。拒绝操作不修改文件内容/元数据，也不消费独立密码/presign 能力。

新增测试覆盖顺序 handler 与实际 API 路由认证，不涵盖授权检查后与在途写入/撤销的线性化，也不把 miniredis 视为真实 Redis 或本地 blob 视为 S3 验证。

## SEC-021 单进程会话续期收尾

2026-10-28：续期必须条件替换仍存在且未变的原记录，不能恢复已撤销会话；用户/全局撤销与续期串行后不遗留新记录。普通授权读及已获准传输不加全程锁。采用既有 database 门面进程锁，兼容与旧 ID 注销范围见 [ADR-010](ADR-010-SESSION-RENEWAL.md)。多实例和会话家族不在本轮承诺内。

## SEC-032 File Request 完成边界

安全不变量：File Request 凭据只能添加或完成属于该请求的有效预约分片；同一所有者的另一请求、其他所有者请求、普通管理上传分片均不获得隐式授权。拒绝必须发生在文件打开/删除、元数据创建和异步任务排队之前，不能改变受保护分片、元数据或预约。合法完成仍归属于请求所有者并释放预约。源码与隔离测试证据确认后才判定修复；详细复现只进入私密审查。

## SEC-032 普通上传作用域修复

普通分片 UUID 在当前用户空间内解释；其他用户同 UUID 的上传不能改变或消费受保护分片。不同用户可独立创建同名上传，缺少自己分片的完成请求必须在异步排队前拒绝。任何完成错误清理也仅作用于已授权存储范围；旧无所有者临时文件不能被新请求认领。File Request 继续受预约检查限制。同用户有效长期/临时 Key 与浏览器身份映射一致；对外进度保留 UUID，进度记录按用户隔离。已授权任务允许结束，不引入跨实例或通用事务协议。

## M2 验收边界更新

五条核心授权不变量按 [M2-GOAL](M2-GOAL.md) 的单进程语义完成验证，逐项证据见 [M2-COMPLETION-AUDIT](M2-COMPLETION-AUDIT.md)。已授权任务可结束；公开能力有其有限读取语义；普通上传的 UUID 只在当前用户域解释。此结论不覆盖多实例、OIDC 身份迁移、累计上传预算或可靠物理删除，相关威胁和后续 SEC-ID 保留。

## M3 资源预算不变量

用户确认 MaxSize 保持单文件语义、另增请求累计限制。每个metadata按当前所有者的可验证SizeBytes独立计逻辑用量，共享SHA1/加密/展示RealSize不能抵扣；实例暂存及最低磁盘余量独立约束。稳定上传ID的重复声明/完成须幂等，provider原子检验used+reserved+requested；取消/失败清理后释放，重启恢复先对账。HTTP正文/内存/异步处理并发有总界，未知长度仍按实收字节限制；正常1–2GB不整文件读内存。以上为M3完整目标，分层已实现及剩余边界见下；不由SEC-031现状测试推出整体已实现；详见 [ADR-013](ADR-013-UPLOAD-BUDGETS.md)。

SEC-030计算边界：非法/负数/溢出不产生可用容量或部分快照；逻辑计量独立于去重/加密展示，返回map不与输入共享可变记录。纯CanReserve决定必须和provider写入原子执行后才具有准入意义，10/50并发请求分别计算true不能算防护。桥接必须显式解释旧0=无限制并保留有限实例上限。

SEC-030持久基础：快照、容量判断和预约/完成metadata在同一SQLite事务或Redis WATCH/EXEC中提交；过期不释放尚未清理的资源。满账本仍可确认清理，终态回执有有限期限和数量。迁移不能丢活动状态或混入目标遗留认证数据。CleanupConfirmed只是可信内部断言，实际清理/恢复和入口当前策略仍待接入；见[ADR-014](ADR-014-DURABLE-UPLOAD-RESERVATIONS.md)。

SEC-062测试环境不变量：setup的真实HTTP回归不能依赖固定空闲端口；操作系统分配的listener从分配至Serve始终被持有，每轮启动/结束有同步，延迟关闭只作用于本轮服务器。主动占住旧53842时仍须验证原认证、一次性证明、重放和慢请求头边界；不能用重跑或跳过断言冒充通过。生产监听策略保持不变，其他固定端口夹具仍需按实际证据验证。

SEC-030接收边界：file-first尚未知稳定上传ID时，正文读取前也必须先取有限接收槽；每批数据在写spool前原子取得有限字节预算，未知/低报HTTP长度不能绕过。Close或delete未确认时不还字节，重启先核对自有marker/普通payload，未知项不删。该receiver只管理可信DataDir中的独占暂存目录；不能将其保证扩大到持久chunk/metadata总量、共享blob或多进程，见[ADR-015](ADR-015-BOUNDED-UPLOAD-RECEPTION.md)。

接收层已验证范围以[ff7497b证据](evidence/sec030-receiver-ff7497b/README.md)为准：10/50混合入口竞争、未知长度、真实取消/断线、槽满HTTP/1拒绝、QP兼容及1–2GiB读回。认证/过期能力在receiver外提前拒绝的传输行为另列VALIDATE，尚无该类真实socket结论；不能由忙碌拒绝用例推导。持久chunk离开receiver后的资源、累计预算与完整恢复仍是待实现不变量。

SEC-062构建不变量：接收器之前的标准库HTTP协议解析也必须使用已审查安全补丁；不能以handler槽/正文预算覆盖协议层资源风险。根模块、生成用模块和三个Go构建镜像声明一致，CI验证真实GOVERSION，产物BuildInfo记录编译器；扫描按实际生产default/noaws及生成器/服务端/CLI产物执行，发现已知漏洞或扫描失败必须使Gate失败，不能因JSON格式退出0误记PASS。补丁compiler改变后需重生成两个WASM及配套wasm_exec.js；生成成功不等于浏览器交互验收。此Gate不允许开启继承的上游发布任务，不触碰数据/密钥/schema。

SEC-062证据边界更新：58d2cb5实际compiler/生产源码配置/独立构建产物扫描、有限Range兼容与1–2GiB流式验收已通过，见最终证据；历史负向扫描的退出3是预期RED，不能视为旧程序安全。新Docker镜像仅验证构建和BuildInfo；没有将此扩大成启动/非root/浏览器/真实S3或全发布扫描。模块级未调用公告继续记录，后续新增调用必须由扫描Gate拦截；不以静态分析绿色宣称任意动态路径安全。

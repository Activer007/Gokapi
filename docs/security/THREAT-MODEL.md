# M0 信任边界与安全不变量

基线 a9c25d453aab0c25f2d6e6926ec7312628638844，证据及分类见 [SECURITY-BASELINE](SECURITY-BASELINE.md)。模型面向个人 VPS / 小团队；不改变单体 Go、SQLite/Redis、本地/S3 或现有 API 架构。

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

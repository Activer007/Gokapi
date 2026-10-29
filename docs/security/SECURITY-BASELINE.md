# M0 安全基线

研究日期：2026-10-27（Asia/Shanghai）。范围为个人 VPS / 小团队的单体 Go 服务。本文是源码与测试基线，不是安全认证。只向用户已独立的 **Activer007/Gokapi** 交付；Forceu/Gokapi 只读，不创建上游 PR、不推送、不触发其工作流。

## SEC-030 分片/请求准入增量：IN_PROGRESS（2026-10-29）

当前起点57e5116，Go1.26.10 windows/amd64；本地tracked基线干净，保留用户两个未跟踪目录。
CONFIRMED_CODE：旧分片绕开持久账本，预分配长度不等于完整覆盖，旧异步完成未在入队前Claim。
隔离57加现有HTTP符号合同实际RED；当前新API全部目标、兼容目标、真实加密流分段及停服目标已PASS。
统一请求累计/active env、当前request策略事务重读、私有有界范围证据和Claim后受控任务已接线，见ADR-017。
这不是未知漏洞定性。VALIDATE：本机完整noaws真实FAIL，Linux全仓/race/大文件及完整M3 Gate尚未验收；
跨所有写者的物理容量协调、其他metadata正向准入和发布后恢复仍开放，不沿用PR21绿色。

## SEC-030 整文件准入增量：代码 Gate 通过（2026-10-29）

开发起点 `710f4bf32ae80ac110bb5cd24e8d92848ac22bd8`，仅源码/合成隔离环境，尚未部署。CONFIRMED_CODE：`/api/files/add` 由共用receiver进入NewFileReserved，按真实payload Reserve/Claim，并把metadata、hotlink和回执纳入同次provider Commit；本增量Gate结果如下，不扩大为完整M3已完成。范围、兼容和失败保持见 [ADR-016](ADR-016-WHOLE-UPLOAD-ADMISSION.md)。

用户已确认每文件 MaxSize保留并新增累计预算；本增量选择可配置的有限默认 owner20GiB/1000文件/4在途、instance100GiB/5000文件/8在途、staging12GiB，没有将具体数值记为用户逐项批准。receiver另有12GiB接收预算；新持久暂存声明为 `4*实际payload+64KiB`。旧超额或异常metadata可能阻止新整文件准入，不自动删除/改写旧数据。

CONFIRMED_CODE：9db2542的[PR CI40778813175](https://github.com/Activer007/Gokapi/actions/runs/40778813175)与[完整手动40778841823](https://github.com/Activer007/Gokapi/actions/runs/40778841823)均SUCCESS；四组全仓、receiver/whole相关race、当前扫描、三镜像隔离构建及真实1GiB明文/2GiB服务器加密HTTP读回通过。[证据](evidence/sec030-whole-9db2542/README.md)区分实际48份旧scope源码hash、生成产物和新增完整源码日志，不能把部分hash说成全部输入。

VALIDATE：分片、File Request累计字段和入口、所有正向metadata变更、异步完成及启动目标对账继续OPEN。不能称所有API累计额度已经强制或可防绕过。发布/Commit不确定时保留charge，不自动退款、不删除canonical；内部稳定ID只属于单次调用，不提供跨HTTP幂等。Windows全仓失败单列于TEST-REPORT，不借Linux通过隐去失败。

证据更新：旧SQLite并发Key夹具缺少唯一PublicId，覆盖/401结果不计入配额证明；修订夹具重新冻结基线后，双provider的bytes/files×10/50均接受10/50且无quota拒绝，真实RED退出1。当前API目标相同合同全部PASS（8.330s）；存储含旧路径回归、锁取消/GC交错/active与staging拒绝PASS（7.831s）。新增getter的尾零/不可用provider合同真实FAIL后已GREEN；相关全仓/race及当前大文件仍以TEST-REPORT后续Gate为准，不宣称未知漏洞或完整M3已完成。

已只读复核上游19项开放PR及quota Issues #431/#54，见[清单](evidence/sec030-whole-upstream.json)。#424为SDK整体迁移、#419为替换旧内容回收，与本增量持久整文件准入不同；没有采用无关架构/数据库重构，也未向上游写入。

SQLite16/Redis9不变，无新数据库字段、密码或密文格式；混合E2E与必需服务器加密拒绝，未知服务器密文blob不重写。回滚须停写、备份/核对数据库和DataDir及配套配置密钥；旧代码不理解活动charge，不能因schema未变就直接降级。

环境更新：Windows便携Go1.26.10现已可用；下载zip的实际SHA256与官方清单记录一致，隔离cache/TMP在ignored证据目录。WSL仍BLOCKED，本轮禁止启动/修复宿主；先前WSL失败保留。真实RED和当前NOT_RUN范围见TEST-REPORT，不能继承前一工具链/接收PR的绿色作为本轮通过证明。

## 版本和可复核来源（M0 初始快照）

| 项目 | 实测结论 |
| --- | --- |
| 工作目录 | E:\LHA\Gokapi |
| 分支 / HEAD | master / a9c25d453aab0c25f2d6e6926ec7312628638844 |
| origin | https://github.com/Activer007/Gokapi（fetch/push） |
| 初始工作树 | 无已跟踪修改；用户已有未跟踪目录 Gokapi-Security-Development-Plan-2026-10-27/，未修改 |
| 本地历史 | 非 shallow；初始没有本地 tag refs，但 v2.2.4 commit 对象存在且为 HEAD 祖先 |
| 上游实时 master | git ls-remote 确认与本地 HEAD 相同；没有更晚的 master 提交 |
| 上游 v2.2.4 | 9a4b289c604198c05589e34314e889d2c25386d8；源码版本 2.2.4；go.mod 为 Go 1.25.0 |
| 最新正式发布 | GitHub releases/latest 返回 v2.2.4，published_at 2026-03-10T15:44:19Z；不把 master 当成已发布版本 |
| 当前源码版本 | cmd/gokapi/Main.go: 2.2.5-dev；go.mod: Go 1.26.0；模块 github.com/forceu/gokapi（fork 不改模块路径） |
| 实测工具链 | Windows PowerShell 无 go；Ubuntu-24.04 WSL: go1.26.0 linux/amd64，GOTOOLCHAIN=auto，gcc 可用 |
| Docker 构建声明 | Dockerfile: golang:1.26.2-alpine → alpine:3.23；生成 WASM/静态资源后 CGO_ENABLED=0 构建；基础镜像没有 digest 固定 |
| 当前部署 / 镜像 digest | 用户确认尚未部署，M0 仅以本地 fork 源码与构建为基线；没有运行中的 VPS/镜像，部署 digest 不适用。Docker 实机构建仍 BLOCKED，见 TEST-REPORT |

[上游快照 JSON](evidence/upstream.json)保存公开 PR、release、advisory 元数据；[本地基线](evidence/local-baseline.json)保存版本、remote 和本轮文件范围；[测试报告](TEST-REPORT.md)记录真实命令。初始 refs 通过单次 `git -c http.proxy= ls-remote https://github.com/Forceu/Gokapi HEAD refs/heads/master refs/tags/v2.2.4` 获取；不修改全局代理或 remote。初始 GitHub connector 缺少 link_id，故使用公开只读 API；后续 PR 阶段已确认 Activer007 连接，使用 connector 创建、审查及合并本 fork PR。

## 本 fork PR 审查增补

M1/SEC-010 当前基线为 `0b72099243e091eec4eb053154c46ce3dbf2fd7a`（本 fork PR #2）。2026-10-27重新确认原有首次放行与通配监听为 CONFIRMED_CODE；重新检索上游 open setup PR，#418/#422/#423/#424/#438不包含同样的初始化证明修复。实现与兼容决策见 [ADR-001](ADR-001-BOOTSTRAP-PROOF.md)，RED和回归见 TEST-REPORT。后续 SEC-011/012/013/014 仍未完成；M1 整体 Gate 尚未通过。

M1/SEC-012（2026-10-28）基线 `c1e0ee2d78c8b8ddb951b5f1053f6d5e15572a31`，即本 fork PR #3 合并提交；本地仅 origin=Activer007/Gokapi，分支 `codex/sec-012-secure-cookies`。用户未部署，保留未跟踪的原计划目录。CONFIRMED_CODE：三类 Cookie 缺少 Secure、网页改密未撤销既有会话，隔离 RED 已证明；最小修复及兼容见 [ADR-002](ADR-002-COOKIE-AND-PASSWORD-ROLLOUT.md)。HISTORICAL_FIXED：当前 Argon2id 与 API/部署密码重置注销已存在，不重复实现。VALIDATE：稳定身份绑定、浏览器 SameSite、并发注销与完整旧版冷恢复不由本轮测试保证。2026-10-28 只读检索上游 open cookie PR 仅返回 [#361](https://github.com/Forceu/Gokapi/pull/361)，其主题为按转发头生成代理 URL，不采用它作为可信传输策略；未见相同待合并修复。公告快照仍沿用下表，不声称本轮完成所有公告复现。完整生成/三组/race结果见 TEST-REPORT，M1仍有SEC-011/013/014待完成。

SEC-062 [PR #1](https://github.com/Activer007/Gokapi/pull/1) 已合并，提交 `1f47d1d91eda7da2098d2820c1633f88942f19f4`；这是 SEC-003 的分支基线。五个继承发布 job 均以仓库全名判断，只在 Forceu/Gokapi 执行，因此即使解除 fork 关系，Activer007/Gokapi 也不会向继承的 registry 发布。测试 workflow 改用 go.mod 指定的 Go 版本。GitHub CI [40529619817](https://github.com/Activer007/Gokapi/actions/runs/40529619817) 通过；逐文件自审已记录，未冒充独立审批。

SEC-003 保持测试与证据范围，补强了 File Request Key 与普通 Key 的双向类型边界，并通过隔离 mutation 验证断言有效；修正证据脚本失败时仍返回成功的退出码。重新检索上游 open PR，相关 #422、#428、#436 不包含该测试基线或发布保护；未移植其改动。生产授权、删除、数据格式均不变。原始 M0 快照与日志不改写，后续测试和 Gate 以 TEST-REPORT 的 PR 审查增补为准。

M1/SEC-013（2026-10-28）基线 `7200526486d7a4b317043fe7056e6d279e1c92f3`，即本 fork PR #4 合并提交。本地 E:\LHA\Gokapi、Go1.26.0 linux/amd64（WSL）、分支codex/sec-013-trusted-proxies，唯一remote仍为Activer007/Gokapi，保留原计划目录。CONFIRMED_CODE：CF先于peer检查、Header Auth无peer限制、Docker自动信任网段默认开启；隔离RED已验证。已检查Logging/Authentication/Environment及既有测试、README、setup/advanced/troubleshooting/SECURITY；只读检索上游open proxy PR返回#361/#428/#437，分别是URL、S3 mock、密码拒绝日志，不是本项修复；CF头issue检索返回#209/#69的历史代理配置讨论，未见相同待合并补丁。沿用公开公告表，不把部署前提下的防御测试命名为新的CVE。共享peer策略及默认值迁移见[ADR-003](ADR-003-TRUSTED-PROXY-BOUNDARY.md)；Docker模式测试不代表真实容器网络验证，M1仍待SEC-011/014。

M1/SEC-014（2026-10-28）基线 `138642c72ea481a2b63d2ed5e1aac071de34a01d`，本 fork PR #5 已合并；本地master干净（除用户原计划目录）、remote仅Activer007/Gokapi，工作分支codex/sec-014-request-bounds。CONFIRMED_CODE：限流Wait使用Background、清理Once回调不返回、HTTP缺少独立头期限；隔离RED分别证明取消后等待、128查询遗留128协程、两处服务慢头超过7秒。检查README/docs、Webserver/Setup/Api/RateLimiter、既有安装/上传/SSE测试；上游open PR的ratelimiter及timeout检索均无结果，未发现相同待合并修复。沿用公告快照，未声明新的CVE或外部部署利用。修复与兼容边界见[ADR-004](ADR-004-HTTP-PHASE-AND-CANCELLATION.md)，Docker非rootGate仍须SEC-011。

## 分类规则

M1收尾证据（2026-10-28）：本fork [PR #8](https://github.com/Activer007/Gokapi/pull/8)代码 `4bc1fc3c8a86c1bc39d6ce92ef2e4db7474280df` 已通过完整本地三组及定向race，并重新归档构建镜像、通过真实非root初始化/上传下载/重启验收；源与镜像摘要、M1六项要求逐项审计见TEST-REPORT。最终PR合并状态以GitHub为准。尚未部署或发布，不能把本地image ID等同可复现发布或生产恢复证明。

M1/SEC-014管理预算补充（2026-10-28）：基线 `863fdb5442e8709e1639dfaa50ea26232eff7ec4`（本fork PR #7合并）；本地master仅保留用户计划目录，分支codex/sec-014-management-budget，remote仅Activer007/Gokapi；WSL Go1.26.0。CONFIRMED_CODE：管理路由没有独立正文/响应预算，OAuth回调Exchange/UserInfo使用全局Background；隔离RED证明/login慢正文超过35秒、取消回调仍联系Token端点。上游open timeout PR只读检索无结果，未找到同项待合并修复；检查README/setup/advanced、Webserver/API路由、OAuth与既有测试、SECURITY及已有公告快照。新增30秒管理I/O/Context预算与精确长传输例外，OIDC仅改取消传递，不改变账户身份绑定。正式版本/Argon2id沿用已核实历史差异，不重复重写。详见ADR-004补充、TEST-REPORT和PR动态状态；不声明新的CVE或全局CPU/存储强制取消。

M1/SEC-011（2026-10-28）基线 `462c5d57dc428ccedb352b2c84c4367331687872`（PR #6合并）；E:\LHA\Gokapi，origin仅Activer007/Gokapi，分支codex/sec-011-nonroot-compose，用户计划目录保留。Docker Desktop4.55.0 / Engine29.1.3 linux/amd64、Compose2.40.3；Windows Python3.13；WSL Go1.26.0。镜像按既有Dockerfile的Go1.26.2-alpine构建。CONFIRMED_CODE：镜像默认root、默认Compose无user，旧DOCKER_NONROOT递归改权限；HISTORICAL_FIXED：已有--user支持及迁移说明。检查README/setup/advanced、两份Dockerfile/entrypoint/Compose、设置解析和HTTP上传测试；上游open non-root PR为空，docker关键词返回#435/#414/#415均无相同修复，历史issues #325/#285/#298已讨论权限；未向上游提交任何内容。新增显式非root配置及真实本地存储/SQLite容器验证，默认镜像USER不变。归档构建遇Windows CRLF shebang失效，新增LF属性并显式关闭archive换行转换。最终镜像ID、完整失败与GREEN证据见TEST-REPORT。原M0镜像BLOCKED是历史记录，本轮已完成本地镜像构建及非rootGate；仍未部署VPS或发布镜像。

- **CONFIRMED_CODE**：该 SHA 的代码足以证明与目标安全不变量的差距；不自动等于已证明的远程利用或新 CVE。
- **VALIDATE**：风险前提、协议行为、并发结果或部署暴露面仍需实验。失败测试设计不等于失败测试已执行。
- **HISTORICAL_FIXED**：公告标明已修或相关修复已进入当前 SHA；仍需回归。须另注明 stable 是否包含。

P0/P1 为本项目修复优先级，不是 CVSS。未证明的 OIDC/CSRF 不认定为漏洞。

## 核心源码证据表

所有链接固定到工作基线 SHA。测试文件与对应实现位于相同目录；只有 TEST-REPORT 中列出的实跑结果才表示测试已执行。

| 边界 / 证据 | 源码事实与分类 | 后续不变量 / 已有测试 |
| --- | --- | --- |
| [启动和版本](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/cmd/gokapi/Main.go#L41) | 先 setup，再配置/DB/加密/auth，启动前 CleanUp(true)；-v 输出构建与 VCS 信息。CONFIRMED_CODE：镜像标识仍需外部 manifest | SEC-001/062；Main_test.go；构建与生成产物必须可追溯 |
| [初装](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/configuration/setup/Setup.go#L96) | CONFIRMED_CODE：isInitialSetup 放行 basicAuth，net.Listen 使用 :port；重配有临时 BasicAuth。#417 的 AWS 测试密码不等于整个初装已有认证 | SEC-010；Setup_test.go 的 TestBasicAuth 明确期望初装放行；新增隔离 RED 不调用真正配置写入 |
| [HTTP 路由与超时](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/Webserver.go#L79) | CONFIRMED_CODE：全服务 read=2h/write=12h，未单独配置 ReadHeaderTimeout/IdleTimeout；上传和 SSE 有长请求需求，不能粗暴统一缩短 | SEC-014；Webserver_test.go，Sse_test.go；连接/内存峰值 VALIDATE |
| [会话 Cookie](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/authentication/sessionmanager/SessionManager.go#L96) | CONFIRMED_CODE：HttpOnly/Lax，无 Secure；内部会话 30 天，RenewAt 12 小时（注释的一小时不同于实现）。每次取当前 User | SEC-012/021；SessionManager_test.go；新增 HTTPS Cookie RED |
| [密码验证](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/configuration/Configuration.go#L211) | HISTORICAL_FIXED（仅 master）：新密码 Argon2id，保留旧 SHA-1 验证和迁移标志。不要重做 #384 | SEC-012；Configuration_test.go 的 legacy/Argon2id/migration 测试；回滚不能只降二进制 |
| [OIDC 回调](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/authentication/oauth/Oauth.go#L89) / [本地关联](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/authentication/Authentication.go#L223) | VALIDATE：检查 state Cookie，Exchange 后调用 UserInfo；本地 getOrCreateUser(email)。非空 subject 检查不能证明稳定身份绑定；回调中未见 ID token Verify，不等于已证明身份冒充 | SEC-022；已有模拟 IdP 使用 fakesig，不能证明签名验证；新增签名 IdP 供后续 issuer/sub/email 矩阵 |
| [登录 CSRF](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/authentication/csrftoken/CsrfToken.go#L16) | 历史登录 CSRF 已修；当前 token 全局 map、类型/5分钟/一次性消费，无浏览器标识字段。跨浏览器与 SameSite/Origin 的实际组合为 VALIDATE | SEC-023；CsrfToken_test.go；不能用 token 单元测试代替真实浏览器登录 CSRF 实验 |
| [Header 身份](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/authentication/Authentication.go#L129) / [代理来源](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/logging/Logging.go#L425) | CONFIRMED_CODE：Header Auth 直接读所配置 Header；Cloudflare 分支先信 CF-Connecting-IP，早于 immediate peer 信任判断；普通 XFF 路径有信任判断。实际源站可达性 VALIDATE | SEC-013；Authentication_test.go、Logging_test.go；前提是启用相应模式且源站可直连 |
| [API 路由](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/api/routing.go#L19) / [鉴权](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/api/Api.go#L1269) | 历史修复已在代码中：API 权限、User 存在、过期和 File Request 类型隔离；各动作另查所有者。不能把“分散判断”直接当成越权漏洞 | SEC-020/021；Api_test.go；M0ActorBoundaries 对 SQLite/Redis 跑真实 API 元数据读取矩阵 |
| [替换授权](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/api/Api.go#L770) / [替换存储](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/FileServing.go#L370) | 历史替换越权已修；CONFIRMED_CODE：覆盖旧 SHA1 后没有在 ReplaceFile 中回收旧内容。deleteNewFile 删除的是新分享条目，不能据此断言旧 blob 已回收 | SEC-041；TestFileReplace；先审 #419，仍需多引用与故障测试 |
| [清理/删除](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/FileServing.go#L813) / [deleteSource](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/FileServing.go#L971) | CONFIRMED_CODE：deleteSource 仅输出错误；CleanUp 随后删除 metadata/hotlink。双层 GetAllMetadata，DeleteFileSchedule 按时间戳复查 | SEC-040/042；FileServing_test.go；新增模拟 S3 失败 RED；多所有者、取消异步删除仍待扩展 |
| [分块](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/chunking/Chunking.go#L206) / [接收](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/webserver/fileupload/FileUpload.go#L68) | 现有负数、offset+size、磁盘空间、每文件上限检查；API processNewChunk 使用 MaxBytesReader。VALIDATE：重复 UUID、并发、未知长度和所有路径累计资源边界；不能声称上传完全无限制 | SEC-030/032；Chunking_test.go、FileUpload_test.go；全局/每用户原子额度未见实现 |
| [File Request](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/filerequest/Filerequest.go#L27) / [模型](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/models/FileRequest.go#L12) | CONFIRMED_CODE：MaxSize 注释是单文件，API min(maxUpload, MaxSize)；文档称整请求总额。历史 MaxSize 绕过已修，累计语义为单独 VALIDATE | SEC-031；FileRequest_test.go、API request 测试；先定义迁移语义 |
| [文件系统](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/filesystem/FileSystem.go) / [S3 driver](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/storage/filesystem/s3filesystem/aws/Aws.go#L86) | 单体本地/S3 选择；AWS SDK v1 静态凭据；DeleteObject 返回错误，吞错发生在上层。awsmock 不模拟真实 SDK 的 HTTP 行为 | SEC-040/061；新增 S3DeleteFailureRecovery 用真实 driver + loopback，非云端 |
| [SQLite](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/configuration/database/provider/sqlite/Sqlite.go#L26) / [Redis](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/internal/configuration/database/provider/redis/Redis.go#L26) | 当前 schema 15/8；升级到 2.2.5 清空会话；Redis 有持久化检查。schema、凭据撤销、文件删除需两个 provider 一致，M0 不迁移 | SEC-012/020/040/063；provider 测试；miniredis 不证明真实 Redis 持久化/崩溃恢复 |
| [Dockerfile](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/Dockerfile) / [entry](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/dockerentry.sh) | CONFIRMED_CODE：创建用户但没有 USER；仅旧 DOCKER_NONROOT 分支切 su-exec。README/compose 默认 localhost 端口，降低暴露面；已有 --user 文档 | SEC-011/062；需要新目录/旧卷 owner 的容器验收，不能直接改变所有权 |
| [setup 文档](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/docs/setup.rst#L309) / [usage](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/docs/usage.rst#L54) | 明示加密未独立审计、Firefox E2E 截断限制、File Request 绕过 E2E；这些限制不能被普通单测消除 | SEC-052；跨浏览器字节完整性 VALIDATE；服务端不能扫描 E2E 明文 |
| [安全政策](https://github.com/Forceu/Gokapi/blob/a9c25d453aab0c25f2d6e6926ec7312628638844/SECURITY.md) | 仅最新 stable 支持；禁止公开 issue 披露未知漏洞。M0 没有向任何仓库发送报告/PR | 真正未知可利用问题的详细复现先受控保留；需要报告时使用 Private Security Advisory，另获发送授权；不公开利用步骤 |

## 上游修复与未合并工作

| 编号 | 实时状态 | 对本 fork 的含义 |
| --- | --- | --- |
| [#384](https://github.com/Forceu/Gokapi/pull/384) | merged，60d00f2，2026-03-20 | Argon2id 在 HEAD，不在 v2.2.4；保留兼容测试 |
| [#400](https://github.com/Forceu/Gokapi/pull/400) | merged，c2880b7，2026-05-06 | 下载计数竞态修复在 HEAD，不在 v2.2.4 |
| [#407](https://github.com/Forceu/Gokapi/issues/407) / [#419](https://github.com/Forceu/Gokapi/pull/419) | issue open / PR open，未合并 | 旧 blob 回收不能被历史替换授权修复代替；SEC-041 先审 patch |
| [#418](https://github.com/Forceu/Gokapi/pull/418) | open，未合并 | CSP 候选；SEC-050 先检查 WASM/下载兼容 |
| [#423](https://github.com/Forceu/Gokapi/pull/423) | open，未合并 | SQL provider 扩展不是 M0 必需，不迁移 PostgreSQL |
| [#424](https://github.com/Forceu/Gokapi/pull/424) | open，未合并 | SDK v2 候选；SEC-061 避免与本地补丁冲突 |
| [#431](https://github.com/Forceu/Gokapi/issues/431) | open | 用户额度需求，未作为 stable 功能 |
| [#436](https://github.com/Forceu/Gokapi/pull/436) | open，未合并 | 首次 File Request 普通用户兼容修复候选 |
| [#438](https://github.com/Forceu/Gokapi/pull/438) | open，未合并 | 默认凭据链候选；当前仍静态凭据 |

另外检查到 19 个 open PR；相关项 #435（AD FS claims）、#437（密码失败审计）、#427（S3 下载报错）、#428（mock proxy）、#422（Windows 测试）。仅审读，不自动合并任意第三方 patch。上述状态不意味着 fork 必须向上游贡献。

## 公开公告的历史修复边界

以下仅依据公开公告的 affected/patched 范围标为 HISTORICAL_FIXED；不声称本轮逐一重新复现 CVE。公告 JSON 保留原始版本范围，当前 SHA 的回归强度见测试报告。

| 公告 | 首个修复版本 | 对应回归范围 |
| --- | --- | --- |
| [GHSA-45vh-rpc8-hxpp](https://github.com/Forceu/Gokapi/security/advisories/GHSA-45vh-rpc8-hxpp) / CVE-2026-30961 | 2.2.4 | File Request 单文件 MaxSize；与累计额度语义分开 |
| [GHSA-qwc6-vc2v-2ggj](https://github.com/Forceu/Gokapi/security/advisories/GHSA-qwc6-vc2v-2ggj) / CVE-2026-30955 | 2.2.4 | API DoS 防回归 |
| [GHSA-j6jp-78w8-34x6](https://github.com/Forceu/Gokapi/security/advisories/GHSA-j6jp-78w8-34x6) / CVE-2026-30943 | 2.2.4 | 替换权限与所有权 |
| [GHSA-m2hx-wjxc-9fp4](https://github.com/Forceu/Gokapi/security/advisories/GHSA-m2hx-wjxc-9fp4) / CVE-2026-29060 | 2.2.3 | 临时 auth token 权限 |
| [GHSA-q658-hfpg-35qc](https://github.com/Forceu/Gokapi/security/advisories/GHSA-q658-hfpg-35qc) / CVE-2026-29061 | 2.2.3 | 降权后 Key 撤权 |
| [GHSA-hcff-qv74-7hr4](https://github.com/Forceu/Gokapi/security/advisories/GHSA-hcff-qv74-7hr4) / CVE-2026-29084 | 2.2.3 | 登录 CSRF；本轮跨浏览器假设仍 VALIDATE |
| [GHSA-3c22-5j5m-4jq7](https://github.com/Forceu/Gokapi/security/advisories/GHSA-3c22-5j5m-4jq7) / CVE-2026-28683 | 2.2.3 | SVG hotlink 禁用/迁移 |
| [GHSA-c36c-7pc2-f2ph](https://github.com/Forceu/Gokapi/security/advisories/GHSA-c36c-7pc2-f2ph) / CVE-2026-28682 | 2.2.3 | SSE 所有者隔离 |
| [GHSA-4xg4-54hm-9j77](https://github.com/Forceu/Gokapi/security/advisories/GHSA-4xg4-54hm-9j77) / CVE-2025-48495 | 2.0.0 | API Key 名称输出转义 |
| [GHSA-95rc-wc32-gm53](https://github.com/Forceu/Gokapi/security/advisories/GHSA-95rc-wc32-gm53) / CVE-2025-48494 | 2.0.0 | 文件名输出转义 |

## 发布限制与回滚

M0 只增加文档、测试和入口脚本，不改公开 API、模板/WASM、密码格式、schema、存储驱动或删除语义；生成在临时副本，未覆盖用户文件。回滚只撤销本轮新增文件，不删除用户的计划目录；原始日志应保留供审计。没有数据库或真实文件需要回滚。

后续 master→stable 切换必须保留升级前 config/database/blob/E2E key 的配套快照。当前 master 的 schema 15/8 和 Argon2id 不能假设可由 v2.2.4 读取；回滚旧二进制要还原对应旧数据库/配置，而非强行降低版本号。

镜像 Gate：隔离构建后保存源 SHA、生成文件差异、Go 版本、build tags、二进制 SHA256、镜像 ID/RepoDigest/平台、扫描结果；在实际部署机器只读比对 `gokapi -v` 和 `docker inspect` 的必要字段，不导出环境或 secrets。尚无 digest 的 latest 不算可复现发布。SEC-062 PR #1 已禁用本 fork 的继承发布 job；以后配置自己的发布目的地、镜像固定与扫描仍需独立 Gate。不要为回滚测试而撤销该保护。

## M2 / SEC-020 增量基线（2026-10-28）

本地路径 `E:\LHA\Gokapi`，分支 `codex/sec-020-authorization-matrix`，基线 `c14e6c73a49a5d5a89d815cf4b660da25cc98d56`；origin仅为Activer007/Gokapi。用户确认尚未部署，运行镜像/digest不适用；保留未跟踪的用户计划目录。WSL Ubuntu24.04，Go1.26.0 linux/amd64；Windows未安装Go。上游只读HEAD仍为a9c25d4，不能把2026-10-27历史快照当作自动更新的版本结论。

- **HISTORICAL_FIXED**：当前替换逻辑检查目标替换权、源读取权及可选删除源的Key/对象权限；临时Token签发及正常降权清理已有历史修复。对应公告见上表。新测试保留这些边界。
- **CONFIRMED_CODE**：基线API入口读取当前用户，但四类管理/替换权限依赖Key位；隔离库注入“用户已撤权、Key仍保留旧位”时，16个请求返回200而目标为401。这是故障状态防御性RED，不等同于已证明外部可利用的新漏洞。新纯策略在每次API入口检查当前用户与Key交集，并集中对象授权规则。
- **VALIDATE**：真实撤销并发、presign消费、公开分享、分片/异步任务、OIDC稳定身份及跨浏览器CSRF仍属M2后续Gate。此次管理矩阵不代表M2全部完成。

已检索README、usage/advanced文档、SECURITY.md、关键实现和现有测试；上游19项开放PR与相关Issues的只读清单见[m2-upstream.json](evidence/m2-upstream.json)。未发现同一集中策略的待合并修复；#435的OIDC回退不能直接采用，#419的旧内容回收不属于本PR。不会向上游提交PR或修改上游设置。

兼容：保留HTTP路由、正常请求的状态码/错误结构、页面/WASM、SQLite/Redis格式、存储与删除流程；无依赖、schema、密码或OIDC身份迁移。无效用户/未知权限默认拒绝，遗留超额Key在使用时返回原有401 Unauthorized。回滚只需撤销本SEC-020代码/测试提交，无数据转换；回滚会恢复旧的Key权限判断，不能视为同等安全。当前无生产数据操作，重大身份/删除方案另设ADR。

## M2 / SEC-021 使用记录与撤销（2026-10-28）

基线bf5918e69827b75b5321acc37efd66f4b7e1b498（SEC-020 PR #9已合并），分支codex/sec-021-revocation；WSL Ubuntu24.04 / Go1.26.0。本地仍无部署，origin只指向Activer007/Gokapi，用户未跟踪计划目录保留。

**HISTORICAL_FIXED**：通过真实管理API降权、移除日志权限、删除用户及删除Key后，后续请求读取当前权限，永久/临时Key与会话回归通过。**CONFIRMED_CODE**：Redis使用记录更新与SQLite合同不同，隔离provider测试证明其可能改写非时间字段/TTL或创建缺失记录。以最小原子单字段更新对齐两种provider；不由provider时序测试宣称已证明完整HTTP攻击链。公开描述只保留数据更新合同和必要修复信息。

本轮只读检索上游开放PR、Redis相关PR/Issues（2026-10-28）；Redis相关PR #423/#180/#245/#243及Issues #413/#264/#209/#179/#198/#185/#39未提供同项使用记录修复。沿用SEC-020已核实的历史降权/临时Token公告结论；不修改上游。关键证据见TEST-REPORT和ACCESS-CONTROL-MATRIX。

无schema、身份标识、密码、公开API、页面/WASM、物理文件删除变更。Redis使用既有Lua能力，单Key脚本参数化前缀/字段/时间，保持TTL；自定义ACL需允许EVAL/EVALSHA及脚本内EXISTS/HSET。现有数据不迁移，回滚代码即可，但会恢复旧更新语义。已存在的异常记录不自动修复，生产数据修复需备份后另评估。presign消费、分片、异步授权、OIDC、CSRF仍为VALIDATE，M2未完成。

## SEC-021 presign补充基线

2026-10-28，d599aabfbc7f52aa06a14a006348992adc94247a（PR #10已合并）；本地E:\LHA\Gokapi，分支codex/sec-021-presign；origin仍为Activer007/Gokapi，用户计划目录保留，尚未部署。WSL Ubuntu24.04 / Go1.26.0 linux/amd64。已检查签发API、presign内存store、登录middleware、消费handler、README/docs/setup保护路径、admin_api/admin_ui_allPages调用及现有测试。上游presign查询仅返回SDK迁移PR #424与文件名Issue #302，未见同项消费授权修复；未写上游。

**CONFIRMED_CODE**：旧管理grant不保留签发主体并且消费不复查对象授权。新合同RED在两种隔离provider各10项状态中返回200；正常下载、管理员读取、ZIP、过期/待删拒绝、匿名拒绝均正常。该证据定义M2需要收紧的管理capability合同，不把合法capability转交自动称为未知漏洞。新增当前主体/Key/逐文件授权及原子一次领取；公开分享路径不改变。ADR-006记录行为兼容、无schema迁移、重启与回滚；全目标结果和未覆盖边界见TEST-REPORT。分片、异步任务、OIDC与浏览器CSRF仍为VALIDATE。

## 2026-10-28 SEC-032 取消预约

基线 8377de414a8a07ab1019397470f9547a206c2715。本人仓库已独立（REST fork=false），PR #11 合并记录仍可访问。CONFIRMED_CODE：取消预约按当前有效预约处理，删除失败保留重试；VALIDATE：完整分片生命周期和在途任务仍待验证。遵循私密安全审查流程，公开内容仅保留必要修复、单元回归和兼容说明；详细条件测试与 RED 证据未放入发布分支。见 ADR-007、TEST-REPORT。
## SEC-023 最小发布集（2026-10-28）

基线 50965ba978aa10972423cea782fab2d72e68cfd8。浏览器表单上下文修复经私密 HTTP/浏览器验证及代码自审，
分类 CONFIRMED_CODE；公开分支仅保留生产修复、必要回归和兼容说明，不携带详细复现历史。
本实例尚未部署；只操作临时测试数据。方案/限制见 ADR-009，真实命令与最终结果见 TEST-REPORT。

## SEC-020 公开能力矩阵补充（2026-10-28）

基线 0d918a1bfa7df96f149de7f380026a504d164f81，E:\LHA\Gokapi，分支 codex/sec-020-capability-matrix，WSL Ubuntu24.04 / Go1.26.0 linux/amd64。本人公开仓库 Activer007/Gokapi 已独立；尚未部署，用户未跟踪计划目录保留。SEC-023 最小修复已合并，生成及四组父仓库 CI 40671571541 PASS，公告未发布。

CONFIRMED_CODE：管理 API 使用 apikey 查数据库并校验凭据种类；公开下载使用文件状态和文件级密码 Cookie。新增实际路由表遍历与三类公开下载 handler 的两种 provider 矩阵，基线通过；隔离故障注入能检出文件绑定/待删检查失效，不作为生产漏洞 RED。没有修改运行时代码、schema、UI/WASM、驱动或公开 API。

已只读检查 README、docs/changelog.rst、docs/advanced.rst、SECURITY.md、Api/routing、GetFile/ServeFile、downloadPasswordToken、现有矩阵及历史公开公告。上游开放 PR #436（Issue #433）涉及首次创建 File Request 的前端额度默认值，#437 是错误下载密码审计，#136 是旧 hotlink 生成问题；未见同范围能力隔离矩阵。初次 gh 搜索参数错误已纠正，不能把该错误当作没有结果。未写入 upstream。

VALIDATE：完整分片/完成/在途及异步任务授权、会话续期/Key 签发并发、旧 hotlink 撤销竞争仍未由本项证明。M2 保持开放；OIDC 评估及迁移方案在独立私密审查中，未实施身份 schema 迁移。

## 2026-10-28 用户修订 M2 部署范围

用户确认个人/可信小团队、单 VPS 单进程，公网下载者和 File Request 上传者仍不可信。已复核 User.go 的三角色/9 用户权限位、ApiKey.go 的10动作位、Policy.go 纯策略和 API 当前权限检查。CONFIRMED_CODE：现有实现无需新增权限引擎；VALIDATE：单进程会话续期/撤销及 File Request add/complete 收尾。目标调整见 [M2-GOAL](M2-GOAL.md)，不删除既有测试，不把延期标成已修复。代码仍为 79a0480；本轮仅改文档，尚未部署。

## SEC-021 单进程续期增量

基线 aff864aa48a77fc795ad91d2f3a4381b8d5afe59；Windows 工作区、WSL Ubuntu-24.04、Go 1.26.0 linux/amd64。CONFIRMED_CODE：隔离会话合同及受控 HTTP 认证交错证明旧续期路径不满足已完成撤销合同；最小修复在门面协调会话变更并条件替换，未修改 provider/schema。详细复现保留私密，不能据此声称生产被利用。HISTORICAL_FIXED：此前 API 权限交集、使用记录和管理 presign 修复保留。VALIDATE：真实 Redis/浏览器、多实例、会话家族、完整登录验证与新会话创建交错；M2 中 File Request add/complete 和最终审计仍开放。

已复核 README、advanced/changelog、SECURITY.md、会话及撤销调用者和既有测试；只读检索上游 session Issues/PR、开放 PR 与公开安全公告，未发现同范围待合并修复。开放 #435 为 OIDC claims 兼容，#436 为 File Request 前端默认额度，均非本次续期修复。只写自己的仓库及未公开审查入口。

## SEC-032 File Request 完成边界增量

基线 a65ecfef82f50e374bc0d3978d5e4dfac239db69；该 SEC-021 合并后 CI 40684368510 已核实 success，详细评估提交未进入 master，公告仍未公开。CONFIRMED_CODE：File Request add 已检查预约归属，complete 的隔离合同发现不一致；最小修复复用内存预约并在任何完成处理前检查。历史取消修复保留。VALIDATE：普通管理分片所有者绑定、完整追加/取消/完成竞争、资源预算及真实后端部署不由本项证明。详细请求复现私密保存，不声称生产被利用。

已复核 README、advanced/changelog、SECURITY.md、API 路由/解析、fileupload、chunking/reservation、filerequest、存储完成与既有测试。只读检查上游 chunk Issues、当前开放 PR 与公开公告，未发现同范围待合并完成作用域修复；#436 为首次 File Request 前端额度问题，旧上传大小公告不等于本项已修复。未向 upstream 写入。

## SEC-032 普通上传作用域修复

生产基线 3d93cdbf7dda7428e96c0ef9f2e5a780b72331df，File Request 完成修复 PR #4 的合并后 CI 40689442018 已通过。CONFIRMED_CODE：普通上传按当前用户隔离服务端临时存储，同一用户有效 Key 可互换；File Request 预约保持原有边界。完成入口、错误清理和进度缓存采用一致用户范围，客户端 UUID 不变。新临时命名与旧 UUID 规范化生成的文件名不相交，不盲目认领旧分片。源码、测试及兼容决策见 ADR-012、TEST-REPORT。

完整私密合同及原始复现不进入公开发布；公开保留最小回归和运行脚本。VALIDATE：同用户在途追加/完成竞态、全局资源预算、真实后端与浏览器环境未由本项穷尽。没有 schema、依赖、UI/WASM、加密或物理文件清理策略变更。上游 #429 仅修改 CLI 错误处理，不是同范围服务端修复；未向 upstream 写入。

## M2 代码验收（2026-10-28）

基线 3a9627c3ede21728688ea13f69937f2c87420ef4，已与测试发布树 64de3b7 比较一致，合并后 CI 40697431982 success。按用户确认的单进程/可信小团队/内置登录参考范围，权限与撤销、公开能力、上传域和 CSRF 的逐项源码/测试/兼容证据通过审计；见 [M2-COMPLETION-AUDIT](M2-COMPLETION-AUDIT.md)。OIDC 仅评估与迁移设计完成，实施延期，不称为安全部署。其余资源、可靠删除、加密/恢复/可复现发布事项保持开放；当前未部署。历史各节的“仍待验证”以审计列明的更新与明确限制为准，不删除旧失败记录。

## M3 / SEC-031 语义基线（2026-10-28）

代码基线 e38b03f。CONFIRMED_CODE：双 provider 的真实 API 合成上传证明 MaxSize=1MiB 可完成两个768KiB文件、总计1.5MiB；usage原累计文案与实现不符，改为每文件。用户确认保留旧字段语义、另增累计限制。请求体开销/未知长度和现有数量/空闲空间/过期控制的实测见 [M3-UPLOAD-SEMANTICS](M3-UPLOAD-SEMANTICS.md)，计量/预约草案见 ADR-013。HISTORICAL_FIXED：单文件历史公告与本项累计额度不同；VALIDATE：累计预算、原子预约、恢复和大文件峰值 Gate 仍未完成。无schema/运行时/旧数据语义改动。

SEC-030纯计算：在SEC-031提交bfd4998上新增uploadquota，按metadata逻辑字节/文件数计算用户和请求用量并检查两维used+reserved+requested及int64溢出。共享blob、加密显示大小、待删/过期不降低用量；当前没有入口调用，不构成并发超卖防护。单测开发RED与最终结果见TEST-REPORT；数据库/schema、预算强制执行和物理容量仍VALIDATE。

2026-10-29 SEC-030持久基础，起点357670c：SQLite16/Redis9增加有界预约账本，当前metadata现算used，事务/CAS检验所有范围并原子提交完成metadata。CONFIRMED_CODE范围及证据见[ADR-014](ADR-014-DURABLE-UPLOAD-RESERVATIONS.md)/TEST-REPORT；没有接入HTTP上传或启用默认额度。旧MaxSize不重释；迁库拒绝活动预约、非空应用目标及部分损坏状态。VALIDATE：真实I/O、策略接入、启动恢复、共享blob物理清理与1–2GB峰值，不能从数据库合同扩大成端到端不耗尽承诺。

2026-10-29 SEC-062测试监听隔离，基线4ad1255：PR #17已合并，PR头CI通过，但合并后CI40726798576两次因setup首次绑定53842失败，不能记为合并后PASS。CONFIRMED_CODE：占住旧端口的隔离合同可复现启动失败；动态持有listener修复及相关race已通过。CI占用者未识别，临时端口范围冲突仅为VALIDATE。此项不是新安全漏洞，未改变生产端口、权限、数据库或上传策略；完整结果见TEST-REPORT。

SEC-030接收增量，基线f4ce5db：[ADR-015](ADR-015-BOUNDED-UPLOAD-RECEPTION.md)记录已接入四类真实上传入口的单进程接收槽、字节/字段/正文边界与自有spool恢复。CONFIRMED_CODE：隔离10/50门控、未知长度、实际HTTP取消及加密工作描述符合同先RED再修复。未据内部资源用例声称未知可利用漏洞。VALIDATE：持久上传总账接线、异步完成/完整区间、共享blob和完整M3大文件矩阵；本地2GiB尝试因WSL只读/服务E_UNEXPECTED失败，后续远端结果以TEST-REPORT及PR为准。PR #18主分支CI40734115453已核实PASS。

已公开工具链公告（2026-10-29核对）：当前复现/CI基线Go1.26.0落在官方[GO-2026-6603](https://pkg.go.dev/vuln/GO-2026-6603)列出的HTTP/2 Trailer内存问题版本范围（修复门槛go1.26.9）内；[GO-2026-6089](https://pkg.go.dev/vuln/GO-2026-6089)另涉及未加密HTTP/2前言读取超时。HISTORICAL_FIXED仅指上游Go已提供修复；VALIDATE为本仓库具体配置/可达调用链及构建工具链推进。不得从receiver测试绿色声称标准库问题已修复，发布前需独立SEC-062工具链/govulncheck Gate。本轮不混入依赖升级。

SEC-030最终接收代码ff7497b：CONFIRMED_CODE并有远端实跑证据，生成/四组全仓/相关race及真实1GiB明文、2GiB服务器加密HTTP上传和读回通过；[43份源码hash与Gate](evidence/sec030-receiver-ff7497b/README.md)匹配该提交。局部默认4槽、12288MiB保守接收预算，不等同用户或File Request累计配额；MaxSize保持单文件。WSL本地BLOCKED及先前失败继续保留。接收前外层认证拒绝的传输等待、持久预算/所有metadata路径、异步完成和恢复仍VALIDATE；无新schema/密码/密文格式变更，未部署或发布。

SEC-062工具链增量（2026-10-29），工作基线cb5868c：官方[release history](https://go.dev/doc/devel/release)及[下载清单](https://go.dev/dl/?mode=json)核实1.26受支持分支当前补丁1.26.10（2026-10-19，含crypto/x509修复）；本轮保持1.26，不做语言主版本升级。CONFIRMED_CODE：根/build go.mod为1.26.0，两个Docker builder为1.26.2、批量builder为1.26.1，CI从根go.mod取版本；已对齐五处声明。go.mod的go行设置最低patch，不仅添加可绕过的toolchain建议行。原build/go.sum不因升级整理；五个上游publisher保护保留，无发布。

HISTORICAL_FIXED（Go上游）：[GO-2026-6609](https://pkg.go.dev/vuln/GO-2026-6609) Range CPU、[6603](https://pkg.go.dev/vuln/GO-2026-6603) H2 Trailer内存、[6611](https://pkg.go.dev/vuln/GO-2026-6611) H2 SETTINGS CPU已在1.26.9修复，[6089](https://pkg.go.dev/vuln/GO-2026-6089)未加密H2 preface超时在1.26.6修复，[6809](https://pkg.go.dev/vuln/GO-2026-6809) x509 denylist在1.26.10修复。CONFIRMED_CODE入口：静态FileServer/自定义ServeFile/本地ServeContent保留Range；UseSsl开启时采用默认ListenAndServeTLS。VALIDATE：实际H2协商/代理、出站TLS及系统根条件，不能仅从API名证明恶意负载可利用。当前没有显式启用h2c；旧x/net checksum不证明生产选择了该模块。没有执行高资源攻击负载或声明新未知漏洞。

当前本地仅版本声明合同RED→GREEN；实际编译器、源码/产物扫描、Docker编译和新补丁大文件Gate须按TEST-REPORT的真实远端结果补充。WSL故障持续，不修复宿主；本地Go生成/测试/扫描NOT_RUN。工具链修补不能代替M3累计配额、持久chunk、异步/恢复。

SEC-062当前代码58d2cb5已验证：[PR #20](https://github.com/Activer007/Gokapi/pull/20)的PR CI40763628844、推送40763619586及手动40763621734全部SUCCESS。CONFIRMED_CODE：实际Go1.26.10、固定govulncheck1.9.0、default/noaws/WASM源码及生成器/服务端/CLI二进制扫描退出0；旧cb5868c/Go1.26.0源码扫描实际退出3且含GO-2026-6609，构成已公开问题的历史RED。三个隔离Docker镜像构建及版本检查、当前compiler的1GiB明文/2GiB加密HTTP读回均通过。[公开证据](evidence/sec062-toolchain-58d2cb5/README.md)含44份源码、11份当前输入/harness、7份旧输入和6份生成产物hash；前三类按各自Git版本核对，生成产物仅记录runner实际hash，不误称Git文件。

已知限制仍保留：扫描报告GO-2026-5932、GO-2022-0646/0635模块级公告，但当前选择的源码/产物未报告受影响可达调用；没有通过忽略列表消音，也不声称依赖无公告。当前安全构建Gate仅接受审查过的1.26.10，go.mod最低行不能替代其他Go分支的公告核验。镜像ID为runner本地结果，未发布、未固定全部base/action digest；本地Go/浏览器交互/新镜像运行验收/OS扫描/生产恢复仍NOT_RUN，M3额度/完整生命周期仍OPEN。

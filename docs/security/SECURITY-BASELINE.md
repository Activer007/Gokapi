# M0 安全基线

研究日期：2026-10-27（Asia/Shanghai）。范围为个人 VPS / 小团队的单体 Go 服务。本文是源码与测试基线，不是安全认证。只向 **Activer007/Gokapi fork** 交付；Forceu/Gokapi 只读，不创建上游 PR、不推送、不触发其工作流。

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

SEC-062 [PR #1](https://github.com/Activer007/Gokapi/pull/1) 已合并，提交 `1f47d1d91eda7da2098d2820c1633f88942f19f4`；这是 SEC-003 的分支基线。五个继承发布 job 均以仓库全名判断，只在 Forceu/Gokapi 执行，因此即使解除 fork 关系，Activer007/Gokapi 也不会向继承的 registry 发布。测试 workflow 改用 go.mod 指定的 Go 版本。GitHub CI [40529619817](https://github.com/Activer007/Gokapi/actions/runs/40529619817) 通过；逐文件自审已记录，未冒充独立审批。

SEC-003 保持测试与证据范围，补强了 File Request Key 与普通 Key 的双向类型边界，并通过隔离 mutation 验证断言有效；修正证据脚本失败时仍返回成功的退出码。重新检索上游 open PR，相关 #422、#428、#436 不包含该测试基线或发布保护；未移植其改动。生产授权、删除、数据格式均不变。原始 M0 快照与日志不改写，后续测试和 Gate 以 TEST-REPORT 的 PR 审查增补为准。

## 分类规则

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

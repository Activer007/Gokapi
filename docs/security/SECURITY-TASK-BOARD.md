# 安全任务看板

> 当前 M2 范围以用户于 2026-10-28 确认的 [简化目标](M2-GOAL.md) 为准：单进程、可信小团队，保留关键授权边界。下列历史矩阵/待办不再全部构成 M2 阻塞 Gate；延期不等于已修复。

初始工作基线：a9c25d453aab0c25f2d6e6926ec7312628638844；2026-10-27。SEC-003 分支基线：1f47d1d91eda7da2098d2820c1633f88942f19f4（SEC-062 PR #1 已合并）。沿用用户现有安全开发计划的 SEC 编号。P0/P1 是工程顺序，分类是证据强度，两者独立。**所有 PR 的 base repository 只能是 Activer007/Gokapi**；上游 PR 链接仅为只读参考。本轮按用户授权创建、审查和合并本 fork PR，不执行发布。

## Gate 定义

- G0：固定 SHA、上游状态、工作树保护、无秘密；证据可重读。
- G1：单项不变量、前提、最小 RED；隔离 fixture 能复现，不能用编译失败冒充 RED。
- G2：最小修复后目标 GREEN；运行相关测试与官方三组；记录 vet/race/govulncheck 的实际范围和失败。
- G3：密码、OIDC、E2E、schema、物理删除须兼容/迁移/回滚评审及故障测试；重大方案有 ADR。
- G4：生成产物、二进制、镜像 digest 与源 SHA 对照；备份恢复和发布目的地核验。

G0/G1 是推进下一轮的基础，不代表 G2–G4 已通过。M0 的 FAIL/BLOCKED 详见 [TEST-REPORT](TEST-REPORT.md)。初始M0仅测试/证据，后续生产实现及ADR以各增补条目为准。

## 当前 M3：最小崩溃恢复与阶段收口

按用户2026-10-29最新指令，后续范围由[M3-GOAL](M3-GOAL.md)冻结，运行时基线60b2fb0。
不再自动推进新功能线；以下之外的原M3债务保留OPEN/VALIDATE，不改成已修复。

| SEC-ID / 子项 | 状态 | PR | 测试 / Gate |
| --- | --- | --- | --- |
| SEC-030-R1 启动时最小有界恢复 | COMPLETE（冻结范围） | [PR35](https://github.com/Activer007/Gokapi/pull/35) | 新合同RED、本地GREEN、最终CI40833366371；确认清理后退款 |
| SEC-030-R2 不确定状态清单与人工runbook | COMPLETE（冻结范围） | 同上 | ADR-021、UPLOAD-RECOVERY-RUNBOOK及脱敏/保留检查通过；不补提交/删canonical |
| SEC-030-R3 三类崩溃验收与文档对账 | COMPLETE（冻结范围） | 同上 | 真实exit97+fresh进程、2个negative与相关race通过；命令/边界见TEST-REPORT |

本轮基线c3058ed；NEW_CONTRACT/STUB_RED不是旧漏洞证据。原head的CI40831437902成功后，
同期PR36改变module/import并产生实际Git冲突，适配后最终head db4dc8a由CI40833366371验证。
候选a78f7be5/tree89276c43；最终收尾只改安全文档/文本，不重跑同代码CI。
无新增大文件/浏览器/真实后端矩阵，无部署和上游写入。延期项仍为OPEN，不能改标HISTORICAL_FIXED。

## SEC-030 文件替换增量：MERGED

PR30合并60b2fb0；CI40823139886 SUCCESS，checkoutdda1f25，tree1325ff65与实际合并一致。
生成、四组全仓、相关race/锁及当前扫描通过；新矩阵只在race步骤运行一次，无重复合并后CI。

基线79f2577；分支codex/sec-030-replace-admission；ADR-020。
G0/G1：仓库/用户改动保护、上游定向检查、5组隔离业务RED已完成；PR419不是相同quota修复。
G2：核心/双provider/锁整包、HTTP5组和干净副本storage整包PASS；原FAIL处理与最终CI见TEST-REPORT/关联PR。
不新增session/schema/UI或物理stage；仅显式拒绝同ID+delete。批量迁移、其他全量写者及物理/恢复仍OPEN。
用户最新方向：本替换项完成后简化并更新后续开发目标，转入“崩溃恢复与阶段收口”；不继续自动扩展功能线。
延期事项将保留风险及限制记录，不能改标为已修复；具体后续目标在本项合并后单独更新。

## SEC-030 单文件owner增量：MERGED

PR27合并79f2577；最终CI40816967130 SUCCESS，候选d129cfb含UI PR26；合并树一致，无重复CI/部署。

工作基线c3244f0；分支codex/sec-030-owner-admission；ADR-019。
G0：本地tracked起点干净，两个用户未跟踪目录保留；上游定向未找到相同owner准入修复。
G1：隔离HTTP4组真实ASSERTION_RED；G2：核心、双provider整包及最终HTTP4组PASS，Linux最终CI待运行。
不重跑未受影响的大文件/浏览器矩阵；最终CI与PR结果在关联审查记录，避免为补文档重复CI。
范围只到 `/api/files/changeOwner`；replace、deleteUser保留文件的批量迁移及物理协调/恢复继续OPEN。

## SEC-030 复制增量：MERGED

PR24合并为c3244f0；最终CI40812061382 SUCCESS，实际候选5d37f97含UI PR23/25，合并树一致。
首轮共享fixture失败修正后通过，完整命令/审查结论见PR24；合并后未重复CI，无部署。

PR22已合并为c657d547，合并后CI40804164086成功且输入匹配。当前复制小增量：
codex/sec-030-duplicate-admission，ADR-018；隔离HTTP ASSERTION_RED及核心/双provider目标GREEN已获得。
核心/双provider整包及最终12组HTTP回归PASS；独立审查的FR跨owner兼容问题先RED后修复。CI及合并状态见关联PR。
G0/G1已完成；G2的Linux CI待最终head一次运行，G4发布/部署NOT_RUN。完整M3仍OPEN。
按用户最新要求，相关定向为主；同一代码只做一次必要完整验证，文档不追加全仓、未改流式路径不跑大文件。
CI按head合并push/PR重复事件；新复制合同只在现有race步骤运行一次，不增加独立大矩阵。

## SEC-030 分片与请求增量：代码 Gate GREEN

起点57e5116，分支codex/sec-030-chunk-admission；整文件PR21已合并，合并后CI40783153865成功。
用户最新确认默认owner5GiB、instance20GiB，其他数量/active/staging不变；按此更新PR22并重新验证。
当前实施普通/浏览器分片及File Request持久准入、受控完成与新私有进度文件，见ADR-017。
干净57 API合同真实RED退出1；新会话桩真实RED退出1；请求策略及会话局部目标已GREEN，
新API完整目标45.622s、旧语义/处理器兼容目标及真实E2E流分段/停服目标已PASS，生成PASS。
本机完整noaws实际FAIL（既知Windows差异及两份旧guard fixture）；修正有效夹具后，全新归档的
完整API（含所有旧/新chunk/whole合同）PASS 48.144s，282份源码hash前后不变，不能标全仓绿色。
143f041的PR CI40798733852及完整手动40798813978实际SUCCESS：四组全仓、相关race、scan、
三镜像及原whole/新chunk 1–2GiB均通过。[PR22](https://github.com/Activer007/Gokapi/pull/22)当前
等待最终文档CI及合并核验，合并状态以PR为准。复现证据见evidence/sec030-chunks-143f041。
两成功job输入202 Go+2harness均匹配Git，50/50变更Go被hash清单覆盖；G0/G1/G2本增量通过，
G3回滚及限定场景验证见ADR017，不等于完整M3/真实生产G4通过。
完整M3及PrepareTarget崩溃对账、其他metadata正向准入不因此关闭；P0下一小闭环建议先做duplicate
同事务准入并与预约竞争，P1继续replace/transfer、物理free模型及真实后端矩阵。

## SEC-030 整文件增量：初期历史记录

以下保留开发初期的失败和当时状态；最终9db2542/PR21的通过结果见TEST-REPORT，不能把这些旧待办当作当前未实施。

工作基线710f4bf，范围仅 `/api/files/add` 的真实长度Reserve/Claim、同步处理、metadata/hotlink/回执Commit及明确清理后的暂存释放，见[ADR-016](ADR-016-WHOLE-UPLOAD-ADMISSION.md)。G1已取得历史隔离源码/新增桩合同的真实断言RED；G2最终目标/全仓/race、G3故障保持/回滚和G4证据整理仍在进行，不预填PR或合并结果。

执行更正：SQLite旧并发夹具因空PublicId唯一列覆盖而401，不计为有效配额RED/GREEN；修订为唯一PublicId并回读所有Key，冻结基线重跑待补。Redis原基线两维10/50争额RED有效。首轮实现target中Redis竞争、双provider共享blobPASS，API整体FAIL；新增blob getter损坏编码/不可用provider合同也有真实FAIL，投影修复及完整GREEN仍待验，详见TEST-REPORT。不能通过删断言或放松认证修复夹具。

本增量选择可配置的有限默认owner20GiB/1000文件/4在途、instance100GiB/5000文件/8在途、staging12GiB，不记为用户逐项批准；用户已确认MaxSize保留单文件并另增累计预算。新错误合同区分413/21永久额度与receiver429/20，不能用接收槽拒绝充当持久quota证明。

仍OPEN的P0：整文件本轮完整验收；分片/File Request累计入口及兼容；复制/替换/所有者转移等正向metadata准入；发布后/不确定Commit的目标对账与可靠恢复。新路径失败保持charge且不删canonical，内部ID不提供跨HTTP幂等；无新schema不代表旧代码可安全理解这些活动记录。Windows便携Go1.26.10可供隔离Gate，WSL仍BLOCKED，不启动宿主修复。完整M3不能因此关闭。

## M0 交付

| ID | 优先级 | 状态 | 证据与验收 | PR |
| --- | --- | --- | --- | --- |
| SEC-001 | P0 | M0 基线完成，发布 Gate 保留 | HEAD/tag/master/module/Docker 声明、公开 PR/公告快照；用户确认尚未部署，部署 digest 不适用；镜像构建 BLOCKED | 随 SEC-003 作为必要证据，不能标记已发布 |
| SEC-002 | P0 | 文档完成 | THREAT-MODEL 的参与者/边界/不变量；每个 P0 有前提、代码引用和可编写 RED | 随 SEC-003 作为测试合同 |
| SEC-003 | P0 | 夹具与实跑记录完成，审查补强 | 三用户、多种 Token、会话、File Request、去重/密码/过期文件、S3 故障、签名 IdP；SQLite/Redis 与 race；opt-in RED；双向凭据类型 mutation | [PR #2](https://github.com/Activer007/Gokapi/pull/2)；最新 CI、审查与合并状态以 PR 为准 |

## 后续任务

每行的“不变量/前提/最小 RED”展开于 [THREAT-MODEL](THREAT-MODEL.md)，固定行链接在 [SECURITY-BASELINE](SECURITY-BASELINE.md)。未执行的测试设计保持 NOT_RUN，不能视作既有漏洞认定。

| ID | 优先级 / 分类 | 状态与风险 | 依赖 / 下一项 Gate / 最小 PR 范围 |
| --- | --- | --- | --- |
| SEC-010 | P0 CONFIRMED_CODE | M1 已合并，RED→GREEN：本地证明、回环监听、单次保存与同源保护 | [PR #3](https://github.com/Activer007/Gokapi/pull/3)；合并c1e0ee2；ADR-001；本地与CI通过，Docker实机Gate仍归SEC-011 |
| SEC-011 | P0 CONFIRMED_CODE / HISTORICAL_FIXED | RED→GREEN：显式非root Compose、权限负例、真实初始化/上传/下载/重启持久化 | [PR #7](https://github.com/Activer007/Gokapi/pull/7)；合并863fdb5；ADR-005；本地镜像/容器Gate与最终CI通过；默认镜像USER保持root，旧生产卷迁移NOT_RUN |
| SEC-012 | P0 CONFIRMED_CODE + HISTORICAL_FIXED | M1 已合并，三类Cookie、网页改密注销、HTTP/TLS代理回调、SQLite/Redis旧密码真实登录迁移 | [PR #4](https://github.com/Activer007/Gokapi/pull/4)；合并7200526；ADR-002；本地回归及最终CI通过；原删除延迟断言一次失败保留；浏览器/冷恢复仍为023/063 Gate |
| SEC-013 | P0 CONFIRMED_CODE | M1 已合并：共享直接peer检查、CF/XFF严格解析、Docker网段显式选择 | [PR #5](https://github.com/Activer007/Gokapi/pull/5)；合并138642c；ADR-003；本地及最终CI通过，真实容器网络Gate仍归011 |
| SEC-014 | P0 CONFIRMED_CODE / P1 调优 | RED→GREEN：取消等待、清理协程有界、慢头、30秒管理I/O/Context、OIDC取消；长正文/SSE正例通过 | [PR #6](https://github.com/Activer007/Gokapi/pull/6)已合并462c5d5；补充[PR #8](https://github.com/Activer007/Gokapi/pull/8)，代码4bc1fc3，本地三组/定向race及最新镜像非rootGate PASS；ADR-004；最终CI/合并以PR为准；100GB/真实代理负载仍VALIDATE |
| SEC-020 | P0 HISTORICAL_FIXED / CONFIRMED_CODE | 用户修订范围内 M2 验收通过：当前授权交集、对象/凭据/公开能力矩阵 | PR #9/#12 已合并；源码、实跑和限制见 [M2 审计](M2-COMPLETION-AUDIT.md) |
| SEC-021 | P0 HISTORICAL_FIXED / CONFIRMED_CODE | M2 验收通过：真实撤销、使用记录、presign 消费、单进程续期 | d599aab / 8377de4 / a65ecfe；ADR-006/010；多实例和会话家族明确延期 |
| SEC-022 | P0 企业场景 VALIDATE | 已有私密双 IdP 评估和迁移建议；实际身份迁移延期，不阻塞内置登录参考部署 | 003/020 → G1/G3；先模拟 IdP + 完整回调，证实后才设计账户迁移 ADR；参考 #435，不冒称漏洞 |
| SEC-023 | P0 HISTORICAL_FIXED / CONFIRMED_CODE | 表单上下文修复经私密 HTTP/浏览器 RED→GREEN 与三组/race 验证；最小发布集已合并 0d918a1，合并后四组 CI PASS | ADR-009；不改变 OIDC 链路，公开回归/限制见 TEST-REPORT |
| SEC-030 | P0 CONFIRMED_CODE / VALIDATE | 整文件API准入9db2542：Linux四组全仓/相关race/扫描/1–2GiB Gate通过；M3其他入口及恢复继续OPEN | [PR #21](https://github.com/Activer007/Gokapi/pull/21)，ADR-016/TEST-REPORT；既有PR16/17/19保留，最终文档head及合并结果另核实 |
| SEC-031 | P0 CONFIRMED_CODE / VALIDATE | 双 provider 顺序实测确认 MaxSize 每文件；用户确认保留并新增累计限制，usage纠正；并发/累计预算未实施 | [PR #15](https://github.com/Activer007/Gokapi/pull/15)已合并8019c61、CI40707671124 PASS；[M3语义](M3-UPLOAD-SEMANTICS.md)、ADR-013；#436非累计/事务修复 |
| SEC-032 | P0 CONFIRMED_CODE / VALIDATE | M2 上传授权验收通过：取消、File Request 完成、普通分片/进度隔离 | 50965ba / 3d93cdb / 3a9627c；ADR-007/011/012；资源预算与完整生命周期仍开放 |
| SEC-040 | P0 CONFIRMED_CODE | 删除错误与元数据生命周期不一致；S3 RED | 003 → G1/G2/G3；先保留记录/传递错误/重试；复杂状态机后置；不把 #419 当相同修复 |
| SEC-041 | P0 CONFIRMED_CODE + HISTORICAL_FIXED | 旧内容回收缺口与历史替换越权是两件事 | 020/040 → G1/G2/G3；先评审 [#419](https://github.com/Activer007/Gokapi/pull/419)，多引用与中断回滚；仅 fork 应用经验证的补丁 |
| SEC-042 | P1 VALIDATE | 双层元数据扫描；真实性能瓶颈待测 | 040/041 → 基准后 G3；先 1k/10k 扫描计数，必要时 Blob 索引，SQLite/Redis 都可重建 |
| SEC-050 | P1 VALIDATE | 全路由响应头/CSP 覆盖待测 | 012/023 → G1；先审 [#418](https://github.com/Activer007/Gokapi/pull/418)，下载/WASM/SSE 兼容，逐步启用 |
| SEC-051 | P1 HISTORICAL_FIXED / VALIDATE | SVG/文件名历史 XSS 已修，其他 MIME 路径需回归 | 020/050 → G1；浏览器执行与 Content-Disposition；E2E 无明文扫描承诺 |
| SEC-052 | P1 VALIDATE，实证严重完整性问题升 P0 | 文档提示 Firefox 截断、File Request 非 E2E | 003/032/050 → G1/G3；浏览器/大小/截断/中断 SHA256；不自制新加密协议 |
| SEC-060 | P1 VALIDATE | 审计 schema 与脱敏覆盖待盘点 | 020/040 → G1；白名单字段和失败结果；先审 #437；日志不含密码、Key、Cookie、完整 capability URL |
| SEC-061 | P1 CONFIRMED_CODE / VALIDATE | SDK v1 静态凭据；云服务行为仍需验证 | 003/040 → G1/G3；先审 [#424](https://github.com/Activer007/Gokapi/pull/424)、[#438](https://github.com/Activer007/Gokapi/pull/438)，SDK 与凭据链分开验收 |
| SEC-062 | P1 CONFIRMED_CODE / VALIDATE | [PR #1](https://github.com/Activer007/Gokapi/pull/1) 已合并：发布保护与 CI Go 声明修正；浮动镜像、vet 告警仍在 | G2 的 CI 子项通过；G4 仍待固定构建清单、扫描、fork 命名空间；不直接发 latest |
| SEC-063 | P1 VALIDATE | 尚未做冷恢复/旧版本回滚演练 | 012/040/052/062 → G3/G4；config/DB/blob/密钥一致备份，在隔离副本恢复并验证分享权限 |

SEC-015..019、024..029、033..039、043..049、053..059 在原计划中未分配：保留编号，不虚构风险或声称这些编号已有验收。以上覆盖原计划 SEC-010 至 SEC-063 的全部已定义任务。

## 小 PR 计划与 M1 顺序

PR 均为 **Activer007/Gokapi ← codex/...**。SEC-062 的先决发布保护已通过 PR #1 合并；SEC-003 承载本轮夹具及其必要基线文档，以下其余功能修复仍是计划：

1. M0/SEC-001：版本与上游快照、测试入口和原始基线结果；SEC-002 文档可独立审核。
2. M0/SEC-003：测试夹具、实际 API/provider/SDK 测试、显式 security_red 入口；默认测试不含故意失败的目标不变量。文档和夹具分提交时必须同时说明各自测试覆盖。
3. M1 P0：SEC-010 bootstrap → SEC-012 Cookie → SEC-013 可信代理；各自先单项 RED、再最小修复、完整回归。SEC-011 非 root 文档/容器 Gate 并行排期，当前环境不能替代容器实测。
4. 下一组 P0：SEC-040 删除可恢复（已有 S3 失败夹具）→ SEC-041；SEC-020/021 权限矩阵；SEC-022/023 只做验证，不能为了交付而将假设升级成漏洞。
5. P0 资源任务：SEC-031 明确合同 → SEC-030 预算 → SEC-032 跨路径边界。P1 顺序为 062 构建/扫描 → 014 调优 → 050/051 → 060/061 → 042/052/063，052 可按证据升级。

每项修复 PR 描述只包含必要问题、行为、测试与兼容信息；未知可利用问题的具体复现不发布到公开 fork PR。提交前再次确认目标 remote、diff、生成变更及秘密扫描。不得借 M0 混入 React/Next、PostgreSQL、数据库重构或真实物理删除。

## M1验收状态（2026-10-28）

六项要求的代码、测试、限制和兼容决策逐项映射于[TEST-REPORT的M1审计](TEST-REPORT.md#m1-逐项验收审计)。最新代码4bc1fc3的完整本地Go回归、定向race和重建镜像的非root初始化/上传/下载/重启Gate均PASS；SEC-010/011/012/013及SEC-014首项PR已合并，最后补充PR #8以最终checks及合并记录为准。该结论不清空本看板其他P0/P1，也不代表G4发布完成。当前尚未部署，所有操作仅在本fork和隔离测试副本中完成。

## SEC-020 能力隔离矩阵增量

2026-10-28，基线 0d918a1。公开 ID/密码 Cookie/session/presign/API Key 权限域与公开下载状态矩阵已新增；两种 provider 基线 PASS，隔离 mutation RED 生效，最终全量/race/PR Gate 见 TEST-REPORT。File Request 正例在认证边界止步，不冒称已验证完整上传/完成流程；跨请求和并发 Gate 继续开放。

本轮按用户确认收缩 M2，收尾只保留单进程续期/撤销、File Request add/complete 作用域和最终审计；完整要求见 M2-GOAL。PR #12 已合并 79a0480，合并后 CI 40675833652 PASS。

## SEC-021 单进程续期收尾

基线 aff864a；私密请求合同 RED 已确认，门面条件续期和撤销协调已实现，相关回归与 merge Gate 见 TEST-REPORT；ADR-010 记录兼容与回滚。状态以最终测试/PR 记录为准，不宣称覆盖多实例或旧 ID 会话家族。M2 后续仅剩 SEC-032 分片 add/complete 作用域及最终审计（需本项 Gate 完成后确认）。

## SEC-032 分片 add/complete 收尾

基线 a65ecfe；已写真实 API 作用域合同并确认 RED，新增最小预约查询与完成入口检查。同步/异步拒绝、到期秒边界及合法完成证据见 TEST-REPORT；兼容与回滚见 ADR-011。详细评估保留私密分支，不另开待合并评估 PR。最终 merge Gate 以唯一修复 PR 和 CI 为准。之后进行 M2 收尾审计；普通管理分片绑定尚属 VALIDATE，不提前宣称所有上传授权均已证明。

## SEC-032 普通上传作用域修复

基线 3d93cdb；最小用户存储隔离及进度隔离已实现。ADR-012 记录升级前完成上传、旧未完成普通上传需重新上传、无数据库/已完成文件迁移和回滚边界。目标合同、三组全量及相关 race 证据见 TEST-REPORT；详细评估仅私密保存。只提交一个最小修复 PR，不另建待合并评估 PR。剩余 Gate：发布提取一致性 → PR 审查/合并及主仓库 CI → 对 M2 修订目标进行最终审计。OIDC 继续按用户决定延期，M2 尚未宣称完成。

## M2 收尾验收（2026-10-28）

SEC-020/021/023/032 在用户修订范围内的代码 Gate 已完成，基线 3a9627c，合并后 CI 40697431982 PASS。证据、跳过项、兼容与原始 A–E 映射见 [M2-COMPLETION-AUDIT](M2-COMPLETION-AUDIT.md)。SEC-022 的双 IdP 评估和 ADR 私密保存，实施继续延期。该收尾文档按独立 PR 审查/合并，不能把自审称为独立批准。

后续 P0 为 SEC-031/030 上传计量/预算及 SEC-040/041 删除可恢复；每项仍先隔离 RED。P1 为发布清单/扫描及恢复演练等；没有开始部署、发布镜像、迁移 OIDC 或清理真实文件。

## M3 上传资源阶段

阶段起点 e38b03f，当前接收代码ff7497b；用户确认个人/可信小团队和MaxSize兼容合同。SEC-031语义、SEC-030计算/持久原语/接收层已分别交付；最终测试/审查/合并以TEST-REPORT和PR为准。以下按最终端到端Gate跟踪，局部完成不等于整项关闭：

- SEC-030：纯逻辑用量/安全加法 → SQLite事务/Redis WATCH-MULTI预约、metadata提交一致性、重启恢复。
- SEC-030：普通/API整文件/浏览器/FileRequest接入，用户和请求累计/在途额度、实例暂存/处理槽，有限默认值及旧数据迁移回滚。
- SEC-032：实收字节、未知长度、极端offset/重复块/重放/断线/取消/过期统一；同步/异步完成有界且幂等。
- 验收：10/50并发抢1额、加密/共享blob、失败最终恢复、API/CLI兼容；真实1–2GB流式正确性/吞吐和内存/暂存峰值。不能用顺序绿色或纯计算证明端到端不超卖。

只向自有仓库提交；新未知可利用问题的详细证据仍走私密安全评估。

SEC-030计算PR仅交付Measure/CanReserve及测试，没有启用额度。独立源码审查未见阻塞，保留“旧0=无限制显式转换、异常metadata失败关闭”接入要求。事务、普通/FileRequest入口、物理预算、重启和流式性能继续OPEN，不能以计算单测覆盖率关闭M3。

2026-10-29持久预约基础：[PR #17](https://github.com/Activer007/Gokapi/pull/17)，代码e018d58，起点357670c，SQLite16/Redis9；有界账本/幂等/完成metadata原子提交、独立连接争额和离线迁移预检已实现，本地生成/合同/三组全量/相关race PASS，最终CI/合并以PR为准。TEST-REPORT及ADR-014记录证据。下一P0仍为策略及普通/整文件/公开上传入口、正向metadata变更、实际I/O清理和启动恢复；未知长度/异常分块、1–2GB峰值全部保持OPEN。基础层不等于生产额度已经启用。

SEC-062测试可靠性增量，[PR #18](https://github.com/Activer007/Gokapi/pull/18)，代码691238b，基线4ad1255：PR #17已合并；合并后CI40726798576 attempt1/noaws、attempt2/integration均因setup固定53842首次监听冲突失败，保留原始证据，不继续盲重跑。隔离占端口RED确认后改用持有的动态listener；生产默认net.Listen不变，保存配置/认证等原断言保留。最终393FfY占配置端口合同/生成/setup/相关race/三组全量全部PASS，见TEST-REPORT；PR审查及合并后CI仍按实际记录。M3运行时额度/清理/性能Gate不因此关闭。

SEC-030接收增量初期记录（最终Gate见下），[PR #19](https://github.com/Activer007/Gokapi/pull/19)，起点f4ce5db、codex/sec-030-bounded-receiver：真实四类入口已共用有界receiver；槽/字节/字段/ownedspool、失败清理与重启、实际payload末块检查及小文件堆上限已实现，见ADR-015/TEST-REPORT。本地环境/入口目标及相关race通过，首轮混合fixture失败保留；1GiB实传通过，2GiB遇本机WSL只读/E_UNEXPECTED。首个远端全量因旧网页3/50字节夹具及错误文案失败，修正后Gate另验证。仍OPEN：ADR-014持久ID接线、用户/请求累计/在途、正向metadata、chunk完整区间、异步完成与完整恢复；不缩小M3目标。

P0 SEC-062追加已公开标准库公告核验：Go1.26.0复现基线不能等于安全发布工具链；GO-2026-6603/6089官方版本与适用条件见SECURITY-BASELINE，下一独立工具链修补/可达性及govulncheck Gate，未运行的扫描不记PASS。保留个人/单进程范围，不以应用层正文预算替代handler之前的协议防线。

SEC-030接收层最终代码ff7497b：[PR CI40751200410](https://github.com/Activer007/Gokapi/actions/runs/40751200410)及[完整手动CI40751191903](https://github.com/Activer007/Gokapi/actions/runs/40751191903)全部PASS，含生成、占端口合同、四组全仓、相关race及真实1GiB明文/2GiB加密HTTP读回。43份实际源码hash逐项匹配，见[最终证据](evidence/sec030-receiver-ff7497b/README.md)；之后仅补文档，不改变代码/运行器/workflow。合并及主分支结果以PR实际记录为准。M3继续OPEN；外层认证/过期请求在进入receiver之前拒绝正文的传输行为仍VALIDATE，后续应先做隔离真实socket合同，不把槽满回归扩大为所有早期拒绝已获证明。

SEC-062当前增量：基线cb5868c、codex/sec-062-patched-toolchain，官方确认Go1.26.10；五处模块/Docker compiler声明对齐，源/产物govulncheck固定v1.9.0且使用真实非零退出码，生成器/WASM/CLI构建及dispatch隔离Docker/历史扫描Gate。静态声明合同实际RED5项→GREEN，实际Go/Docker及扫描仍NOT_RUN，等远端证据；不将文本一致性称为标准库修复已经验证。M3累计预算/完整生命周期仍OPEN；原用户未跟踪目录不纳入提交。

SEC-062工具链本增量Gate已通过：58d2cb5，[PR #20](https://github.com/Activer007/Gokapi/pull/20)，PR40763628844/推送40763619586/手动40763621734均SUCCESS，四组全仓/相关race/实际扫描、历史RED、三镜像构建和当前Go1.26.10的1–2GiB均获证据。只补最终文档后检查新PR CI，合并/主分支以PR真实记录为准。完整发布固定/OS扫描/跨平台/浏览器仍OPEN；下一P0回到SEC-030真实入口持久准入及正向metadata核算，随后SEC-032分块/异步/恢复。不得将此工具链闭环标成M3完成。

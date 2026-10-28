# 安全任务看板

> 当前 M2 范围以用户于 2026-10-28 确认的 [简化目标](M2-GOAL.md) 为准：单进程、可信小团队，保留关键授权边界。下列历史矩阵/待办不再全部构成 M2 阻塞 Gate；延期不等于已修复。

初始工作基线：a9c25d453aab0c25f2d6e6926ec7312628638844；2026-10-27。SEC-003 分支基线：1f47d1d91eda7da2098d2820c1633f88942f19f4（SEC-062 PR #1 已合并）。沿用用户现有安全开发计划的 SEC 编号。P0/P1 是工程顺序，分类是证据强度，两者独立。**所有 PR 的 base repository 只能是 Activer007/Gokapi**；上游 PR 链接仅为只读参考。本轮按用户授权创建、审查和合并本 fork PR，不执行发布。

## Gate 定义

- G0：固定 SHA、上游状态、工作树保护、无秘密；证据可重读。
- G1：单项不变量、前提、最小 RED；隔离 fixture 能复现，不能用编译失败冒充 RED。
- G2：最小修复后目标 GREEN；运行相关测试与官方三组；记录 vet/race/govulncheck 的实际范围和失败。
- G3：密码、OIDC、E2E、schema、物理删除须兼容/迁移/回滚评审及故障测试；重大方案有 ADR。
- G4：生成产物、二进制、镜像 digest 与源 SHA 对照；备份恢复和发布目的地核验。

G0/G1 是推进下一轮的基础，不代表 G2–G4 已通过。M0 的 FAIL/BLOCKED 详见 [TEST-REPORT](TEST-REPORT.md)。本轮无生产行为改动，无需新增架构 ADR。

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
| SEC-030 | P0 CONFIRMED_CODE / VALIDATE | 当前 User 无累计额度字段，未见原子预算；资源耗尽需量化 | 003/020 → G1/G3；先计量语义与预约并发测试，按用户/实例分 PR；参考 issue #431 |
| SEC-031 | P0 CONFIRMED_CODE / VALIDATE | 双 provider 顺序实测确认 MaxSize 每文件；用户确认保留并新增累计限制，usage纠正；并发/累计预算未实施 | [M3语义](M3-UPLOAD-SEMANTICS.md)、ADR-013；#436仅页面默认值，非累计/事务修复；测试/PR Gate见下 |
| SEC-032 | P0 CONFIRMED_CODE / VALIDATE | M2 上传授权验收通过：取消、File Request 完成、普通分片/进度隔离 | 50965ba / 3d93cdb / 3a9627c；ADR-007/011/012；资源预算与完整生命周期仍开放 |
| SEC-040 | P0 CONFIRMED_CODE | 删除错误与元数据生命周期不一致；S3 RED | 003 → G1/G2/G3；先保留记录/传递错误/重试；复杂状态机后置；不把 #419 当相同修复 |
| SEC-041 | P0 CONFIRMED_CODE + HISTORICAL_FIXED | 旧内容回收缺口与历史替换越权是两件事 | 020/040 → G1/G2/G3；先评审 [#419](https://github.com/Forceu/Gokapi/pull/419)，多引用与中断回滚；仅 fork 应用经验证的补丁 |
| SEC-042 | P1 VALIDATE | 双层元数据扫描；真实性能瓶颈待测 | 040/041 → 基准后 G3；先 1k/10k 扫描计数，必要时 Blob 索引，SQLite/Redis 都可重建 |
| SEC-050 | P1 VALIDATE | 全路由响应头/CSP 覆盖待测 | 012/023 → G1；先审 [#418](https://github.com/Forceu/Gokapi/pull/418)，下载/WASM/SSE 兼容，逐步启用 |
| SEC-051 | P1 HISTORICAL_FIXED / VALIDATE | SVG/文件名历史 XSS 已修，其他 MIME 路径需回归 | 020/050 → G1；浏览器执行与 Content-Disposition；E2E 无明文扫描承诺 |
| SEC-052 | P1 VALIDATE，实证严重完整性问题升 P0 | 文档提示 Firefox 截断、File Request 非 E2E | 003/032/050 → G1/G3；浏览器/大小/截断/中断 SHA256；不自制新加密协议 |
| SEC-060 | P1 VALIDATE | 审计 schema 与脱敏覆盖待盘点 | 020/040 → G1；白名单字段和失败结果；先审 #437；日志不含密码、Key、Cookie、完整 capability URL |
| SEC-061 | P1 CONFIRMED_CODE / VALIDATE | SDK v1 静态凭据；云服务行为仍需验证 | 003/040 → G1/G3；先审 [#424](https://github.com/Forceu/Gokapi/pull/424)、[#438](https://github.com/Forceu/Gokapi/pull/438)，SDK 与凭据链分开验收 |
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

当前基线 e38b03f，用户确认个人/可信小团队和MaxSize兼容合同。SEC-031完成当前语义实测与文档校正；最终测试/审查/合并以TEST-REPORT和PR为准。以下全部是M3剩余Gate，未关闭：

- SEC-030：纯逻辑用量/安全加法 → SQLite事务/Redis Lua预约、metadata提交一致性、重启恢复。
- SEC-030：普通/API整文件/浏览器/FileRequest接入，用户和请求累计/在途额度、实例暂存/处理槽，有限默认值及旧数据迁移回滚。
- SEC-032：实收字节、未知长度、极端offset/重复块/重放/断线/取消/过期统一；同步/异步完成有界且幂等。
- 验收：10/50并发抢1额、加密/共享blob、失败最终恢复、API/CLI兼容；真实1–2GB流式正确性/吞吐和内存/暂存峰值。不能用顺序绿色或纯计算证明端到端不超卖。

只向自有仓库提交；新未知可利用问题的详细证据仍走私密安全评估。

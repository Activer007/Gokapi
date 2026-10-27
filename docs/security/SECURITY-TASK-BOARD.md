# 安全任务看板

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
| SEC-011 | P0 CONFIRMED_CODE | 默认镜像未 USER；实际部署 uid VALIDATE | 001 → Docker G1 BLOCKED；先 fork 部署文档/非 root 示例，旧卷迁移独立，不自动 chown |
| SEC-012 | P0 CONFIRMED_CODE + HISTORICAL_FIXED | M1 本地RED→GREEN；三类Cookie、网页改密注销、HTTP/TLS代理回调、SQLite/Redis旧密码真实登录迁移 | [PR #4](https://github.com/Activer007/Gokapi/pull/4)；基线c1e0ee2；ADR-002；generate/官方三组/定向race通过，已自审；最终CI/合并状态见PR；浏览器/冷恢复仍为023/063 Gate，不重复实现#384 |
| SEC-013 | P0 CONFIRMED_CODE | Header/CF 信任入口，前提是模式开启且源站直连 | 002/003 → G1；仅可信 peer 判定及配置兼容；不把日志来源伪造等同已证实账户接管 |
| SEC-014 | P0 验证 / P1 调优，CONFIRMED_CODE | 长全局超时，等待使用 Background；DoS 强度 VALIDATE | 003 → G1；先取消上下文或头超时一个子项，保留上传/SSE 正例 |
| SEC-020 | P0 HISTORICAL_FIXED / VALIDATE | 既有越权公告已修；集中策略只是方案，未证明需重构 | 003 → G1/G2；先补动作矩阵；仅遇到实证不一致才抽最小共同策略 |
| SEC-021 | P0 HISTORICAL_FIXED / VALIDATE | 历史降权/临时 token 已修；所有撤销路径仍需矩阵 | 020 → G1；降权/删除/重置后的即时权限交集，不改无关用户 UI |
| SEC-022 | P0 企业场景 VALIDATE | 邮箱关联、issuer/sub/claims 完整策略未验证 | 003/020 → G1/G3；先模拟 IdP + 完整回调，证实后才设计账户迁移 ADR；参考 #435，不冒称漏洞 |
| SEC-023 | P0 验证 VALIDATE | 历史 CSRF 已修；跨浏览器绑定疑点 | 003/012 → G1；真实浏览器/Origin/SameSite 矩阵，未经实证不替换认证框架 |
| SEC-030 | P0 CONFIRMED_CODE / VALIDATE | 当前 User 无累计额度字段，未见原子预算；资源耗尽需量化 | 003/020 → G1/G3；先计量语义与预约并发测试，按用户/实例分 PR；参考 issue #431 |
| SEC-031 | P0 CONFIRMED_CODE / VALIDATE | 单文件实现 vs 总额文案；累计绕过未做攻击认定 | 003，语义定案后接 030 → G1/G3；连续/并发请求实测再选兼容方案；#436 先评审 |
| SEC-032 | P0 VALIDATE | 已有 body/offset/空间检查；跨请求资源与 chunk 所有权待证 | 003/030 → G1/G2；逐入口长度、offset、取消、UUID 复用测试；并发修复需 race |
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

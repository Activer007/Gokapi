# ADR-002：对外 HTTPS Cookie 与密码迁移发布边界

日期：2026-10-28；SEC-012；基线 `c1e0ee2d78c8b8ddb951b5f1053f6d5e15572a31`。范围仅 Activer007/SupaShare；当前尚未部署。

## 决策与兼容

以部署者配置的 `ServerUrl` scheme 为 Cookie 传输策略来源。HTTPS（含反代终止 TLS、后端 HTTP）为会话、下载密码及 OAuth state Cookie 设置 Secure；显式 HTTP 开发仍可登录。不根据客户端可提供的 X-Forwarded-Proto 或后端 r.TLS 决定该属性。部署者必须将 Public Facing URL 配为真实外部 HTTPS URL。

保留 HttpOnly、session/state 的 SameSite=Lax，以及下载密码的 Strict；不改 Cookie 名称、路径、期限或 OIDC 账户关联。Lax 保持外部 IdP 顶层 GET 回调兼容。新测试使用 HTTP/TLS 代理和 CookieJar；不等同实际浏览器 SameSite/CSRF 验证。

网页改密成功后撤销该用户全部会话，清空当前浏览器 Cookie 并要求重新登录。错误表单不改密码、不撤销会话；其他用户不受影响。API 重置密码及部署密码重置已有注销路径，不重复改写。此处不声称实现并发密码变更/登录的事务性全局撤销；相关竞态矩阵仍属 SEC-021。

既有会话有效期仍为内部登录30天、每12小时续期；OAuth 按配置重查间隔计算到期。当前没有独立的首次签发绝对期限，新增它需要单独兼容设计；本 PR 不调整 schema 或 Token/API Key 生命周期。

## 升级步骤

1. 保持维护窗口，停止写入；备份整个 config、SQLite 或 Redis 持久化快照、文件数据与加密材料，记录源版本/镜像 digest。备份中含密钥，不能提交仓库。
2. 在隔离副本确认对外 URL、代理 TLS、初装证明、登录/回调/下载正常，再部署固定构建。不要将上游稳定版镜像当成包含本 fork 修复。
3. Cookie 属性只影响新发出或续期的 Cookie，不能追溯修改浏览器中已有 Cookie。既有部署须在受控维护窗口通过已有重配流程注销全部会话（`configuration.LoadFromSetup` 已调用 `database.DeleteAllSessions`），让用户重新登录；不要等待滑动续期作为上线验收。HTTP→HTTPS 切换也必须完成这一步。
4. 保留旧全局 salt，直到所有 legacy 密码完成迁移。检查成功登录升级、错误密码拒绝，以及重启后可登录，再解除维护。无须强制用户提前改密。

## 历史哈希与回滚

**HISTORICAL_FIXED**：v2.2.4（`9a4b289c604198c05589e34314e889d2c25386d8`）使用 SHA-1(password + 全局 salt)；当前开发线已由上游 #384 实现 Argon2id。真实认证路径仅在 legacy 密码校验成功后写回 Argon2id。本 PR 只补 SQLite/miniredis 持久化与恢复测试，不重新实现算法。

SEC-012 本身不改密码格式、schema、文件内容或身份绑定；回滚本项代码无数据转换，但会失去新增 Cookie/会话保护。回滚至 v2.2.4 是另一项操作：旧程序不能直接消费已升级 Argon2id 记录或假定兼容开发版 schema。须停写并恢复升级前相互匹配的配置、数据库、文件和密钥；不能将 hash 逆转换成 SHA-1，也不能只替换二进制。

测试证明单条合成 legacy 记录成功迁移、数据库重连后持久化及恢复原记录可校验；尚未完成旧二进制的全量冷恢复演练。真实 Redis 持久化、容器/浏览器及发布恢复 Gate 仍须 SEC-011/023/063 完成，不以单测替代。

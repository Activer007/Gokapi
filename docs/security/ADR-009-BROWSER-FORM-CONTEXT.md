# ADR-009：浏览器表单上下文（SEC-023）

2026-10-28，基线 50965ba978aa10972423cea782fab2d72e68cfd8。

内部登录和改密表单 Token 绑定独立的 HttpOnly 浏览器 Cookie，并使用不同用途类型。
服务端在同一锁内验证绑定、类型、五分钟有效期并单次消费；空或不匹配的绑定拒绝。
不同绑定不会消费原 Token，旧无绑定 Token 不可通过浏览器入口。

Cookie 复用 40 字符随机上下文以兼容多标签页，寿命 30 分钟并在渲染时刷新；表单响应 no-store。
外部 URL 为 HTTPS 时使用 __Host-gokapi_csrf、Secure、Path=/、无 Domain；HTTP 开发模式使用 gokapi_csrf。
两者均为 HttpOnly/SameSite=Lax。Cookie 按主机隔离而非端口，不能把同主机恶意服务视为隔离信任域。
绑定校验不替代密码、当前会话或资源权限检查，也不是 Origin 白名单。

页面字段、公开 API、API Key、OIDC callback、session Cookie、密码哈希和数据库 schema 均保持兼容。
旧的已打开表单升级后需刷新；脚本使用内部登录页时应先 GET 并保留 Cookie 再 POST。
现有已登录会话不因本改动失效，成功改密仍撤销该用户的全部会话。

无需数据迁移。回滚代码并重启即可，新上下文 Cookie 会自然过期，旧版忽略它；回滚也会恢复旧表单上下文语义。
没有对真实密码或生产数据进行操作。外部 HTTPS 配置必须正确；HTTP 模式不保证对抗网络攻击者。

完整 HTTP/浏览器对照和相关回归已在私密审查中执行。公开测试覆盖绑定/类型/期限/并发消费、Cookie 属性及多标签页，
并通过既有登录、改密和 OIDC 回归；详细复现不随公开提交发布。未覆盖边界见 TEST-REPORT。

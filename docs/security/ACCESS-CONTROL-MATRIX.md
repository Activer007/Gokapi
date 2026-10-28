# M2 访问控制合同

2026-10-28 验收索引：[M2-COMPLETION-AUDIT](M2-COMPLETION-AUDIT.md)。以下保留合同与历史推进记录；当前已完成项、明确延期和未覆盖边界以上述逐项审计为准。

> 当前 M2 范围以用户于 2026-10-28 确认的 [简化目标](M2-GOAL.md) 为准：单进程、可信小团队，保留关键授权边界。下列历史矩阵/待办不再全部构成 M2 阻塞 Gate；延期不等于已修复。

基线：`c14e6c73a49a5d5a89d815cf4b660da25cc98d56`，2026-10-28。先记录合同及现状测试，再实施策略。下列“允许”均要求资源存在、未过期，凭据仍有效；表格不是测试通过声明。结果以TEST-REPORT为准。

## 主体、权限与对象的交集

管理API必须同时满足：当前数据库用户存在、Key未过期/撤销、凭据种类匹配、Key具有动作权限、用户当前具有对应权限、对象属于用户或具有该动作的跨所有者委托权限。角色本身不替代细粒度权限；管理员通常被赋予全部权限，但不能越过Key权限或超级管理员保护。未知动作默认拒绝。

| 动作 | API Key位 | U1对自己的文件 | U1对U2文件的额外用户权限 | 匿名/公开File Request Token |
| --- | --- | --- | --- | --- |
| 列表/单条元数据 | View | 允许 | ListOtherUploads；列表过滤 | 管理API拒绝 |
| 管理下载、ZIP、presign签发 | Download | 允许 | ListOtherUploads；ZIP每个成员都必须允许 | 管理API拒绝 |
| 修改 | Edit | 允许 | EditOtherUploads | 拒绝 |
| 复制 | Upload | 允许 | ListOtherUploads；复制品归调用者 | 拒绝 |
| 替换目标 | Replace | ReplaceUploads | ReplaceUploads及ReplaceOtherUploads | 拒绝 |
| 替换源 | Replace | 允许读取自有源 | ListOtherUploads | 拒绝 |
| 替换时删除源 | Replace及Delete | 允许 | DeleteOtherUploads（仍须源读取、目标替换权限） | 拒绝 |
| 恢复/删除 | Delete | 允许 | DeleteOtherUploads | 拒绝 |
| 修改所有者 | Edit | 管理员且EditOtherUploads | 同左 | 拒绝 |
| 用户管理 | ManageUsers | ManageUsers；保留不能改自身/超级管理员、不能授予自己没有的权限等限制 | 同左 | 拒绝 |
| 审计读取 | ManageLogs | ManageLogs | 不适用 | 拒绝 |
| 删除日志/重置统计 | ManageLogs | 管理员且ManageLogs | 不适用 | 拒绝 |

永久Key、由会话生成的临时Key适用同一交集。正常降权通过现有API执行，既有会话和Key在下一次请求重新读取当前权限；已删除用户、已撤销/过期Key不能继续访问任何管理动作。另测试故障/并发遗留的旧权限Key，不把直接注入数据库的状态自动认定为可利用漏洞。已开始的请求与权限撤销的并发线性化需单独证明，不能用顺序测试冒充。

## Capability 与File Request

公开分享URL本身授予该文件的有限下载能力，不要求下载者拥有该文件；密码、到期、下载次数、待删状态及File Request文件不可公开下载的规则仍适用。不能将分享ID作为管理Key。公开File Request Token只能操作其绑定请求的上传/预约入口，不能进入普通管理API；普通Key也不能伪装该Token。

短期presign通过授权管理API签发，消费入口当前要求登录。需验证消费时的用户/Key撤销、对象权限变化、一次性消费与ZIP逐成员检查，不能只证明签发时检查。文件分片UUID与所有者/请求绑定、异步任务同样属于M2授权边界；资源用量另归SEC-032。

## 逐步交付与未完成范围

- SEC-020：先覆盖超级管理员、管理员、U1/U2、永久/临时Key、会话换取Token、File Request Token、匿名者的逐动作矩阵及替换三重边界；再提取最小纯策略并复用入口。
- SEC-021：真实降权/权限撤销/删用户/删Key、当前权限交集及异步/跨请求边界。无数据格式变化优先。
- SEC-022：两个签名IdP、相同邮箱/不同subject、改邮箱、AD FS缺失email的真实回调；独立ADR记录issuer+sub迁移、备份及回滚。不采用上游#435跳过签名/audience验证的回退。
- SEC-023：跨浏览器CSRF验证须包含真实浏览器/Origin/SameSite与OIDC正例；进程内token可移植只是一项事实，不能单独当作新增漏洞结论。

矩阵测试禁止读写生产数据。删除负例必须验证元数据和文件未变；正例异步物理删除应在专门隔离测试中完成，不能泄漏后台清理到下一个数据库夹具。M2在上述各项都有直接证据之前保持未完成。

## SEC-021 已增加的验证

真实API对管理员执行降权、单项日志撤权、删用户（合成文件转交而不物理删除）、删永久Key，然后使用原永久/临时Key发起新请求。降权/删除主体/删除Key后，单条元数据、下载、ZIP、presign签发、修改、替换、复制、恢复、删除拒绝；降权用户列表仅保留可读对象。会话重新读取权限，删用户会话拒绝；仅删除一把Key不隐式撤销另一把独立Key或用户会话。混合所有者ZIP及其presign在两种输入顺序下整体拒绝，不输出ZIP或downloadUrl。

使用记录合同在SQLite和miniredis中比较全部Key字段及Redis TTL，并交付删除前保留的延迟快照；时间更新不能撤销管理员已完成的变更。并发用屏障释放更新/删除并等待双方完成。此证明限于使用记录写入，不替代会话续期、Key权限修改/签发、公开能力消费及异步任务的并发测试。

## SEC-021 管理presign消费合同

在签发矩阵之外，消费检查当前签发Key/User、当前下载者及每个文件。有效所有者、有读取权的其他管理员、临时Key与ZIP仍允许；其他无读取权用户、已撤销/过期/失去Download位/转为File Request的Key、删除主体、委托撤销、对象转移、缺失签发绑定均拒绝。失效grant采用既有400响应，匿名入口401。

拒绝不能输出任何文件或ZIP，也不能消耗仍可由合法用户使用的grant。顺序重放拒绝；并发单文件消费只有一个200，存储级32并发领取者仅一个成功。以上以TEST-REPORT最终实跑结果为准；完整浏览器会话、S3外部URL和在途元数据修改仍未由此矩阵证明。兼容与回滚见[ADR-006](ADR-006-PRESIGN-AUTHORITY.md)。

## SEC-020 公开凭据边界补充合同（2026-10-28）

基线 0d918a1。测试先于结果：分享 ID、hotlink ID、密码下载 Cookie、presign、session ID、Key 的 PublicId、File Request ID 均不得作为 apikey 进入任何 API 路由；File Request Key 仅可通过公开上传路由认证。遍历真实路由表并保留合法永久/临时 Key 和 File Request 认证正例。

公开分享授予有限读取能力，不受下载者所有权限制；普通下载、带文件名下载和 hotlink 均拒绝过期、耗尽、待删、缺失、File Request 文件。受密码保护的下载仅接受匹配文件的有效密码 Cookie，管理 Key、session ID 和 File Request Key 不能替代密码 Cookie。拒绝不修改文件元数据或物理内容。

本项只验证顺序消费，不证明在途元数据变更、旧 hotlink 撤销竞争、S3 外部 URL、分片完成或异步任务已满足授权合同。结果记录到 TEST-REPORT；如基线通过，以隔离故障注入证明负例有效，不伪造生产 RED。

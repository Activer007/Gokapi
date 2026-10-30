# M5-FIX-02：首次 File Request 额度初始化

2026-10-30。本地验收 PASS；独立 PR 与其最新 head 自动 review 以文末交付记录为准，不自动合并或发布。

## 基线、范围与根因

master 仍为 `77b8fa2c729db05a5e2ae3434b50f02eda127def`，#56/#57/#58 已合并。FIX-01 的 [PR #59](https://github.com/Activer007/SupaShare/pull/59) 保持 OPEN，head `29367b5ebdf951bc013bbe14dd752f4b6f328396` 自动 review completed/no findings。本包从该已审提交建立 `codex/m5-fix-02`，PR 基于 `codex/m5-fix-01` 以保留共用测试/文档；仅本包差异进入审查。没有代替用户合并 #59 或覆盖其他工作树。

源码/工具链基线：Go 1.26.10 linux/amd64、CGO=1、WSL Ubuntu 24.04 原生文件系统、非 root；Node 24.16.0、Windows Chrome 与已安装 Playwright。已复核当前工作树、remote、主线和 PR。Actions 保持 `enabled=false`，本包不运行 CI，旧 CI 不作为新候选证据。

**已确认的生产视图缺陷**：[AdminView.convertGlobalConfig](../../internal/webserver/Webserver.go) 将普通用户 `FileRequestMaxFiles/Size` 初始化放在已有收集循环内，并在 owner 存在及可见性过滤之后。空库或没有可见收集时，页面输出 `limitMaxFiles/Size=0`。表单因而初始化为不限量，首次保存发送 0，API 按真实 guest cap 拒绝（HTTP400 / AdminOnly）。管理员/超级管理员的 0 是既有豁免，不是本问题。

目标只修复首次页面策略投影；不放宽保存/上传 API，不修改 FR policy、授权、密文、schema、历史 ID、兼容环境变量或配额 ledger。MaxSize 仍是单文件 MB，不重新解释成累计字节。

## RED 与最小修复

新增 [TestCollectionInitialLimits](../../internal/webserver/collection_initial_limits_test.go)，直接执行真实 `convertGlobalConfig → renderPage("uploadreq")`，不手填 view 限额来掩盖初始化问题。矩阵为 SQLite/miniredis × 四种上限（两项有限、各自 0、全 0）× 空/其他 owner 不可见/孤儿/可见 own × 普通/admin/superadmin，共 **96** 组。

不可变 `29367b5` 归档加该测试得到业务 **RED exit1 / 1.733s**：18 个普通用户空/过滤/孤儿有限策略组合失败，78 个对照通过；view 和实际模板中的数值都错误为 0。独立 native RED 夹具仅将 TestConfiguration.go 两处固定回环端口 `53843` 改为 `0`，没有 HTTP 客户端依赖该端口；适配前后哈希留在本地证据中。最终 GREEN 全组使用未改端口的真实受测源码，未把端口适配提交到仓库。

真实 Chrome 同时取得业务 RED：全新普通用户、空列表、无 localStorage 偏好，打开新建表单为 0/0，保存 HTTP400 / ErrorCode3 / AdminOnly，未新增收集；管理员与超管 0/0 对照正常。

生产修复仅将原 `if !user.IsAdmin()` 两项赋值移到 `ViewFileRequests` 的循环之前，仍沿用原角色判断与配置。列表循环只负责 owner/权限过滤、排序及追加，未改变请求可见性。新增 Node 合同验证限额传入表单后的锁定、提交参数、各项 0 和已有有限偏好；不修改运行时 JS，因此生成资源不需更新。

新增 [TestCollectionInitialPolicyAPI](../../internal/webserver/api/collection_initial_policy_test.go) 的 27 个真实 handler 用例，证明普通用户省略/0/超限请求仍被拒绝且不新增 FR/key，恰好限额可创建，admin/super 可豁免 guest cap，每项 0 独立生效。成功仍核对 owner 与 scoped key 绑定；此安全合同在基线已 GREEN，不声称为生产漏洞 RED。

## 最终源码与本地验证

最终受测代码为 `9c18a42d834531290e426863486ad37e85f3f3e7`。仅一个运行时 Go 文件的初始化位置变更，其他为测试；随后只补文档/证据。完整命令与结果见 [results.tsv](evidence/fix-02/results.tsv)，源码与归档哈希见 [source.json](evidence/fix-02/source.json)。

| 实际验证 | 结果 |
| --- | --- |
| `go generate ./...` + tracked diff guard | PASS；资产无漂移 |
| `go test ./internal/webserver ./internal/webserver/api -tags=test,noaws -run '^TestCollectionInitial(Limits\|PolicyAPI)$' -count=3 -timeout=180s -v` | PASS；视图 96×3、API 27×3 全部通过 |
| 相关七个包的 race，选择新合同、CollectionPolicy defaults、SEC030RequestEnvironment/Policy、SEC031UploadSizeSemantics、UIUploadManagementContexts | PASS；详细包/regex 见 results。filerequest 包为 `[no tests to run]`，不将其当作专用 reservation-display 合同已执行 |
| `go test ./... -tags=test,noaws -count=1 -timeout=300s` | PASS，wall84s；含实际 Web UI/HTTP 和 FIX-01 的 API 删除生命周期回归，不包含其额外安全 tags/SDK 全套 |
| 相关 webserver/API/SQLite/Redis，`test,awsmock` | PASS，wall3s；所列 filerequest 包同样无匹配专用测试 |
| 默认及 `-tags=noaws` server build | PASS / PASS |
| `node --test build/tests/*.test.cjs`（实际展开文件名） | PASS 51/51，0 skip |
| 冻结输入逐文件 SHA256 前后复核 | PASS；最终提交归档与受测副本逐字节一致 |

早期 SETUP_FAIL 单独保留、不计 RED：冷归档缺 ignored WASM；固定测试监听端口冲突（未终止任何未知进程）；私有浏览器 helper 曾将 GetEnvironment 的值副本当可写指针；API 新夹具空 PublicId 触发唯一约束替换导致401，修为各自随机 PublicId 后目标/race均 GREEN。最终验证未使用这些错误夹具，也未放宽生产约束。

## 浏览器 QA

流程：登录 → 空收集列表 → 新建表单 → 默认额度 → 保存 → API200/新行 → 刷新持久化 → 窄桌面新建表单。Browser plugin 不可用，按 frontend-testing-debugging 流程使用现有 Playwright/Chrome，不安装新依赖。

仅在隔离合成数据服务 `http://127.0.0.1:53924` 验证；私有 TestMain fixture 的测试监听端口改为53924，设置 guest 7文件/13MB、默认中文。产品 Webserver.go 的规范化哈希与最终提交一致；辅助 Go 文件、端口适配、生命周期控制及截图均在仓库外，不作为运行时代码提交。

| QA 检查 | 结果 |
| --- | --- |
| 页面 URL/title/实际内容，非空、无框架错误覆盖 | PASS |
| 普通用户首次空列表，fresh localStorage | PASS；7/13 已启用、不可取消、input max 正确 |
| 管理员/超级管理员空列表对照 | PASS；仍0/0，可自行启用上限 |
| 点击实际保存及刷新 | PASS；HTTP200、maxfiles7/maxsize13/downloads1，恰好1行且刷新仍在 |
| 1440×1000 与700×950桌面窗口 | PASS；表单可用，无水平溢出；保持桌面 UA，不更改移动访问策略 |
| console/pageerror、截图及交互证据 | GREEN 无相关错误；RED 的400与AdminOnly日志是预期故障证据。已检查宽/窄截图 |
| 结束清理 | 两个 preview 及各自的所有浏览器 context/Chrome 已结束；未部署服务 |

浏览器 JSON、截图和完整原始日志保留于本次聊天 artifact 的 `m5-fix-02` 目录；仓库仅保留脱敏小摘要，见 [browser.json](evidence/fix-02/browser.json) 和 [RED 摘要](evidence/fix-02/red-summary.txt)。不将一次 Chrome 合成场景外推为所有浏览器、真实后端或生产环境保证。

## 兼容、回滚与交付

无数据迁移或新配置；旧收集、普通/管理员下载计数、5天/1次默认、权限与服务器上传边界保持。回退该变更无需调整数据库，只会重新出现空列表初始化问题；不涉及恢复已删除内容。已保存但超过当前服务器上限的旧 localStorage 偏好仍走现有校验，本包不额外重设用户偏好。

真实 Redis 持久化/云 S3、安装升级、实际备份恢复/回滚演练、RC、发布/部署均 NOT_RUN。FIX-03/REL-01 尚未开始，M5 总目标继续 ACTIVE。候选 full-noaws 通过不代表 master 已包含本包，合入后的最终 master 验收须单独执行。

独立 [PR #60](https://github.com/Activer007/SupaShare/pull/60)，base 为 `codex/m5-fix-01`；code SHA `9c18a42d834531290e426863486ad37e85f3f3e7`。最新 head 的自动 review 与精确交付 SHA 在该 PR 的 Codex Summary/交付 comment 核对，不借用前置 PR 的 review。PR #59 手动合并后应重新核对本包 diff 并调整 base，不自动执行合并。Actions 持续关闭，不自动合并，不发布。

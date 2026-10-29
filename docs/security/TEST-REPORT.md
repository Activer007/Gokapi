# M0 本地测试报告

日期：2026-10-27（Asia/Shanghai）。初始源码基线 a9c25d453aab0c25f2d6e6926ec7312628638844。目标是建立可复现证据；这里的 PASS 不代表上线许可。初始 M0 仅本地交付，后续按用户授权在 Activer007/Gokapi 创建、审查及合并 PR；无部署。最新审查结果见末尾增补，早期记录保留原始结果。

## 当前后续目标（2026-10-29）

替换完成后按用户指令收敛至[M3-GOAL](M3-GOAL.md)。PR31仅范围文档，无运行时测试重跑；
本轮在c3058ed实现最小恢复，结果单列如下，不借用历史PASS。

## SEC-030 最小启动恢复（2026-10-29）

最终收口：[PR35](https://github.com/Activer007/Gokapi/pull/35)，代码db4dc8a7e0ede74390cd422728fccd9ebf3287b3，
[CI40833366371](https://github.com/Activer007/Gokapi/actions/runs/40833366371) SUCCESS，job123312410322。
实际checkout a78f7be52633fef9897a0c32e6f9ab491ef5fda2，父32fe5c1+db4dc8a，tree89276c432411389c7e685223fece6ce74eb19496，
Go1.26.10 linux/amd64、Ubuntu24.04；包含UI PR32–34及仓库身份迁移PR36。收口提交仅安全Markdown/文本证据，代码/运行器不变。

| 最终CI真实命令 | 结果 |
| --- | --- |
| `go generate ./...` | PASS，含后续scan中的生成核对 |
| `go test ./... -parallel 8 -count=1 --tags=test,awsmock` | PASS |
| `go test ./... -parallel 8 -count=1 --tags=test,noaws` | PASS |
| `go test ./... --tags=test,noaws,integration -count=1` | PASS |
| `GOKAPI_AWS_BUCKET=gokapi GOKAPI_AWS_REGION=eu-central-1 GOKAPI_AWS_KEY=keyid GOKAPI_AWS_KEY_SECRET=secret go test ./... --tags=test,awstest -count=1` | PASS，仅合成凭据/已有模拟环境，不是真实S3 |
| `go test -race ./internal/environment ./internal/storage ./internal/storage/uploadreceive ./internal/webserver/fileupload ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC030Receive\|TestSEC031UploadSizeSemantics\|TestReceive\|TestSpool\|TestSEC062Range'` | PASS，既有receiver共享路径race |
| `bash docs/security/tools/run-sec030-whole.sh target` / `bash docs/security/tools/run-sec030-whole.sh race` | PASS，两次source-unchanged为0；三窗口新矩阵只在race执行（6.29s，A/B1.33s、C3.63s），negative2.78s；小chunk helpers随target/race执行，未声称所有helper只跑一次 |
| `bash docs/security/tools/run-sec062-toolchain.sh scan` | PASS，格式/生成、server/CLI/generator构建与实际BuildInfo、扫描返回码核实；模块提示仍保留 |

公开[CI摘要](evidence/sec030-recovery-c3058ed/ci-summary.txt)和PR审查记录保留实际输入/结果；没有为文档改动重跑全套CI。
文档白名单检查最初误将新增summary算作tracked而拒绝；修正为6个tracked Markdown+1个新增文本后PASS，源码/运行器无变化。
下面为开发期记录；旧阶段文字不能覆盖本最终结果。冻结范围Gate R1/R2/R3完成，广义M3延期不改标已修复。

环境：Windows/amd64，portable Go1.26.10，CGO=0、无gcc；测试仅临时SQLite/local副本。
ignored本地证据：`evidence/sec030-recovery-local.secret/`。两个用户目录未改，无真实部署数据操作。
先冻结ADR-021不变量及三窗口合同，再用新入口空实现取得NEW_CONTRACT/STUB_RED；不是基线漏洞定性。

| 实际命令 | 结果 |
| --- | --- |
| `go test ./internal/storage -count=1 -tags=test,noaws,security_recovery -run '^TestSEC030RecoveryCrash$' -timeout 3m -v`（空入口） | FAIL，退出1/6.493s；A/B/C均真实到达exit97后fresh进程业务断言失败，无编译/环境失败冒充RED |
| `go test ./internal/storage ./internal/storage/chunking/chunksession ./internal/storage/uploadreceive ./internal/webserver/fileupload ./cmd/gokapi -count=1 -tags=test,noaws,security_chunk,security_recovery -run '^TestSEC030Recovery' -timeout 3m -v` | PASS，退出0；storage7.627s（3窗口+2negative）、chunksession4.894s、receiver1.432s；fileupload5.263s/main7.592s为编译及TestMain，无匹配测试 |
| `go test ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws,security_recovery -run '^TestSEC030Recovery.*ReadMetadata$' -timeout 90s -v` | PASS，退出0；SQLite4.355s、miniredis4.405s，正常完整模型/missing/cancel/nil/坏gob；miniredis无法检查真实持久化是预期限制 |

独立审查后收紧canonical同长损坏、whole不可能状态；两个negative均PASS，不扩大provider矩阵。
后续审查补充checked/context metadata读取、删除前取消检查、whole FR-scope拒绝及ScanComplete时机；
补充项单列验证，最终完整版本交CI，不重复本地大矩阵。新读取接口仅恢复使用，原legacy getter未重构。
原生-race因无CGO/gcc为BLOCKED；最终Linux相关race结果见本节收尾。
首个冻结head ac2b6ec 的CI40831437902已SUCCESS，候选22e11b2/treeb06a48e8含UI PR32–34；
生成、四组全仓、receiver/durable race及scan均通过，三窗口race6.26s、negative2.71s。
合并时GitHub返回405冲突：同期PR36已将module/import迁移到Activer007/Gokapi（master32fe5c1）。
保留迁移，仅同步本轮新增Go imports并合并Main启动门禁；旧CI不代表这个新组合，另行验证并记录结果。
迁移后的实际命令：`go test ./internal/storage ./internal/storage/chunking/chunksession ./internal/storage/uploadreceive ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis ./cmd/gokapi -count=1 -tags=test,noaws,security_chunk,security_recovery -run '^TestSEC030Recovery' -timeout 3m -v`
PASS，退出0；新module包storage7.326s/chunksession4.697s/receiver1.282s/SQLite1.589s/Redis1.646s，main9.002s编译+TestMain无匹配case。
这次重复由真实基线/import变化触发，保留前次PASS与Git合并冲突记录；迁移后的最终CI见本节收尾。
scan保留模块级GO-2026-5932及GO-2022-0646/0635提示，受测配置未报affected调用，不声称依赖已修复。
大文件/浏览器/真实Redis持久化/S3/断电/生产演练NOT_RUN。三窗口只证明SQLite/local进程边界，
C先保留未知whole硬链接，再在隔离副本备份、same-inode+SHA256确认仅unlink该私有链接，才恢复已知chunk输入。
不会把Committed、相同文件名或仅mtime当作任意删除/退款证据；所有旧失败与延期记录继续保留。

## SEC-030 文件替换增量（2026-10-29）

最终：[PR30](https://github.com/Activer007/Gokapi/pull/30)合并60b2fb0；[CI40823139886](https://github.com/Activer007/Gokapi/actions/runs/40823139886)
实际SUCCESS，checkoutdda1f255bf72a531db5bda9a00dfbb1cce6e8875，tree1325ff654d27942ac78f7a086c6e70400e00a072与合并一致。
环境Go1.26.10 linux/amd64、Ubuntu24.04；生成、awsmock/noaws/integration/awstest四组全仓、receiver及
durable admission相关race、当前build/scan、两次source-unchanged均通过。替换4容量例6.37s+self1.11s、
核心、双provider和锁合同在race日志明确PASS。最终命令详见PR30审查；仅取消重复push运行，未重跑合并CI。
大文件/浏览器/真实Redis/S3/手动镜像发布NOT_RUN。下文CI_PENDING描述保留当时阶段状态，以本收尾为最终结果。

基线79f2577b3827140db8de65e90bfc41e8a17685ff，Go1.26.10 windows/amd64；未部署。
ignored证据目录 `evidence/sec030-replace-local.secret/`。生产开始前tracked internal与79f无diff，
仅新增HTTP合同；临时SQLite/loopback miniredis/隔离canonical副本，含GC的self edge也仅在临时子进程。

| 真实命令 / 输入 | 结果 |
| --- | --- |
| 基线：`go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole,security_replace -run '^TestSEC030ReplaceQuota$\|^TestSEC030ReplaceSelfDeleteConflicts$' -timeout 2m -v` | ASSERTION_RED，退出1，7.812s；4容量例分别接受9/49次增长，self edge旧200；均到达业务断言 |
| `go test ./internal/uploadquota ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis ./internal/webserver/api/mutex/apimutex -count=1 -tags=test,noaws,security_replace -timeout 2m` | PASS，2.917s/6.283s/9.040s/2.339s；完整相关包回归，含新delta、删除、WATCH及锁碰撞/反序合同 |
| 接线后同上HTTP命令 | PASS，15.106s，5组全部通过；随后审查将Now刷新到blob探测后，最终该版本由CI验证 |
| `go test ./internal/storage -count=1 -tags=test,awsmock -timeout 2m` 首次 | FAIL，14.172s，仅TestReplaceFile因前序owner0合成metadata无法计量；补正旧删除fixture owner，不放宽校验/改断言 |
| 同命令第二次 | FAIL，17.519s，TestReplaceFile通过，但TestDuplicateFile遇到前次Windows未删净的旧owner0数据库；需清理已确认的测试副本后重新验证，不能记PASS |
| 清理已确认的 `internal/storage/test` 合成副本后，同storage命令 | PASS，13.636s；全部原storage合同通过，删除夹具仅补正UserId，原断言/生产校验保留 |
| 路由：build/go-generate下 `go run updateApiRouting.go` | PASS，无routingParsing内容diff |

最终storage干净副本PASS；原失败日志保留，不把前轮结果扩大为本轮PASS。最终Linux CI待运行一次，
结果记关联PR审查，不为补文档重复触发CI。
本机race因既有CGO0/无gcc为BLOCKED且不重复尝试；最终CI包含新锁及替换合同的race。
大文件/慢速/浏览器/真实Redis/S3/手动发布：NOT_RUN，未修改流式或crypto代码；无依赖变更。

## SEC-030 单文件owner准入增量（2026-10-29）

收尾：PR27合并79f2577；[CI40816967130](https://github.com/Activer007/Gokapi/actions/runs/40816967130)
实际SUCCESS，checkoutd129cfb含UI PR26，tree4de09843与实际合并一致。生成、四组全仓、相关race及
扫描通过；新owner合同仅在race运行一次，完整命令与审查记录在PR27，未补文档重复CI。

基线c3244f008214a851404ecfcf5367e3c1878b58fa，Go1.26.10 windows/amd64，未部署。
防御性合同复用临时SQLite及loopback miniredis子进程；本轮原始证据在ignored
`evidence/sec030-owner-local.secret/`。生产实现开始前确认tracked internal源码与基线无diff，仅增加HTTP合同；
其子进程的数据/文件均隔离，因此不再复制整仓库后重复编译。不复用前轮绿色。

| 真实命令 / 输入 | 结果 |
| --- | --- |
| c324运行时：`go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole,security_owner -run '^TestSEC030OwnerQuota$' -timeout 2m -v` | ASSERTION_RED，退出1，7.289s；双provider bytes9次转入、files50次转入均成功，4组到达CAPACITY业务断言 |
| 核心：`go test ./internal/uploadquota -count=1 -tags=test,noaws,security_owner -timeout 60s` | PASS，2.090s；含既有整包合同及目标delta/同owner/不重复计instance与request/全部hold计费 |
| Provider：`go test ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws,security_owner -timeout 2m` | PASS，SQLite6.085s / Redis8.800s；含既有整包回归、当前字段保留、noop、pending及source/target WATCH竞争 |
| 最终HTTP：`go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole,security_owner -run '^TestSEC030OwnerQuota$' -timeout 2m -v` | PASS，7.215s；双provider各bytes10/files50（含已有hold与真实chunk竞争），4组全通过 |
| 路由：在build/go-generate下 `go run updateApiRouting.go` | PASS，无routingParsing内容diff |
| `openapi.json` 使用ConvertFrom-Json解析并检查changeOwner响应键 | PASS，200/400/401/404/409/413；未改变成功响应schema |

核心、provider与HTTP已冻结；Linux最终CI/race待运行一次，结果记入关联PR审查，不再为补文档触发重复CI。
本机native race仍受既有CGO0/无gcc限制，BLOCKED且未重复尝试；Linux相关race为本轮最终Gate。
本轮大文件/慢速/浏览器/真实后端及本地完整生成、全仓：NOT_RUN；最终CI按变更范围执行一次。

## SEC-030 复制准入增量（2026-10-29）

收尾：PR24合并为c3244f0；[最终CI40812061382](https://github.com/Activer007/Gokapi/actions/runs/40812061382)
实际SUCCESS，checkout5d37f97含UI PR23/25，tree2982bfdf与实际合并一致。生成、四组全仓、
相关race及扫描通过；复制12组/core/双provider仅在race步骤跑一次。最终命令及审查记录在PR24，
没有为补文档重跑CI；下表及CI_PENDING描述保留各阶段当时状态，不能替代此最终结果。

起点c657d547322c8ef0339287dde6dddd5688384763，便携Go1.26.10 windows/amd64；SQLite临时目录及loopback miniredis，未部署。
原始日志位于ignored `evidence/sec030-duplicate-local.secret/`；不包含真实数据。基线归档仅叠加新HTTP合同后执行RED。

| 真实命令 / 输入 | 结果 |
| --- | --- |
| c657归档：`go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole,security_duplicate -run '^TestSEC030DuplicateQuota$' -timeout 2m -v` | ASSERTION_RED，退出1，9.738s；12组均到达业务竞争断言，非编译/auth/setup失败 |
| 首次实现：`go test ./internal/webserver/api ./internal/storage ./internal/uploadquota -count=1 -tags=test,noaws,security_whole,security_duplicate -run '^TestSEC030DuplicateQuota$\|^TestDuplicateFile$' -timeout 2m -v` | PASS；API8.863s、storage5.982s；uploadquota无匹配测试，不计核心合同已执行 |
| 审查兼容用例：`go test ./internal/uploadquota -count=1 -tags=test,noaws,security_duplicate -run '^TestSEC030DuplicateManagementRequestOwnership$' -timeout 30s -v` | ASSERTION_RED，退出1，2.304s；FR跨owner副本误拒绝，随后修正 |
| 最终核心/双provider：`go test ./internal/uploadquota ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws,security_duplicate -timeout 2m` | PASS，2.818s / 7.098s / 6.684s；含原有整包回归及新源变化、损坏投影、WATCH合同 |
| 最终HTTP/兼容：`go test ./internal/webserver/api ./internal/storage -count=1 -tags=test,noaws,security_whole,security_duplicate -run '^TestSEC030DuplicateQuota$\|^TestDuplicateFile$' -timeout 2m -v` | PASS，API9.211s、storage5.697s；含跨owner副本再次复制及12组10/50竞争 |
| 路由生成：在build/go-generate下 `go run updateApiRouting.go` | PASS，无内容diff；第一次从仓库根运行因相对路径错误FAIL，已纠正，不作为产品RED |
| PR24首轮CI40811235737，合并候选1cd3fb9（含已合并UI PR23） | FAIL：生成/编译器检查通过，awsmock整包仅TestDuplicateFile因前序testget/testhash夹具UserId为0失败；后续步骤SKIPPED，不能记PASS |
| 夹具修正后的顺序回归：`go test ./internal/storage -count=1 -tags=test,awsmock -run '^TestGetFile$\|^TestGetEncInfoFromExistingFile$\|^TestDuplicateFile$' -timeout 90s -v` | PASS，5.930s；三项按原顺序运行，全部旧断言保留 |

最小修复为这两个既有合成fixture补正UserId，不改旧断言或生产校验；不引入数据库切换/清库框架。

Linux完整CI/race在最终head运行一次，结果记录于关联PR审查，避免补文档重复触发CI。
本地完整go generate、Windows全仓、native race、大文件/慢速/浏览器/真实Redis/S3：NOT_RUN，本增量不触及流式I/O、依赖或生成页面。
Native race仍受既有CGO0/无gcc限制，本轮不重复尝试；Linux执行相关race。历史PR22结果不扩大为本轮PASS。
完整M3仍OPEN：replace/owner transfer/bulk metadata准入、跨写者物理协调及崩溃恢复继续单独验证。

## SEC-030 分片/请求接线：代码 Gate 通过（2026-10-29）

基线57e5116；Windows便携Go1.26.10，仍不启动/修复WSL。独立归档及当前临时SQLite/loopback Redis
证据位于ignored sec030-chunks-local.secret和request-policy-*，不是生产数据。
首个代码head be5f385已创建[PR22](https://github.com/Activer007/Gokapi/pull/22)。用户随后明确默认
owner5GiB/instance20GiB，其他上限不变；旧head的PR40797560851/手动40797586038已主动取消，
不记完整PASS；更新默认值后的143f041已实际通过完整Gate（见下节）。以下本地结果保留各自实际输入及范围。
新默认值最小合同在隔离be5实现叠加当前test后真实RED：`go test ./internal/environment -count=1
-tags=test,noaws -run '^TestSEC030WholePolicyDefaults$' -timeout 30s -v`，退出1，3.074s，实际报告
`UploadUserTotalMB default=20480; want 5120`。当前代码的environment/fileupload两包
`-run 'TestSEC030Whole|TestSEC030Request' -timeout 90s -v`实际PASS（fileupload8.168s），
并再次`go generate ./...` PASS；default tag、严格预解析及文档一致，显式非默认容量仍受原测试覆盖。
大文件夹具改用用户选择的5120/20480 MiB字节上限，仍待实际运行，不沿用旧head结果。

- API干净57归档加新HTTP合同：`go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole -run '^TestSEC030Chunk' -timeout 7m -v`，实际ASSERTION_RED退出1，29.099s。
- chunksession新桩合同编译并执行后退出1，6.074s；新接口开发RED，不称旧代码存在同接口漏洞。
- request env/core/双provider合同：真实stub RED退出1→目标及四包原foundation完整回归退出0；源码hash一致，未执行本轮全仓/race。
- 会话target-1变量遮蔽失败、target-3 Windows句柄断言失败保留；target-4真实PASS，13.133s，固定清理ID/FD及范围/并行/space/过期合同通过。
- stdlib uploadservice8项合同编译实际RED→目标PASS；本地race尝试因cgo关闭/无gcc为BLOCKED。
- 接线首次编译FAIL（遗漏内部chunkParams接口），恢复后声明总量10个双provider/scoped子例PASS，7.686s。此编译失败不作为产品RED。

本轮后续实际执行（原始日志仍在上述ignored目录，不把以前PR的绿色扩大到新代码）：

| 命令/合同 | 实际结果 |
| --- | --- |
| 57归档 `go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole -run '^TestSEC030ChunkCompleteCoverage$' -timeout 45s -v` | ASSERTION_RED，退出1，7.134s，四种provider/入口均到达缺失覆盖拒绝断言 |
| 完成全矩阵历史尝试 `-run '^TestSEC030ChunkComplete' -timeout 3m` | FAIL/timeout，186.201s；旧版本没有待测账本而等待settled，不能称全矩阵已获完整RED；随后收敛到上行最小合同 |
| 会话最终 `go test ./internal/storage/chunking/chunksession -count=1 -tags=test,noaws,security_chunk -run '^TestSEC030ChunkSession' -timeout 90s -v` | PASS，16.500s，包括unknown/foreign/committed取消no-op且不清理/不退款 |
| 新API `go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole -run '^TestSEC030Chunk' -timeout 7m -v` | PASS，45.622s；10/50 bytes/files/active、声明总量、FR累计、覆盖、完成竞争/重放及worker尾部 |
| `go test ./internal/storage/processingstatus/pstatusdb ./internal/webserver/fileupload/uploadservice -count=1 -tags=test,noaws -timeout 45s -v` | PASS；首次遗漏test标签的命令为setup FAIL，未冒充产品RED |
| `go test ./internal/storage ./internal/webserver -count=1 -tags=test,noaws,security_whole -run 'TestSEC030Whole\|TestSEC030UploadShutdown' -timeout 3m -v` | PASS，7.583s/11.376s；包含真实HTTP/worker尾部排空与低空间拒绝。空间fixture首次超出旧int parser的32位范围导致FAIL，改为合法2147483647 MiB，不分配大文件 |
| `go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole -run '^TestSEC030ChunkE2E' -timeout 90s -v` | PASS，6.955s；真实连续writer/输出buffer reset/最终Close尾段，密文计量、假plaintext display不减quota、解密精确读回；不是浏览器/WASM运行 |
| `go generate ./...` | PASS，原生Windows生成路由、env表、WASM及minified产物；仅保留本轮env文档变化，既有手工SEC013代理行恢复，无UI重写 |
| 6包及status子包，`-tags=test,noaws,security_whole,security_chunk -run 'TestSEC030Chunk\|TestSEC030RequestReservationDisplay\|TestSEC030UploadShutdown\|TestSEC032BrowserChunkScope\|TestSEC031UploadSizeSemantics\|TestChunkComplete\|TestChunkUpload\|TestProcess' -timeout 5m -v` | PASS；API51.057s、fileupload9.647s、webserver13.700s、session15.683s，display/status通过；processingstatus父包无匹配，不算该包合同已执行 |
| 新建57归档叠加当前源码及已生成embed，`go test ./... -count=1 -tags=test,noaws -timeout 4m` | FAIL。五个既知Windows断言仍失败（打开文件删除、localstorage路径、断线spool观察、SSL）；另发现两份SEC032旧fixture缺完整header/未存当前owner，已补有效参数及当前主体、保留精确400/12并加强不变断言。282份Go输入测试前后不变，不能声称全仓通过 |
| 全新归档，`go test ./internal/webserver/api -count=1 -tags=test,noaws,security_whole,security_chunk -timeout 5m -v` | PASS，48.144s；包含全部API旧测试、新chunk/whole合同及两份修正guard，282份Go输入前后不变。之前在已跑过的Windows副本重跑5.749s FAIL/Users.Name冲突，属未清理的旧fixture残留，未隐去失败，改为全新副本才得此PASS |
| 隔离副本 `GOOS=linux GOARCH=amd64 go test -c ./internal/webserver/api -o ../linux-chunk-large-api.test -tags=test,noaws,security_whole,security_chunk,security_large` | 首次FAIL（Linux专属large test一个未使用import），移除后实际编译PASS；只交叉编译，Linux大文件运行仍NOT_RUN |

首次完成RED调用因归档工作目录下使用相对本地env路径未启动Go，标记NOT_RUN；绝对路径重试才是上表真实结果。
首次组合regression保留FAIL：新私有payload布局使旧flat-path测试失效，Windows断线观察仍失败；
旧fixture已改为通过冻结Lease精确读回和确认自身未发布资源清理，不删除安全断言或取消校验。
低空间检查现覆盖N+真实密文长度+余量+MinFreeSpace，整数边界单测通过。
上述本地阶段race因cgo/GCC限制BLOCKED，Linux四组全仓/race/scan/1–2GiB当时尚未运行，
后续实际结果见下节；真实Redis/S3仍NOT_RUN。20ms资源采样只代表采样峰值，不证明全部瞬时峰值。
完整M3仍缺其他metadata正向准入、跨所有写者的物理容量协调、Claim/发布后崩溃对账及全部路径慢请求矩阵。
有限模式扫描本轮59个待审文件未匹配私钥、GitHub Token、AWS访问Key或带敏感参数的URL；
这不是全仓秘密审计。用户两个未跟踪目录未复制、未修改、未提交。

### 最终 Linux 代码 Gate

已验证代码 `143f041162605a160c98f9cf0420033260d8bded`，用户选择owner5GiB/instance20GiB；
[PR CI40798733852](https://github.com/Activer007/Gokapi/actions/runs/40798733852)及
[完整手动40798813978](https://github.com/Activer007/Gokapi/actions/runs/40798813978)均实际SUCCESS。
Ubuntu24.04 runner、Go1.26.10 linux/amd64；原始decoded日志在ignored目录，
[公开数值与复现说明](evidence/sec030-chunks-143f041/README.md)不包含敏感请求正文。

| 实际命令/Gate | 结果 |
| --- | --- |
| `go generate ./...`、占端口setup合同 | PASS |
| `go test ./... -parallel 8 -count=1 --tags=test,awsmock` | PASS |
| `go test ./... -parallel 8 -count=1 --tags=test,noaws` | PASS |
| `go test ./... --tags=test,noaws,integration -count=1` | PASS |
| `go test ./... --tags=test,awstest -count=1`，workflow提供合成AWS mock设置 | PASS，不称真实S3服务 |
| workflow的receiver相关`go test -race` | PASS |
| `bash docs/security/tools/run-sec030-whole.sh target`及`race` | PASS；whole/mock/sdk/source-unchanged结果均0；包含新Chunk/Request/UploadService/UploadShutdown合同 |
| `bash docs/security/tools/run-sec062-toolchain.sh scan` | PASS，当前源码/构建与扫描器Gate |
| 同runner `historical` | 合同PASS，旧编译器扫描实际退出3并命中GO-2026-6609；不误写扫描命令退出0 |
| 同runner `containers` | PASS，server/CLI/builder三镜像隔离构建，不推送、不部署 |
| `bash docs/security/tools/run-sec030-receiver.sh large` | PASS，原whole1GiB明文/2GiB服务器加密实传及SHA256读回 |
| `bash docs/security/tools/run-sec030-whole.sh large` | PASS，新增ordinary1GiB明文/FR2GiB服务器加密的真实未知长度8MiB分块HTTP上传、完成和流式SHA256读回 |

新增chunk ordinary：15.780s、64.89MiB/s，heap/RSS采样峰值3512472/27406336 bytes；FR服务器加密：
35.248s、58.10MiB/s，3594128/28213248 bytes。磁盘表观峰值分别2147498459/6423946408 bytes，
分配峰值2147516416/6423973888 bytes。读回直接打开隔离canonical blob，FR先解密；
不称HTTP下载或真实浏览器/WASM运行。20ms采样不证明全部瞬时峰值，不是生产吞吐承诺。
Native失败及已知限制不因Linux绿色撤销；完整M3保持OPEN。

### 输入与审查核验

两份成功job decoded日志分别对照143f041的Git blob原始字节（不经PowerShell文本管线转换），
范围为Main、database/environment/storage/uploadquota及完整webserver。202份Go与2份harness均匹配，
base57→143的50份非删除Go变更50/50覆盖，0 mismatch；target/race/large的source-unchanged均退出0。
[204项清单](evidence/sec030-chunks-143f041/source.sha256)和[核验数据](evidence/sec030-chunks-143f041/verification.json)
明确不是全仓所有文件或语句/分支覆盖率。PR10条/手动13条SEC030结果全0，手动恰4条真实whole/chunk metric。

首次简单合并全日志同路径hash得到manual MISMATCH：historical工具链步骤使用cb5868c6旧workflow，
当前harness仍为143版本。经分开归属，两种workflow的Git blob SHA各自真实匹配；旧扫描实际退出3，
并有published GO-2026-6609 RED marker。原误归类JSON仍保留，不通过忽略任意冲突伪造VERIFIED。
独立只读代码/远端patch与关键blob核对无本增量剩余阻断；[GitHub COMMENT审查](https://github.com/Activer007/Gokapi/pull/22#pullrequestreview-5691708097)
明确为工具辅助self-review，不声称独立人类审批。最终文档提交将保持全部Go/harness输入与143一致。

## SEC-030 整文件准入：代码 Gate 通过（2026-10-29）

下列初期阶段片段保留当时的IN_PROGRESS/失败状态，最终结果以本节“最终Linux代码Gate”增补为准。

工作基线 `710f4bf32ae80ac110bb5cd24e8d92848ac22bd8`，已验证代码9db2542，[PR #21](https://github.com/Activer007/Gokapi/pull/21)。本节先保留阶段记录，最终Linux结果见下方增补，最终文档head及合并结果另核实。范围为 `/api/files/add`，设计/默认值/回滚见[ADR-016](ADR-016-WHOLE-UPLOAD-ADMISSION.md)。此前PR的绿色不作为本轮接线结果。

执行环境新增Windows PowerShell / 便携Go1.26.10 windows-amd64，官方清单记录与实际zip SHA256均为 `7ecd2d7260e82c59bcbd7f1077a07cee2dea607295b858df67195a1e4e09629b`。下载及隔离cache/module/TMP保留于ignored的 `docs/security/evidence/sec030-whole-local.secret/`，没有调用生产数据或真实AWS。WSL仍BLOCKED，未启动/修复宿主；不能将早期WSL或依赖下载失败记为本轮合同通过。

| 已核对证据 | 实际结果及边界 |
| --- | --- |
| `api-red.log` | FAIL：包setup失败；没有运行到API安全断言，不能作为RED依据 |
| `red-archive-verified.log`、`red-inputs.sha256` | FAIL / ASSERTION_RED：710f4bf归档叠加记录过hash的防御合同；配置字段/非法值未满足合同，存储开发桩及API断言实际执行 |
| API并发Redis原基线 | 按父subtest核对：bytes/files两维、10/50均为successes10/50、quota_denied0、receiver_busy0，且完成回执0；是有效的独立合同RED |
| API并发SQLite旧夹具 | bytes/files两维、10/50均为successes1、quota_denied0；只有1个Key进入正文，其余401。空PublicId触发唯一列INSERT OR REPLACE覆盖早先Key，不能作为quota争抢RED或GREEN；原日志保留 |
| API共享blob | 第三份768KiB实际200/0，重连后仍200/0，目标为413/21；与MaxSize单文件历史公告分开，不认定新未知漏洞 |
| `storage-stub-red-verified.log` | FAIL / ASSERTION_RED：新增接口桩可编译并返回ErrInvalid，happy/fault合同失败；属于新实现开发RED，不称原源码存在同一接口漏洞 |
| `policy-red-verified.log` | FAIL / ASSERTION_RED：配置合同实际失败；旧guest零语义正向对照PASS，不用取消旧防线获得RED |
| `hotlink-red-verified-20261029-054910.log`及metadata文件 | 实际退出1，RESULT=ASSERTION_RED；SQLite/Redis hotlink原子性、冲突/类型及WATCH竞态合同失败，输入hash和完整命令已保存 |

上述权威RED中已核实完整命令的一项为：

```sh
go test ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws,security_whole -run '^TestSEC030WholeHotlink' -v
```

其余归档日志已核对包、测试名和实际断言；完整shell调用细节待执行者按本地记录补录，不在此推测。原始日志/源码hash保留ignored目录，不把其中私密诊断直接加入公开提交。当前API合同以启动前子进程环境配置、真实HTTP、同owner多Key、首Read barrier排除receiver假证明；共享blob按metadata分别计量并验证内容及关闭重连。

### 本轮首个实现target与夹具更正

`root-target.command.txt`记录实际执行命令，`root-target-environment.txt`确认Go1.26.10 windows/amd64：

```sh
go test ./internal/webserver/fileupload ./internal/webserver/api ./internal/storage/filesystem/s3filesystem/aws -count=1 -tags=test,noaws,security_whole -run 'TestSEC030Whole|TestSEC030AWS' -timeout 8m -v
```

`root-target.log`实际整体FAIL：fileupload和noaws AWS目标PASS，Redis字节/文件竞争PASS，sharedblob双providerPASS；SQLite10/50仅1个请求达到body barrier，其余401，不能把该失败解释为持久额度竞态。修订仅在合成Key加入随机PublicId，并在并发前回读确认全部Key存在，没有改生产认证。修订测试的冻结基线RED正在重跑，结果和新输入hash尚待执行者补录；上表旧SQLite结果已降为夹具失败，不删除原日志。

`blob-first.command.txt`另记录实际执行：

```sh
go test ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws,security_whole -run '^TestSEC030WholeBlob' -v
```

`blob-first.log`整体FAIL：两provider的损坏加密编码合同失败，Redis不可用provider合同出现nil pool panic。审查另确认投影强制转换可能掩盖非法类型，已作修订；本节未核实修订后的最终GREEN，保留为IN_PROGRESS。它们是本轮新增getter的防御合同，不从孤立损坏数据测试推定外部可利用的未知漏洞。

当前源码已经接上整文件准入、严格候选策略及hotlink同Commit；最终目标/故障/全仓/race结果仍IN_PROGRESS，完成后按具体源码hash补录。未记录的完整 `go generate ./...`、三组/四组全仓、相关race、当前变更govulncheck、本轮1–2GiB及真实Redis/S3/生产恢复均不记PASS。本次文档整理仅核对既有记录，未追加执行Go测试；已存在的局部日志不能被扩大为上述Gate已通过。

本增量选择可配置有限默认额度，不记为用户逐项批准；SQLite16/Redis9无新增schema，内部稳定ID不提供跨HTTP幂等。分片/File Request累计、正向metadata准入、异步完成及启动目标对账保持OPEN；发布后或Commit不确定时保留charge，不自动退款、不删canonical。回滚必须停写并核对配套数据库/DataDir/配置密钥备份，不能因schema未变就直接换回忽略活动charge的旧代码。

### 本地已核实增量结果

便携Go1.26.10 windows/amd64；使用既有系统代理但不记录其值，缓存/临时目录位于ignored隔离目录。loopback和NTFS hardlink测试采用提升执行权限，不改宿主策略或WSL。

| 实际命令（均加 `-count=1`） | 结果 |
| --- | --- |
| 冻结710f4bf叠加修订API夹具，`go test ./internal/webserver/api -tags=test,noaws,security_whole -run '^TestSEC030Whole' -timeout 8m -v` | ASSERTION_RED，退出1，9.168s；双provider bytes/files×10/50均成功10/50、quota拒绝0，无401夹具问题；共享blob合同亦失败。`red-api-fixed.log`及修订输入SHA保存 |
| 当前相同API目标及 `./internal/webserver/fileupload` 的Whole响应/limits合同 | PASS，退出0；API 8.330s；新413/21、active/staging及固定错误文案合同通过 |
| `go test ./internal/storage -tags=test,noaws,security_whole -run '^TestSEC030WholeReserved\|^Test(NewFile\|NewFileFromChunk\|DuplicateFile\|ReplaceFile)$' -timeout 5m -v` | PASS，退出0，7.831s；包含最终flags guard、锁取消、GC交错、active/staging零input读取以及旧路径回归 |
| 两provider `-tags=test,noaws,security_whole -run '^TestSEC030WholeBlob' -v` | 新getter首轮实际退出1（两边接受尾零、Redis nil pool panic）；reader剩余长度/字节CAST/nil guard后退出0。TEXT+NUL投影上界另经源码核对 |
| 两provider `-run '^TestSEC030WholeHotlink'` 及原foundation `-run '^TestSEC030'` | PASS；前置真实RED与首个竞态测试夹具错误均保留，不将Redis EXEC运行期错误称回滚 |
| AWS包 `-tags=test,awsmock -run '^TestSEC030AWS' -v`、`-tags=test -run '^TestSEC030AWS' -v` | 均PASS，含真实SDK合成loopback上传/HEAD/取消；跨桶删除mock合同另RED退出1→一行locator修复→GREEN退出0（7.276s），不是产品S3漏洞认定 |
| `go generate ./...` | PASS，退出0；保留原有代理策略文案，新增环境变量参考及整文件额度说明；未修改UI实现 |
| `bash -n docs/security/tools/run-sec030-whole.sh` | PASS，退出0，Git Bash；未启动WSL |

首次本地全仓noaws实际FAIL：Go枚举到ignored便携SDK内的Go测试源码，同时原证书日期断言失败（365/-1）。为执行器隔离目录添加独立go.mod边界后，`go list ./...`退出0且不再包含SDK；已开始后续命令不追改原结果。当前三组全仓及native race执行完成后另补结果。Linux全仓/race、当前扫描及1–2GiB尚NOT_RUN，不能沿用前PR结果。

### 本轮最终Linux代码 Gate：9db2542

[PR CI40778813175](https://github.com/Activer007/Gokapi/actions/runs/40778813175)与[完整手动40778841823](https://github.com/Activer007/Gokapi/actions/runs/40778841823)全部SUCCESS；手动job123135051507，ubuntu-latest/Linux amd64，Go1.26.10，GOTOOLCHAIN=local。

真实执行并PASS：

```sh
go generate ./...
bash docs/security/tools/run-sec062-setup-listener.sh --occupied
go test ./... -parallel 8 -count=1 --tags=test,awsmock
go test ./... -parallel 8 -count=1 --tags=test,noaws
go test ./... --tags=test,noaws,integration -count=1
GOKAPI_AWS_BUCKET=gokapi GOKAPI_AWS_REGION=eu-central-1 GOKAPI_AWS_KEY=keyid GOKAPI_AWS_KEY_SECRET=secret go test ./... --tags=test,awstest -count=1
go test -race ./internal/environment ./internal/storage ./internal/storage/uploadreceive ./internal/webserver/fileupload ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC030Receive|TestSEC031UploadSizeSemantics|TestReceive|TestSpool|TestSEC062Range'
bash docs/security/tools/run-sec030-whole.sh target
bash docs/security/tools/run-sec030-whole.sh race
bash docs/security/tools/run-sec062-toolchain.sh scan
bash docs/security/tools/run-sec062-toolchain.sh historical
bash docs/security/tools/run-sec062-toolchain.sh containers
bash docs/security/tools/run-sec030-receiver.sh large
```

awstest的值是workflow原有合成loopback配置，不是真实AWS密钥。whole target/race各自的whole/mock/sdk返回0；含双provider10/50争1份bytes/files余量、共享blob、Commit不确定保额、关闭失败、hotlink原子性、bounded getter、GC交错及真实SDK取消。历史Go1.26.0扫描实际退出3且含已公开GO-2026-6609，按预期RED通过；当前Go1.26.10的default/noaws/WASM源码及minifier/server/CLI二进制共6项扫描均退出0。模块级OpenPGP GO-2026-5932、AWS S3 Crypto GO-2022-0646/0635仍报告，受测配置未报告affected可达调用，不称依赖无公告。三个Docker构建和版本核对通过，未运行真实应用容器或发布镜像。

| 当前代码实际HTTP+SHA256读回 | 时间 / 吞吐 | heap / RSS峰值 | 暂存表观 / allocated峰值 |
| --- | --- | --- | --- |
| 1GiB明文 | 9.219s / 111.07MiB/s | 3053736B / 24420352B | 2147483683B / 2147495936B |
| 2GiB服务器加密 | 27.981s / 73.19MiB/s | 3079136B / 25628672B | 6427254819B / 6427271168B |

[公开证据](evidence/sec030-whole-9db2542/README.md)包含真实步骤/返回码、48份旧scope源码hash和6份生成产物hash。48项逐一匹配9db2542 Git blob；它们不是全部provider/whole输入。后续仅扩大完整源码哈希日志并补文档，运行时Go保持不变，最终PR CI会另核对扩展scope。20ms峰值采样不证明所有瞬时峰值，不用不同runner速度作性能优劣比较。真实Redis/S3、全部多路径/浏览器/完整崩溃恢复和M3其他入口仍NOT_RUN/OPEN。

### Windows全仓限制与基线对照

三条实际本地全仓命令均FAIL，含 `-count=1 -timeout 8m`：`go test ./... -parallel 8 -tags=test,noaws`、同awsmock、`go test ./... -tags=test,noaws,integration`。首两条还含SDK误枚举/运行期间建立模块边界的setup错误；awsmock编译时新跨桶mock负例尚未修，后续单包已GREEN。没有把这些非冻结工作树尝试作为最终head Gate。

Windows断言包括chunk打开文件删除、localstorage反斜杠、receiver断线前未观察到spool、SSL证书365/-1；后续suite还有UserExists/fixture残留，integration的TestParallelDownloads在8分钟超时。干净710f4bf归档的同一有限子集真实退出1：前述五个测试名称仍失败，ParallelDownloads单独PASS 0.04s；1285个原始输入hash前后无变化。不能从子集推定full挂起或跨suite残留已归因旧代码，两者保留VALIDATE。

进一步分别从710f4bf和9db2542建立全新归档，串行执行 `go test ./internal/storage -count=1 -tags=test,noaws,integration -timeout 45s -v`，两者单包全套均PASS（9.047s / 11.908s），ParallelDownloads各0.01s；两边原始输入hash无变化。该结果仅表明干净单包未复现挂起，不覆盖前述全仓失败或确证残留/调度的具体因果。

native `go test -race ./internal/storage -count=1 -tags=test,noaws,security_whole -run '^TestSEC030WholeReserved' -timeout 5m`先退出2（CGO未启用），显式 `CGO_ENABLED=1 go test -race ./internal/uploadquota -count=1 -tags=test,noaws`实际退出1，报C compiler gcc不存在；本地race为BLOCKED，不记PASS。相应Linux相关race已实际PASS。WSL仍BLOCKED，未启动或修复。测试生成残留按已核对路径移入ignored隔离证据，未操作用户原有目录或生产数据。

## 环境与隔离

- Windows 工作树 E:\LHA\Gokapi，PowerShell 中 go/docker 命令未找到；Go 测试在 Ubuntu-24.04 WSL 完成。
- go1.26.0 linux/amd64，Linux 6.18.54.1-microsoft-standard-WSL2 x86_64，gcc 可用，GOTOOLCHAIN=auto。最终 [platform.log](evidence/run-80e7sZ/platform.log)记录实际 Go 平台配置。
- [run-baseline.sh](tools/run-baseline.sh)使用 git archive HEAD 解包到新 /tmp/gokapi-m0-*；不会复制工作树部署配置、用户未跟踪计划或 .env。新夹具模式仅叠加列明的 Go 测试文件，保存 overlay.sha256。
- 使用 env -i 清除 GOKAPI/AWS 等部署变量，只保留 HOME/PATH/Go toolchain 和网络代理；NO_PROXY 固定 localhost/127.0.0.1/::1。代理值不打印。HOME 仅供 Go 缓存；测试 S3 显式回环端点和合成凭据，未走真实云账户。
- 官方三组顺序运行，避免上游 miniredis 固定 16379 端口冲突。新 fixture 使用 t.TempDir、随机回环端口；有全局配置/DB 的测试不并行。
- 每个命令保存 .command.txt、.log 和退出码 results.tsv；900 秒 timeout 是执行器上界。早期脚本仅收集证据、总是退出0；SEC-003 审查已修正为任一命令失败则最终退出1，并启用 pipefail。仍继续收集后续结果，必须逐项读取 results.tsv；--red/--fixtures 包含有意 RED，所以目前总体返回1。

## 运行历史与失败归因

| 运行 | 内容 | 原样结果与结论 |
| --- | --- | --- |
| 初始检查 | Windows go version | CommandNotFound；不是 Go 测试 FAIL |
| 首次脚本启动 | evidence 目录尚未创建 | mktemp 失败，退出1；未开始测试；已补 mkdir |
| [run-d5BXes](evidence/run-d5BXes/results.tsv) | Go 版本 + generate | Go 版本0；依赖下载在无代理环境等待。无代理 curl 实测8秒超时、保留代理可达；核实活进程后主动终止，generate 记录143；后续组 NOT_RUN |
| [run-tStSGu](evidence/run-tStSGu/results.tsv) | 原始 HEAD，无新增 fixture | generate PASS；三组 FAIL：唯一失败 package 为 internal/environment，TestTempDir 不接受执行器指定 TMPDIR=/tmp（期望空字符串或 data）；这是入口环境差异，不修改上游测试来掩盖 |
| [run-FFCfrI](evidence/run-FFCfrI/results.tsv) | 移除 TMPDIR，HEAD + 测试 overlay | generate、三组、fixture race、真实 S3 driver PASS；vet FAIL 为原有问题。RED 组中010/012失败；040因测试 SaveUser 缺 bool 参数编译失败，这一次不能作为040缺口证据 |
| [run-80e7sZ](evidence/run-80e7sZ/results.tsv) | 仅修正 opt-in RED 测试的 SaveUser 参数后，新副本重跑 | generate PASS；三个目标不变量均出现断言 FAIL，无编译失败；本地 build/version/build-info PASS；docker version 退出127，Docker 构建 BLOCKED |
| 追加 S3 race 的启动尝试 | 试图复用 run-FFCfrI 的 /tmp 副本 | cd 报 No such file or directory，实际测试 NOT_RUN；随后只读 ls 确认所有旧 /tmp/gokapi-m0-* 已不存在（原因未证实）。跨 PowerShell/WSL 的内联退出码采集也失效，results.tsv 末尾空白 s3-driver-race 项是无效记录，不是 PASS；保留原记录并改用脚本新副本重跑 |
| [run-r6ErJq](evidence/run-r6ErJq/results.tsv) | --s3-race 新副本 | generate PASS；真实 S3 driver 的故障/恢复 race 检查 PASS，使用文件脚本正常采集退出码 |

初始 M0 最后一次修改仅在 test && security_red 文件中补齐参数，默认三组编译不包含该文件。该阶段 overlay 清单见 [run-80e7sZ/overlay.sha256](evidence/run-80e7sZ/overlay.sha256)，普通测试结果对应 [run-FFCfrI/overlay.sha256](evidence/run-FFCfrI/overlay.sha256)；两者只在该 RED 文件不同。后续 PR 审查修改与重跑见末尾增补。

## 初始 M0 实际命令和状态

以下命令的 cwd 是对应 workspace.txt 中的隔离副本，环境如上。命令参数保持官方工作流分组，增加 count=1 防止缓存假通过。

| 命令 | 结果 | 原始日志 |
| --- | --- | --- |
| go generate ./... | PASS，0 | [generate](evidence/run-FFCfrI/generate.log) |
| go test ./... -parallel 8 -count=1 -tags=test,awsmock | PASS，0 | [awsmock](evidence/run-FFCfrI/awsmock.log) |
| go test ./... -parallel 8 -count=1 -tags=test,noaws | PASS，0 | [noaws](evidence/run-FFCfrI/noaws.log) |
| go test ./... -count=1 -tags=test,noaws,integration | PASS，0 | [integration](evidence/run-FFCfrI/integration.log) |
| go vet -tags=test,noaws ./... | FAIL，1，已有告警 | [vet](evidence/run-FFCfrI/vet.log) |
| go test -race ./internal/test/securityfixture -count=1 -tags=test,awsmock | PASS，0 | [fixture race](evidence/run-FFCfrI/fixtures-race.log) |
| go test ./internal/test/securityfixture -count=1 -tags=test -run TestM0S3 | PASS，0，真实 SDK + 回环模拟 S3 | [S3 driver](evidence/run-FFCfrI/s3-driver.log) |
| go test -race ./internal/test/securityfixture -count=1 -tags=test -run TestM0S3 | PASS，0 | [S3 driver race](evidence/run-r6ErJq/s3-driver-race.log) |
| go test ./internal/configuration/setup ./internal/webserver/authentication/sessionmanager ./internal/test/securityfixture -count=1 -tags=test,security_red -run TestSEC | FAIL，1，三个有意暴露的目标不变量 | [最终 RED](evidence/run-80e7sZ/red.log) |
| go build -o ./gokapi-m0 ./cmd/gokapi | PASS，0，仅构建 | [build](evidence/run-80e7sZ/build.log) |
| ./gokapi-m0 -v | PASS，0，未启动服务 | [binary version](evidence/run-80e7sZ/binary-version.log) |
| go version -m ./gokapi-m0 | PASS，0 | [build info](evidence/run-80e7sZ/build-info.log) |
| docker version | BLOCKED，命令退出127 | [docker](evidence/run-80e7sZ/docker.log) |
| docker build . -t gokapi-m0 | NOT_RUN：当前 WSL 无 Docker CLI；不启动宿主 Docker 或任何 VPS 容器 | — |
| go test -race ./... -count=1 -tags=test,awsmock | NOT_RUN：本轮没有生产并发逻辑变更，已针对新增 fixture 跑 race；不声称全仓无竞态 | — |
| go test ./... -tags=test,awstest -count=1 | NOT_RUN：额外条件组；本轮对真实 SDK 仅跑定向内存 S3 fixture，不代表完整 awstest | — |
| govulncheck ./... | NOT_RUN：没有修改依赖，本轮未固定/安装扫描器；SEC-062 Gate 保留，不能宣称无依赖漏洞 | — |
| Docker/浏览器 E2E/双浏览器 CSRF/真实 Redis 恢复/真实 S3/VPS | NOT_RUN | 后续按任务 Gate 执行 |

vet 的具体原有问题：cmd/gokapi/Main.go:76 向 signal.Notify 传无缓冲 channel；Webserver_test.go 多处 test.PostBody/Cookie/Header 使用未命名字段字面量。原始 HEAD 的 vet 日志已证明同样失败；M0 不混入修复或关闭检查。测试三组 PASS 不能覆盖这些静态告警。

## 新增回归的覆盖强度

| 测试 | 证明了什么 | 没有证明什么 |
| --- | --- | --- |
| TestM0ActorBoundaries/sqlite、redis | 真实 api.Process 的匿名/owner/admin/跨 owner/去重共享/临时/过期/File Request key 读取矩阵；撤销 Key；Session 数据往返；密码哈希可验证与过期模型；审查补齐权限相同的双向凭据类型边界 | 完整 CRUD/ZIP/replace/async 删除、所有浏览器 session 行为 |
| TestM0IDPProtocol | 临时 RS256 密钥、discovery/JWKS/Exchange/UserInfo 能通过 go-oidc 验证；同邮箱不同 issuer/sub；不同 verified 值 | Gokapi 回调是否验证 ID token、邮箱绑定可利用性、真实 AD FS/浏览器 Cookie 行为 |
| TestM0S3DeleteFailureRecovery | 真实 aws driver 在 DELETE=503 时返回错误且对象保留；取消故障后可删除；HEAD 正常与 DELETE 失败可分离 | storage.CleanUp 是否保留 metadata、真实 AWS IAM/持久化/供应商语义 |
| TestSEC010InitialSetupRequiresAuthorization | 已运行 RED：匿名初装请求会到达 next；无真实配置写入、无公网 listener | 实际 VPS 是否暴露初装、远程利用规模 |
| TestSEC012HTTPSCookieRequiresSecure | 已运行 RED：HTTPS 配置下生成 session Cookie 无 Secure | 浏览器跨站利用或完整 OIDC 兼容 |
| TestSEC040FailedDeletionKeepsManagementRecord | 已运行 RED：回环 S3 DELETE 失败、对象仍在而管理元数据消失 | 本地 EPERM、真实 S3、重启/所有者并发、修复后幂等性 |

security_red 是目标不变量测试，默认三组不包含这些故意失败的测试。M1/M4 对应修复必须让这些断言变 GREEN，再迁入默认回归组；不能通过改成当前不安全预期来“修测试”。三个失败的适用前提见 THREAT-MODEL，本轮未把它们命名为新 CVE。

## 二进制与镜像来源

本轮只执行版本查询的二进制来自 run-80e7sZ 的 HEAD archive + 隔离生成；没有 test build tags，新增夹具不进入二进制。SHA256：48afc9b18e15f05e41ff4ee5e58fb10838a27cf4fcbeb579d79fbb1f3ef805da，见 [binary.sha256](evidence/run-80e7sZ/binary.sha256)。版本输出 2.2.5-dev、Go1.26.0、IsDocker=false。

archive 没有 .git，因此二进制内 Git Commit 显示 None；对应源 SHA 由相邻 head.txt 和 overlay 清单提供，不能称其已有完整内嵌发布溯源。此哈希仅标识本轮构建，不承诺换路径/工具链后字节一致。用户确认尚未部署，当前仅以本地 fork 源码与构建为基线；没有运行中的 VPS/镜像，部署 digest 不适用。镜像尚未构建，未来发布 Gate 不通过。

## 复现与回滚

在具备 Go 1.26.0、gcc 和可达 Go module 源的 Linux/WSL 中，从仓库运行：

    bash docs/security/tools/run-baseline.sh
    bash docs/security/tools/run-baseline.sh --fixtures
    bash docs/security/tools/run-baseline.sh --red
    bash docs/security/tools/run-baseline.sh --s3-race

默认模式只测已提交 HEAD；--fixtures/--red 模式叠加明确列出的本轮文件并记录 hash。不在生产目录直接执行 go test 或手动 CleanUp。脚本不主动删除临时目录，但 OS/WSL 可清理 /tmp；workspace.txt 是本次执行位置记录，不承诺该目录持续存在。持久证据保存在仓库；重跑应创建新副本。以后清理前先核实绝对路径属于该次 /tmp/gokapi-m0-*。

本轮无数据迁移、无公开行为变更。回滚新增测试/文档即可，用户原计划目录保持原样。报告保留失败与修复后的结果，不修改原日志、不把 BLOCKED 变 PASS。

交付检查：[review.json](evidence/review.json)记录本轮文档相对链接均存在、常见私钥/访问令牌格式无匹配、已跟踪生产文件 diff 为空。合成密码/Key 明确限于 fixture；这项模式扫描不是全仓秘密审计，也不是对未访问的生产日志作保证。

## M0 逐项验收

| 要求 | 当前证据与结论 |
| --- | --- |
| 工作树/Go/HEAD/tag/master/module/Docker 基线及上游动态 | local-baseline.json、upstream.json、固定 SHA 源码表和本轮构建日志；通过。Docker 工具缺失已记录；用户确认无现有部署需追溯 |
| 指定核心路径带链接扫描 | SECURITY-BASELINE 覆盖所有指定路径、README、workflow、已有测试和指定 Issues/PR；通过 |
| 参与者、资产、边界、证据分类和每个 P0 的测试设计 | THREAT-MODEL 与 SECURITY-TASK-BOARD；010/012/040 有实跑 RED，其他按分类保留验证；通过 M0，不表示修复完成 |
| 官方测试、失败原样留存、代表性夹具 | 三组最终 PASS，vet 原有 FAIL，fixture/S3 race PASS，环境与编译失败历史保留；通过 M0 记录要求，vet 不豁免未来发布 Gate |
| 四份文档、SEC-010..063 顺序、小 PR 与 M1 建议 | 四文档齐备，保留原计划未分配编号；仅 fork PR 计划，无上游写入；通过 |
| 初始交付文件真实性 | 初始8个新增 Go 文件 hash 与 run-r6ErJq overlay 一致；gofmt -l 无输出；bash -n 退出0；git diff --check 退出0；未改已跟踪生产文件；后续 overlay 见审查增补 |

M0 基线与测试基础交付完成。SEC-010/012/040 的修复、OIDC/CSRF 浏览器验证、容器构建、完整 race、依赖扫描、真实存储恢复均仍是看板中的后续工作，不被本阶段验收吞并。

## SEC-003 PR 审查增补

审查基线先为 SEC-062 分支 `1d86b572734b3c885dd1a4a8f60433ed7b7258ca`，其 PR #1 通过 GitHub CI 后 squash 合并为 `1f47d1d91eda7da2098d2820c1633f88942f19f4`。SEC-003 从后者建立分支；两个提交的生产 Go 源码与初始 M0 一致。测试仍在 WSL Go1.26.0 的隔离副本中执行，未加载生产配置。

审查修正两项 CONFIRMED_CODE 测试工具问题：原 request-key 负例可能因权限位不足而误通过，现匹配普通 key 权限并补反向路由负例；原证据收集脚本总是退出0，现保留全部日志且失败时退出1，并启用管道失败传播。没有修改生产类型判断、授权或删除逻辑。新增 [check-credential-guard.sh](tools/check-credential-guard.sh) 只在 git archive 副本中移除一个类型判断，并检查两种 provider 的四个具体失败，拒绝用编译错误充当有效 mutation。

| 实跑命令 / 证据 | 真实结果 |
| --- | --- |
| `bash docs/security/tools/run-baseline.sh --fixtures`，[run-YZYD48](evidence/run-YZYD48/results.tsv) | 总退出1；generate PASS；awsmock FAIL，仅原有 Api_test.go:1108 的 TestDeleteFile 延迟删除断言失败；noaws、integration、fixture race、真实 S3 PASS；vet 与三个 opt-in RED FAIL |
| 同命令新副本重跑，[run-fEX5G8](evidence/run-fEX5G8/results.tsv) | 总退出1；generate、awsmock、noaws、integration、fixture race、真实 S3 PASS；vet 原有告警与三个 opt-in RED 继续 FAIL。最终新增 Go 文件对应此 overlay.sha256 |
| `bash docs/security/tools/check-credential-guard.sh`，[guard-ZVTgGi](evidence/guard-ZVTgGi/results.tsv) | 脚本退出0；generate PASS；`go test ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestM0ActorBoundaries -v` 控制组 PASS；同命令 mutation 组退出1，四个预期边界断言 FAIL，检测结论 PASS |
| mutation 的具体结果 | 两种 provider 均为 request key → management 返回200（期望401），ordinary key → request 返回400（期望401）；[原始断言](evidence/guard-ZVTgGi/mutant.log) |
| SEC-062 PR #1 CI | [run 40529619817](https://github.com/Activer007/Gokapi/actions/runs/40529619817) success，包含 generate 与官方四组；不是 SEC-003 新增文件的测试结果 |

TestDeleteFile 使用固定200ms+900ms等待异步删除；生产任务内部另有1s sleep，完成时刻还受调度和数据库 I/O 影响。此次失败重跑未复现，因此原因仍为 **VALIDATE**，不能仅凭后续 PASS 断言已修复。没有延长或删除断言来掩盖失败；run-YZYD48 原日志保留。已有 vet 告警、容器工具缺失和未执行项沿用上表，合并测试基础不等于通过部署 Gate。

复现 mutation 可运行上表脚本；它不会修改工作树中的 Api.go。普通基线脚本在本 PR 提交后默认 archive 自然包含已提交 fixture，`security_red` 仍须显式启用。回滚 SEC-003 的测试/文档提交即可，无数据库、文件内容、Cookie、哈希或 OIDC 身份迁移。SEC-062 发布保护应独立保留；恢复发布需先配置自己的 registry 和凭据。

暂存审查发现3处原始命令输出含尾随空白（build-info、早期无效 TSV 项、S3 错误日志）。不改写原始证据；.gitattributes 对 evidence 禁用文本转换及空白告警，对新增 shell/Go overlay 固定 LF，避免 Windows checkout 改变可执行脚本和已记录哈希。该属性仅覆盖本次新增材料，生产文件不受影响。

## M1 / SEC-010 初始化证明

本项执行跨2026-10-27/28（Asia/Shanghai）；初始M0日期与历史日志保持不变。

基线 `0b72099243e091eec4eb053154c46ce3dbf2fd7a`，Windows工作树 + WSL Ubuntu-24.04 / Go1.26.0；Docker CLI仍不存在。`run-sec010.sh` 在新的 git archive 副本叠加 setup Go 文件/模板并记录hash；所有安装配置、数据库、证明文件均在隔离副本或t.TempDir中生成。原用户计划目录不复制、不提交。该轮未改依赖、哈希或schema。

| 命令 / 证据 | 真实结果 |
| --- | --- |
| `bash docs/security/tools/run-sec010.sh --red`，[sec010-NzpQ4p](evidence/sec010-NzpQ4p/results.tsv) | generate PASS；SEC-010 FAIL：首次setup无凭据/错误凭据均触达回调，原始M0不变量也失败；是真实断言RED |
| 首版完整回归，[sec010-5xqY10](evidence/sec010-5xqY10/results.tsv) | generate PASS；Go测试编译FAIL：新测试对临时值调用指针方法toJson；已改为局部变量。这不是安全RED |
| 修正测试编译后，[sec010-NyYzaU](evidence/sec010-NyYzaU/results.tsv) | generate、SEC-010、awsmock、noaws、integration、定向race均PASS |
| 同源/方法防御负例，[sec010-HVJfVy](evidence/sec010-HVJfVy/results.tsv) | generate PASS；SEC-010 FAIL：三种跨站来源返回200，GET保存入口返回500而非405；保留并加入最小防护 |
| 最终完整回归，[sec010-0fE7Rw](evidence/sec010-0fE7Rw/results.tsv) | generate、SEC-010、awsmock、noaws、integration、定向race均PASS；退出0。最终生产/测试内容对应此overlay.sha256；随后脚本仅加参数检查与自身hash记录 |

最终实际命令：`go generate ./...`；`go test ./internal/configuration/setup -count=1 -tags=test,noaws,security_red -run TestSEC010 -v`；`go test ./... -parallel 8 -count=1 -tags=test,awsmock`；`go test ./... -parallel 8 -count=1 -tags=test,noaws`；`go test ./... -count=1 -tags=test,noaws,integration`；`go test -race ./internal/configuration/setup -count=1 -tags=test,noaws -run TestSEC010`。全部PASS，逐条.command.txt和.log保留。

实际安装TestIntegration现在用本地证明文件读取的凭据走HTTP：匿名完整配置POST不创建配置，认证后显示页面，错误表单不消耗证明，正确安装成功，文件被删除，再次提交不能改变配置；既有重配内部/Header/OAuth模式测试继续通过。TestInitialSetup覆盖已有配置跳过。专门测试覆盖初装/重配凭据矩阵、0600权限、重启轮换/旧证明拒绝、native/Docker监听地址、HTTP/代理HTTPS同源正例、跨站/opaque Origin反例、GET拒绝、16个并发完成态重放。并发测试只证明完成态重放与互斥路径，不声称覆盖全部存储并发。

M0的SEC-010测试已迁入默认test组并GREEN；SEC-012/040仍为后续opt-in RED，未由本轮修复。Docker真实容器/非root上传下载、实际浏览器行为、完整OIDC回调、全仓race、govulncheck本轮NOT_RUN；不能用本轮HTTP与地址单测替代这些M1 Gate。已有vet告警仍见M0报告。本轮采用标准库来源保护的依据见ADR-001。

兼容与回滚详见[ADR-001](ADR-001-BOOTSTRAP-PROOF.md)。现有服务正常启动不改变；初装/重配须读取本地证明，远程native安装须使用SSH隧道。回滚无需数据转换，但恢复旧代码必须保持安装端口网络隔离。M1其余四个SEC-ID及旧密码迁移验证继续保留，整个目标未完成。

## M1 / SEC-012 Cookie、会话与旧密码迁移

2026-10-28，基线 `c1e0ee2d78c8b8ddb951b5f1053f6d5e15572a31`（SEC-010 PR #3 已合并；其最终head a6039e2，CI runs 40537227199/40537217848 均成功）。环境 Windows PowerShell + WSL Ubuntu-24.04、Go1.26.0 linux/amd64；每次 `run-sec012.sh` 新建 git archive 副本，按明确文件列表叠加并记录 hash，合成配置/数据只在临时目录。没有部署或修改生产数据。

| 命令 / 证据 | 真实结果与原因 |
| --- | --- |
| `bash docs/security/tools/run-sec012.sh --red`，[sec012-Y6hWxh](evidence/sec012-Y6hWxh/results.tsv) | generate PASS；目标测试 FAIL，三类HTTPS Cookie没有Secure、网页改密会话仍有效。这是真实RED；该次尚无legacy迁移测试，fixture包no tests不能算验证通过 |
| 首轮完整回归，[sec012-yOFUvA](evidence/sec012-yOFUvA/results.tsv) | generate PASS；目标/三组/race FAIL；新增断言错误期望相对Location，Go重定向规范化为/login和/admin。测试错误已修正，不当作安全RED；Cookie与legacy迁移断言已通过 |
| 修正重定向断言后，[sec012-Wn12rR](evidence/sec012-Wn12rR/results.tsv) | generate PASS；目标/三组/race FAIL，仅HTTPS代理测试的HTTP CookieJar筛选断言失败。Go1.26 cookiejar secureMatch明确把localhost/loopback视为安全来源；不是生产Secure修复失效 |
| 最终完整回归，[sec012-ax6ZPF](evidence/sec012-ax6ZPF/results.tsv) | environment、generate、目标SEC012、awsmock、noaws、integration、targeted-race全部PASS，脚本退出0；生产/测试文件对应overlay.sha256 |

最终真实命令（逐条.command.txt/.log保留）：

```sh
go generate ./...
go test ./internal/webserver ./internal/webserver/authentication/sessionmanager ./internal/webserver/authentication/oauth ./internal/test/securityfixture -count=1 -tags=test,noaws,security_red -run TestSEC012 -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver ./internal/webserver/authentication/sessionmanager ./internal/webserver/authentication/oauth ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestSEC012
```

HTTPS代理测试使用example.com主机语义，transport禁用代理并强制只连接本地fixture，TLS仍验证证书，不访问真实域名；避免loopback的Secure例外造成错误结论。登录发state→错误state拒绝→正确回调兑换合成token/UserInfo→会话访问→退出失效，HTTP与TLS反代均通过，后端始终HTTP且冲突X-Forwarded-Proto不能改变策略。CookieJar不执行SameSite，SameSite只断言属性；实际浏览器NOT_RUN。没有更改OIDC邮箱关联或宣称补齐ID token验证。

网页改密断言成功更新hash、清除本用户两设备会话和浏览器Cookie，其他用户会话保留；无效CSRF不改密码、不注销。legacy测试独立生成v2.2.4 SHA-1 fixture，调用真实认证路径，在SQLite/miniredis分别验证错误不迁移、成功Argon2id写回、重连持久化、重复正确/错误认证与原记录恢复。恢复单条记录不是旧二进制全量恢复；真实Redis持久化和冷恢复NOT_RUN。

SEC-012原M0 opt-in RED现迁入默认test组；SEC-040仍需显式security_red。依赖未变，govulncheck及全仓race本轮NOT_RUN，原有vet告警未修复。更新环境事实：Windows已安装Docker CLI29.1.3，但Docker Desktop Linux引擎命名管道不存在、WSL无Docker CLI/socket；镜像/非root容器Gate仍BLOCKED，不再笼统称整台机器没安装Docker。

兼容/发布/回滚详见[ADR-002](ADR-002-COOKIE-AND-PASSWORD-ROLLOUT.md)。本项无schema、身份或文件格式变化；旧Cookie需要维护窗口注销再登录，降回v2.2.4必须恢复配套备份。测试/PR通过不等于已发布镜像或M1全部完成。

[PR #4](https://github.com/Activer007/Gokapi/pull/4) 创建于本 fork，初始提交 `f780a84f9e3ed59b8c2a560f48167724c3c890d8`。远端diff已逐项自审并记录COMMENT，不冒充独立审批；后续仅补PR链接和本说明。提交前gofmt、bash -n、diff --check和文档相对链接校验通过，新增证据及fixture的常见私钥/GitHub Token/AWS访问Key模式扫描无匹配；这是有限模式扫描，非全仓秘密审计。合并必须等待最终head的远端CI通过，动态结果以PR checks为准。

SEC-012合并核实：PR #4最终head `bb2008ff755482f877d668c42902bb32c445fa3d`，合并提交 `7200526486d7a4b317043fe7056e6d279e1c92f3`。CI [40542126888](https://github.com/Activer007/Gokapi/actions/runs/40542126888)首轮通过；[40542116738](https://github.com/Activer007/Gokapi/actions/runs/40542116738)首轮FAIL于原有TestDeleteFile/Api_test.go:1108，固定200ms+900ms后记录仍存在；同SHA重跑通过，失败attempt及PR审查评论保留。两条最终CI均执行生成和官方四组，不能记成首次全绿或宣称删除问题已修。

## M1 / SEC-013 可信代理

2026-10-28，基线 `7200526486d7a4b317043fe7056e6d279e1c92f3`；WSL Ubuntu-24.04 / Go1.26.0 linux/amd64，PowerShell工作树。`run-sec013.sh` 从HEAD新建archive临时副本，仅叠加清单中的生产/测试文件，记录hash及脚本hash；没有生产配置/数据。测试非并行，完成后恢复环境和包级配置。

| 命令 / 证据 | 真实结果 |
| --- | --- |
| `bash docs/security/tools/run-sec013.sh --red`，[sec013-lQ9aSZ](evidence/sec013-lQ9aSZ/results.tsv) | generate PASS；SEC013 FAIL，直接peer/CF/畸形与多行代理头、Header Auth、默认Docker网段信任出现预期断言失败；可信正例通过，是有效RED |
| 首次修复完整回归，[sec013-QkoMZC](evidence/sec013-QkoMZC/results.tsv) | generate、SEC013、awsmock、noaws、integration、定向race全部PASS |
| 增补真实HTTP与Docker模式断言后，[sec013-8pVkpv](evidence/sec013-8pVkpv/results.tsv) | generate、SEC013、noaws、integration、定向race PASS；awsmock FAIL仅原有TestDeleteFile/Api_test.go:1108，延迟记录仍存在，未改该生产实现/断言；原因仍VALIDATE |
| 只重跑失败组：`bash docs/security/tools/run-sec013.sh --awsmock`，[sec013-yJt6Pn](evidence/sec013-yJt6Pn/results.tsv) | 新隔离副本generate及awsmock PASS，退出0；与8pVkpv叠加源码/测试hash相同，原失败保留。最终三组均已通过，不声称删除抖动已修 |

真实命令：

```sh
go generate ./...
go test ./internal/logging ./internal/environment ./internal/webserver/authentication -count=1 -tags=test,noaws -run TestSEC013 -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/logging ./internal/environment ./internal/webserver/authentication -count=1 -tags=test,noaws -run TestSEC013
```

覆盖：不可信peer不能改变来源IP或通过Header Auth；CF/X-Real-IP重复/非法值被拒绝；多行XFF保留顺序、可信链从右向左到首个不可信跳点、畸形链退回peer、全可信链取最左端；IPv6回环/CIDR；Docker单代理地址/邻居拒绝/明确CIDR内外边界；默认不自动授信网段，显式旧选项仍可解析。Header已注册用户通过、未知用户拒绝、缺失/重复头拒绝，自动注册模式下未授权请求不创建用户。真实回环HTTP相同请求在未配置peer时返回401、显式配置后204及正确IP。

仅为现有Header正例设置可信RemoteAddr；不删弱化负例。未更改数据库、密码哈希、OIDC账户关联、文件内容/删除、API或界面。普通三组继续包含SEC-010安装与SEC-012回调/迁移回归；Docker配置模拟不等于真实容器网络/非root上传下载。浏览器、真实容器、全仓race、govulncheck本轮NOT_RUN；无依赖修改，原vet告警保留。环境与代理迁移/回滚见[ADR-003](ADR-003-TRUSTED-PROXY-BOUNDARY.md)，M1剩余SEC-011/014未完成。

[PR #5](https://github.com/Activer007/Gokapi/pull/5)初始提交 `86d9afd5591f17df3c6718b73ff42b8aa8cf3575`；远端diff已自审并记录COMMENT，非独立审批。后续只补PR链接与本说明。提交前gofmt、bash -n、diff --check及文档相对链接检查通过；本轮新增证据/fixture的常见私钥、GitHub Token、AWS访问Key模式扫描无匹配（非全仓秘密审计）。8pVkpv与yJt6Pn的overlay hash完全一致。最终head的CI及合并结果以PR checks为准，合并前必须核实成功。

SEC-013合并核实：PR #5最终head `7b1153489bccdb0aa09ee79898e024dff0790847`，CI [40545422637](https://github.com/Activer007/Gokapi/actions/runs/40545422637)和[40545415672](https://github.com/Activer007/Gokapi/actions/runs/40545415672)成功，包含generate及官方四组。合并为 `138642c72ea481a2b63d2ed5e1aac071de34a01d`，本地master已核对同步。

## M1 / SEC-014 请求阶段与取消

2026-10-28，基线 `138642c72ea481a2b63d2ed5e1aac071de34a01d`，WSL Ubuntu-24.04 / Go1.26.0 linux/amd64。`run-sec014.sh` 在新git archive副本叠加清单文件并记录hash；合成账号、文件、HTTP服务只存在于隔离副本。原用户计划目录未复制或提交。

| 命令 / 证据 | 真实结果 |
| --- | --- |
| `bash docs/security/tools/run-sec014.sh --red`，[sec014-Xtpzsv](evidence/sec014-Xtpzsv/results.tsv) | generate PASS；目标FAIL：取消请求仍等约1秒，128查询留下128协程；主服务和安装服务不完整头到7秒客户端截止仍未关闭。均为实际断言RED |
| 首次修复完整回归，[sec014-4tudQI](evidence/sec014-4tudQI/results.tsv) | generate、SEC014、awsmock、noaws、integration、定向race全部PASS；包括五类等待、HTTP断线、主/安装慢头及大文件/SSE |
| 审查增补API取消与修正测试清理后，[sec014-NVbGZp](evidence/sec014-NVbGZp/results.tsv) | generate PASS；目标/三组/race FAIL：新增API正常对照Key缺少ApiPermView，正例被拒绝。属于测试夹具错误，不是安全RED；改为独立合成Key并保留权限检查 |
| 最终完整回归，[sec014-TTDznp](evidence/sec014-TTDznp/results.tsv) | environment、generate、SEC014、awsmock、noaws、integration、定向race全部PASS，脚本退出0；生产与测试文件对应overlay.sha256 |

实际命令（各次.command.txt/.log保留，早期目标包列表不同，以原文件为准）：

```sh
go generate ./...
go test ./internal/webserver/ratelimiter ./internal/webserver ./internal/webserver/api ./internal/configuration/setup -count=1 -tags=test,noaws -run 'TestSEC014|TestIntegration' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver/ratelimiter ./internal/webserver ./internal/webserver/api ./internal/configuration/setup -count=1 -tags=test,noaws -run TestSEC014
```

不变量覆盖：五类限流的未取消正例/耗尽后等待/取消返回，真实HTTP断线取消处理器；取消登录、API Key、bootstrap证明不继续授权；128次查询常驻协程增长有界；真实TCP慢头在主/安装服务约5秒被关闭。原安装证明及重配流程继续通过。目标race仅TestSEC014，不包含整个安装集成测试或全仓。

长传输正例通过真实/api/files/add上传16,896,000字节，超过fixture 10MiB multipart内存阈值，中途暂停6秒，再经/downloadFile匿名下载并核对字节数及SHA-256；在同一SSE连接收取上传暂停之后的事件。测试仅清理自身元数据，TestMain清除隔离文件目录，不启动全局异步GC；生产测试内容的协程退出前必等待结束。此测试不等同真实S3、100GB、E2E浏览器或生产吞吐验收。

兼容/回滚见[ADR-004](ADR-004-HTTP-PHASE-AND-CANCELLATION.md)：仅内部限流签名和HTTP阶段期限改变，公开API、数据、身份、存储格式不变。IdleTimeout=60秒已核实配置，专门60秒空闲连接实测NOT_RUN；真实反代、容器、全仓race、govulncheck亦NOT_RUN，无依赖变化；原vet告警保留。M1非root容器Gate仍待SEC-011，未发布。

提交前检查：WSL /usr/bin/go 为 go1.26.0 linux/amd64，gofmt、bash -n、git diff --check通过；新增证据常见私钥/GitHub Token/AWS访问Key模式扫描无匹配（有限模式扫描，不是全仓秘密审计）。Windows PATH无go；首次格式检查误指定不存在的/usr/local/go/bin/go后，改用既有/usr/bin/go完成检查，未影响已记录的测试。

[PR #6](https://github.com/Activer007/Gokapi/pull/6)初始提交 8defad1a80046b156cf662de6ce8d9e659f45429；远端diff已自审并记录COMMENT，非独立审批。审查修正测试清理与API正例夹具；本次后续提交只补PR链接与审查说明。最终head的CI通过后方可合并，动态结果以PR checks为准。

SEC-014合并核实：PR #6最终head 99da9bbb0c2c7c9d58e8a9c0bbc68b9a16a5ae00，两条CI [40549566906](https://github.com/Activer007/Gokapi/actions/runs/40549566906) / [40549557374](https://github.com/Activer007/Gokapi/actions/runs/40549557374)均成功，日志含generate和官方四组；合并462c5d57dc428ccedb352b2c84c4367331687872。本地已同步。警告仅Node20 action运行时和ubuntu-latest迁移，未混入本项升级。

## M1 / SEC-011 非 root Compose

基线462c5d57dc428ccedb352b2c84c4367331687872，2026-10-28。Docker Desktop4.55.0 / Engine29.1.3 linux/amd64，Compose2.40.3，Windows Python3.13；Go回归在WSL Ubuntu24.04/Go1.26.0运行。实际镜像沿用Dockerfile内Go1.26.2-alpine / Alpine3.23。本地构建，不发布、不操作已有生产数据。

| 命令/证据 | 真实结果 |
| --- | --- |
| `python docs/security/tools/run-sec011.py --red`，[2brmsc02](evidence/sec011-2brmsc02/results.tsv) | FAIL（有效RED）：继承Compose缺少显式非root用户 |
| 首次构建，[fs_cy7nf](evidence/sec011-fs_cy7nf/results.tsv) | FAIL：Docker Hub匿名认证地址连接超时，尚未进入应用构建 |
| 使用已有本机代理重试，[nhb1_0ll](evidence/sec011-nhb1_0ll/results.tsv) | 构建PASS；脚本FAIL于Docker省略空User字段，修正读取默认值；非安全RED |
| [0x48knwb](evidence/sec011-0x48knwb/results.tsv) / [74ktp7l3](evidence/sec011-74ktp7l3/results.tsv) | FAIL：entrypoint无法执行；后者保留安装前日志。镜像内shebang含CRLF，Git原对象为LF；Windows归档转换导致，不是权限负例通过 |
| 关闭archive换行转换重新构建，[pm9ov6yg](evidence/sec011-pm9ov6yg/results.tsv) | 镜像/错误属主拒绝/属主不变/非root身份/证明0600/未授权不写配置PASS；合法安装FAIL于夹具缺少既有API要求的未启用OAuth及enc_pw字段，补全夹具，未改认证实现 |
| [scudb4a0](evidence/sec011-scudb4a0/results.tsv) | 初始化/登录/上传/下载SHA256 PASS；重启容器健康，但Docker随机主机端口从32794变32795，等待旧端口失败。确认根因后中断本轮等待，按精确容器ID及卷标签清理；不计为持久化PASS |
| 最终 `python docs/security/tools/run-sec011.py --image gokapi-sec011:462c5d57dc42`，[bqfn7tyx](evidence/sec011-bqfn7tyx/results.tsv) | 全部PASS、退出0：使用固定空闲端口和匿名readiness；真实容器权限、初始化、上传/下载、重启持久化与已有配置跳过bootstrap通过；最终清理检查未残留测试容器/卷/网络 |
| `bash docs/security/tools/run-sec011-go.sh`，[Ce9VCG](evidence/sec011-go-Ce9VCG/results.tsv) | environment、generate、awsmock、noaws、integration全部PASS、退出0 |

Windows命令显式选择已安装的Docker CLI；成功构建重试仅对子进程设置HTTP_PROXY/HTTPS_PROXY为既有localhost:1080，不改全局网络或Docker配置。一次诊断命令因可能打印完整代理URL被自动审批拒绝，未执行；改为只显示host/port后通过，无凭据输出。初始Docker引擎未启动，已后台启动；WSL无docker.sock，因此容器测试通过Windows CLI完成。

构建输入为 `git -c core.autocrlf=false archive HEAD` tar，经stdin送入 `docker build --progress=plain -t gokapi-sec011:462c5d57dc42 -`。成功修正构建记录在pm9ov6yg/build.log和archive.sha256；最终复用同一不可变image ID `sha256:b02c4a679762bf466dba15f831d36236347e4eea071cea3386c500e536a00c50`，见两次image.json。Docker镜像默认User仍空/root；实际Compose应用进程四个UID均10001，GID10001，根FS只读、cap_drop ALL/no-new-privileges生效。二进制-v显示2.2.5-dev、Go1.26.2、linux/amd64、IsDocker=true；archive构建不含.git，二进制Git Commit为None，源对应关系由archive/hash/runner建立，不能宣称二进制自带可复现来源证明。

使用真实Compose文件（只注入本轮镜像/UID/GID/随机卷名/空闲端口），全新外部Linux命名卷最初root-owned；应用未能创建证明且卷属主不变。测试helper仅对两个仍为空的卷根目录chown/chmod，无递归；随后证明文件0600，未授权设置401且无config，合法设置和登录成功。经会话获得上传临时Token，上传147,456字节合成文件，再匿名下载校验SHA256；重启后字节仍一致、没有新bootstrap文件。凭据与分享ID仅在内存中，证据不保存HTTP响应或认证后日志。

Go回归真实命令：

```sh
go generate ./...
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
```

本项无Go并发/依赖变更，新增-race和govulncheck为NOT_RUN；既有SEC-014定向race记录保持有效，原有vet告警未修。默认镜像USER、原Compose、数据库和文件格式不变；只新增独立profile、entrypoint LF属性和测试/文档。兼容、旧卷副本迁移及回滚见ADR-005。真实旧卷迁移、rootless Docker、SELinux、真实Redis/S3容器和浏览器E2E为NOT_RUN。基础镜像digest固定、发布签名仍归SEC-062，不把本地image ID当成发布完成。

M1逐项复核：SEC-010未授权初始化、SEC-012 Cookie/HTTP与TLS回调/旧密码迁移、SEC-013代理矩阵、SEC-014慢头/取消/大文件/SSE、SEC-011真实非root容器Gate均已有对应实跑证据。第5项“短时管理请求与长时文件传输”目前只证明头/空闲阶段分离，尚无独立管理handler执行预算；继续做SEC-014范围核验，M1暂不标完成。

提交前Python AST、bash -n、git diff --check、文档相对链接校验通过；本轮新增证据常见私钥/GitHub Token/AWS访问Key/Authorization/session Cookie模式扫描无匹配（有限扫描，非全仓秘密审计）。测试脚本自审核对了容器ID/卷标签清理范围、安装前日志范围、镜像复用来源标注、固定端口和匿名重启就绪检查。

[PR #7](https://github.com/Activer007/Gokapi/pull/7)初始提交5e611a211b050b91084a716935be863bfe7e835a；远端diff已自审并记录COMMENT，非独立审批。提交后用默认git archive HEAD读取dockerentry.sh字节，确认无CR、LF属性生效。本次后续仅补PR/审查记录，最终head CI成功后合并，动态状态以PR checks为准。

SEC-011合并核实：PR #7最终head ccb7c7aac44dd90037e1f653110b45a15a2b22e8，CI [40553793808](https://github.com/Activer007/Gokapi/actions/runs/40553793808) / [40553783138](https://github.com/Activer007/Gokapi/actions/runs/40553783138)成功，含generate与官方四组；合并863fdb5442e8709e1639dfaa50ea26232eff7ec4，本地master已核实同步。

## M1 / SEC-014 管理请求预算补充

基线863fdb5442e8709e1639dfaa50ea26232eff7ec4；2026-10-28；WSL Ubuntu24.04 / Go1.26.0 linux/amd64。run-sec014-budget.sh在新archive副本叠加明确清单，记录源码/runner hash，使用合成账号/IdP/文件；不操作生产数据。

| 命令/证据 | 真实结果 |
| --- | --- |
| `bash docs/security/tools/run-sec014-budget.sh --red`，[jvpaI0](evidence/sec014-budget-jvpaI0/results.tsv) | generate PASS；目标FAIL：完整头后的/login正文超过35秒仍等待；已取消回调仍联系Token端点。原HTTP/TLS回调正例PASS，有效RED |
| 首次完整执行，[p5ypuG](evidence/sec014-budget-p5ypuG/results.tsv) | generate、目标、三组、定向race各命令均PASS；但运行中的shell文件被修改，收尾读偏移错乱，额外命令125及语法错误，整轮FAIL/退出1。保留原results、额外命令日志与说明，不能称整轮通过 |
| 最终完整重跑，[DJeLeh](evidence/sec014-budget-DJeLeh/results.tsv) | environment、generate、目标、awsmock、noaws、integration、定向race全部PASS，脚本退出0。脚本整个run_suite函数在执行前解析；测试审查补强了回调协程清理和慢正文最短等待断言 |

真实命令：

```sh
go generate ./...
go test ./internal/webserver ./internal/webserver/api ./internal/webserver/authentication/oauth ./internal/webserver/requestbudget -count=1 -tags=test,noaws -run 'TestSEC014|TestSEC012Callback' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver ./internal/webserver/api ./internal/webserver/authentication/oauth ./internal/webserver/requestbudget -count=1 -tags=test,noaws -run 'TestSEC014|TestSEC012Callback'
```

覆盖：真实应用/login慢正文在约30秒结束，未发会话；管理/传输页面与全部API路由分类，错误路径不获得传输预算；HTTP/1和真实协商HTTP/2慢读退出；不读取响应的TCP客户端不能使管理写阻塞超过预算；正常管理请求后同一keep-alive连接仍完成超出短预算的文件响应，PathValue保留。IdP Token/UserInfo两个阶段的真实HTTP连接随请求deadline断开，取消回调不兑换、不授予会话，HTTP/TLS反代回调仍正常。实际16,896,000字节multipart上传暂停31秒，SSE同连接继续接收事件，下载长度/SHA256一致。

不是强制抢占：预算约束网络I/O及响应Context的调用，已开始且不响应Context的CPU/DB/存储工作可能继续，不自动撤销写入、不保证特定超时状态码。没有新增后台处理器或响应缓存；权限判断、公开HTTP路径、schema、密码、OIDC身份关联和物理删除不变。安装服务原2分钟预算保留。补充兼容与回滚见ADR-004。全仓race、govulncheck、100GB/真实代理负载/浏览器/生产冷恢复本轮NOT_RUN；无依赖修改，原vet问题仍在。

提交前gofmt、bash -n、diff --check、文档相对链接通过；新增证据常见私钥/GitHub Token/AWS访问Key/Authorization/session Cookie模式无匹配（有限扫描）。只读上游timeout PR检索无结果。默认镜像USER仍root，完整Go回归继续覆盖SEC-010安装/重配、SEC-012 Cookie/旧密码、SEC-013代理边界；本轮提交后的最终镜像尚待重建并复跑SEC-011，合并以最终PR checks为准。

提交后验收（替代上段“尚待重建”状态）：[PR #8](https://github.com/Activer007/Gokapi/pull/8)代码提交 `4bc1fc3c8a86c1bc39d6ce92ef2e4db7474280df`；远端diff已自审并记录COMMENT，非独立审批。Windows Python3.13执行 `python docs/security/tools/run-sec011.py`，从该提交的LF archive重新构建镜像（未复用旧镜像），[wtswvd6t/results.tsv](evidence/sec011-wtswvd6t/results.tsv)全部PASS、退出0。镜像 `gokapi-sec011:4bc1fc3c8a86`，不可变ID `sha256:fe7681a6e0bd8a3c3460ba896096f364d4ab1daaab1a0570c7a2ba18d477e0ad`；源archive、runner、Compose摘要及构建日志一并保留。环境沿用Docker29.1.3/Compose2.40.3，本次构建仍使用进程内既有代理。

最新容器验证错误卷属主拒绝且不改权限、实际应用UID/GID10001、只读根FS及降权选项、证明0600、无证明401且无配置、合法初装/登录、147,456字节上传与匿名下载SHA256、重启持久化和已有配置无新bootstrap。测试只使用新建标记卷；清理退出成功，镜像保留供检查。这是本地SQLite/文件驱动验收，不推断真实S3/Redis或旧生产卷迁移。后续提交只补证据和验收文档，应用源码与被测提交相同。最终CI/合并结果以PR #8 checks与合并记录为准。

## M1 逐项验收审计

审计源码为 `4bc1fc3c8a86c1bc39d6ce92ef2e4db7474280df`；最新完整Go证据为[DJeLeh](evidence/sec014-budget-DJeLeh/results.tsv)，最新真实容器证据为[wtswvd6t](evidence/sec011-wtswvd6t/results.tsv)。以下为本阶段工程Gate，不代表整个安全计划或发布Gate完成；PR #8仍须最终head CI成功后合并。

| M1要求 | 代码、可复现证据与结论 | 兼容边界 |
| --- | --- | --- |
| 1. 未授权不能初始化，保留初装/重配 | SEC-010，[bootstrap_test.go](../../internal/configuration/setup/bootstrap_test.go)证明缺失/错误证明拒绝、同源/方法边界、单次提交及并发重放；[Setup_test.go](../../internal/configuration/setup/Setup_test.go)实际安装/重配集成；最新非root容器无证明401且不写配置。PASS | ADR-001；本机回环/SSH隧道、容器显式网络；不使用生产配置 |
| 2. 非root持久化可写，上传下载正常 | SEC-011，最新容器全流程PASS，错误属主保持不变，UID10001及重启后SHA256一致 | ADR-005；默认镜像USER仍root，profile显式选择。旧卷隔离副本迁移/回滚有步骤，真实旧生产卷迁移NOT_RUN |
| 3. Cookie、回调、会话撤销 | SEC-012，sessionmanager/webserver/oauth的security_cookie_test及[security_callback_test.go](../../internal/webserver/authentication/oauth/security_callback_test.go)覆盖HTTP/对外HTTPS、真实TLS代理回调；网页改密撤销自身会话；最新三组回归与HTTP/TLS目标正例PASS | ADR-002；HttpOnly/SameSite保留，绝对会话上限已评估但未改schema；CookieJar不是浏览器SameSite测试，撤销并发矩阵归SEC-021 |
| 4. 代理信任与身份头 | SEC-013，[logging矩阵](../../internal/logging/security_proxy_test.go)覆盖不可信直接peer、CF/XRI/XFF、多层/多行、IPv6、Docker显式IP/CIDR；[Header Auth HTTP测试](../../internal/webserver/authentication/security_proxy_test.go)拒绝不可信/重复身份头且不自动注册；环境测试证明Docker信任需显式选择。最新三组PASS | ADR-003；未声称真实Cloudflare服务或多容器代理拓扑验收。非rootprofile明确关闭隐式Docker信任 |
| 5. 管理与传输预算、慢请求/取消 | SEC-014，主服务/安装慢头、五类限流取消、清理协程有界；本次30秒管理慢正文、HTTP/1与HTTP/2慢读、慢写、keep-alive、两阶段IdP断线；16,896,000字节上传暂停31秒、SSE与下载SHA256。目标/三组/定向race PASS | ADR-004；仅网络I/O及可取消工作有界，CPU/DB不强制抢占或事务回滚；100GB/真实代理负载NOT_RUN |
| 6. SHA-1/Argon2id版本差异及迁移 | HISTORICAL_FIXED：稳定版v2.2.4为SHA-1，开发线已有上游#384 Argon2id；[legacy_migration_test.go](../../internal/test/securityfixture/legacy_migration_test.go)真实登录路径验证错密不变、正确登录升级、SQLite/miniredis重连、原记录恢复。最新三组PASS，无重复算法改写 | ADR-002给出停写、一致备份、保留salt、升级验收和回滚条件；旧二进制全量冷恢复及真实Redis持久化NOT_RUN，归SEC-063 |

M1所需实现、RED→GREEN、本地回归、兼容方案和最新镜像Gate均有记录。发布G4仍未完成：未部署VPS、未发布镜像，基础镜像digest固定/签名、govulncheck及完整冷恢复仍待后续任务。下一轮P0优先SEC-040删除故障恢复及SEC-020/021权限撤销矩阵，随后SEC-030/031/032资源额度；P1推进SEC-062可复现发布与SEC-063冷恢复。未知疑点保持VALIDATE，不升级为漏洞结论。

## M2 / SEC-020 管理授权矩阵与纯策略

2026-10-28，基线c14e6c73a49a5d5a89d815cf4b660da25cc98d56，WSL Ubuntu24.04、Go1.26.0 linux/amd64。Windows无Go；WSL普通沙箱访问被系统拒绝，授权执行后可运行。`run-sec020.sh`在新建/tmp archive中覆盖显式源码清单，记录HEAD、overlay及runner SHA256；测试仅使用临时SQLite、miniredis及合成blob/身份。未操作生产数据或用户计划目录。

| 执行 / 证据 | 真实结果 |
| --- | --- |
| 初始现状矩阵 [jwGxkS](evidence/sec020-jwGxkS/results.tsv) | generate和已有对象/凭据矩阵PASS；尚无当前用户交集RED |
| 防御性RED [awBQ84](evidence/sec020-awBQ84/results.tsv) | generate PASS，目标FAIL；SQLite/Redis、永久/临时Key、四个入口合计16个旧权限状态返回200而期望401。直接注入数据库故障状态，不宣称外部可利用 |
| 策略后目标 [pRponk](evidence/sec020-pRponk/results.tsv) | generate、矩阵及签发回归PASS |
| 首次完整 [nIwpdl](evidence/sec020-nIwpdl/results.tsv) | generate/矩阵/定向race PASS；awsmock/noaws/integration FAIL。旧API测试只给Key位、未给用户管理位，导致401及后续夹具唯一键冲突；失败原样保留 |
| 修正夹具 [rlDEHT](evidence/sec020-rlDEHT/results.tsv) | generate、矩阵、API全量PASS。只按动作授予测试用户相应位并Cleanup恢复，未放宽生产策略或删除原拒绝断言 |
| 最终完整 [BS2VtP](evidence/sec020-BS2VtP/results.tsv) | environment、generate、矩阵、API全量、awsmock、noaws、integration、定向race全部PASS，脚本退出0；源码与runner摘要见同目录 |

真实命令（最终完整执行见后续记录）：

```sh
go generate ./...
go test ./internal/test/securityfixture ./internal/authorization ./internal/webserver/api ./internal/webserver/authentication/tokengeneration -count=1 -tags=test,noaws -run 'TestSEC020|TestGenerate' -v
go test ./internal/webserver/api -count=1 -tags=test,noaws
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/test/securityfixture ./internal/authorization ./internal/webserver/api ./internal/webserver/authentication/tokengeneration -count=1 -tags=test,noaws -run 'TestSEC020|TestGenerate'
```

矩阵覆盖超级管理员/管理员/U1/U2的永久、临时、会话生成Token，以及匿名、File Request、过期/撤销Key、已删除用户状态的12类管理动作；检查拒绝后元数据/blob不变、列表过滤、下载/ZIP字节、presign签发范围、修改/替换/复制/恢复结果。两种provider覆盖替换目标、源读取、可选源删除的11组合，包括不同所有者共享blob。全路由反向凭据类型测试确保普通Key和File Request Key不能互换；纯策略验证未知动作/权限、无效主体及委托撤销。

范围限制：新矩阵的允许物理删除分支明确SKIP，原API删除正例随全量回归执行，不能替代异步撤销/生命周期Gate。session-token走真实sessionmanager和Generate，但不模拟浏览器；已撤销/已删除状态不代表真实撤销并发已验证。公开分享、presign消费、混合ZIP成员、分片UUID、File Request上传、在途请求撤权、OIDC绑定及跨浏览器CSRF仍为后续专项Gate。全仓race、真实Redis/S3、govulncheck本项NOT_RUN；无依赖改动，既有vet限制不在本PR修复。

兼容与回滚：公开API/错误结构、SQLite/Redis数据格式、页面/WASM、OIDC/密码与物理删除不改；不需要数据迁移。回滚SEC-020提交即可恢复旧授权行为，但同时失去本次当前用户交集检查。纯策略不产生缓存、数据库连接或后台任务，不是新的身份系统，因此不新增架构ADR；SEC-022身份迁移另立ADR。

RED复现方式：在新临时目录展开 `git -c core.autocrlf=false archive c14e6c73a49a5d5a89d815cf4b660da25cc98d56`，仅复制本PR的 `internal/test/securityfixture/authorization_matrix_test.go`，先运行 `go generate ./...`，再运行 `go test ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestSEC020CurrentPermissionIntersection -v`；预期退出1，16项401期望失败。不得在真实数据库运行。GREEN使用 `bash docs/security/tools/run-sec020.sh`，其archive/显式overlay及摘要记录实际被测内容。

提交前diff --check通过；新增证据常见私钥/GitHub Token/AWS访问Key/Authorization/session Cookie模式无匹配（有限扫描）。代码自审核对对象策略等价性、四种用户权限映射、既有超级管理员/角色限制和凭据类型门禁；修正了旧API夹具授权前提。PR最终head和CI仍需提交后核实，自审不称为独立审批。

环境检查补充：WSL对Windows工作树运行 `git diff --check` 因两端core.autocrlf不同，把既有CRLF文件报告为尾随空白，退出1；该命令短暂持有index.lock使首次commit失败。等待其正常退出后锁自动释放，未删除锁或修改文件。Windows原生 `git diff --cached --check` PASS；最终overlay全部SHA256一致，构建始终使用LF archive隔离副本。

[PR #9](https://github.com/Activer007/Gokapi/pull/9)已在本fork创建，代码提交a83366a80bc551c65fcde47e943a13df62f159fe。已读取远端patch并提交COMMENT自审，未作为独立审批；未发现本PR范围的阻塞项。后续提交仅更新PR/审查记录，生产和测试源码与BS2VtP摘要一致；最终CI成功后才合并，动态状态见PR checks。M2继续保持未完成。

SEC-020合并核实：PR #9最终head c29148819087698dcc1f2fbd7fdb00bd31ddd98b，CI [40641638222](https://github.com/Activer007/Gokapi/actions/runs/40641638222) / [40641632183](https://github.com/Activer007/Gokapi/actions/runs/40641632183)均成功，生成及四组测试全部通过。合并bf5918e69827b75b5321acc37efd66f4b7e1b498，本地master同步且tree与已审查head一致。CI提示现有Actions Node20弃用及ubuntu-latest迁移，留待SEC-062固定构建环境。

## M2 / SEC-021 撤销与使用记录合同

基线bf5918e69827b75b5321acc37efd66f4b7e1b498；2026-10-28；Windows PowerShell + WSL Ubuntu24.04，Go1.26.0 linux/amd64。`run-sec021.sh --red`固定该基线、仅覆盖新测试；正常模式叠加显式源码/测试清单到新/tmp archive。每次记录runner/overlay摘要、命令、日志及退出码。普通沙箱WSL仍E_ACCESSDENIED，授权后执行；只使用合成身份、临时SQLite/miniredis和临时blob。上游只读检索见[sec021-upstream.json](evidence/sec021-upstream.json)。

| 执行 / 证据 | 真实结果 |
| --- | --- |
| 首轮RED [TKYSMd](evidence/sec021-TKYSMd/results.tsv) | generate PASS；Redis使用记录合同FAIL；删Key API子项另有测试夹具PublicId长度错误导致404，不能当作产品RED |
| 修正夹具后RED [aANPSX](evidence/sec021-aANPSX/results.tsv) | generate PASS；真实API撤销8组PASS；SQLite使用记录合同PASS；Redis非时间字段/TTL、删除后更新、并发删除子项FAIL，符合目标RED |
| 首轮修复完整 [guHGIc](evidence/sec021-guHGIc/results.tsv) | 目标PASS；三组及相关完整race命令FAIL，原因均为旧Redis TestApiKeys要求更新时间重建已删除Key。无race检测报告；命令本身仍计FAIL，记录保留 |
| 最终完整 [e8JIMf](evidence/sec021-e8JIMf/results.tsv) | environment、generate、补强目标、awsmock、noaws、integration及fixture/SQLite/Redis全包race均PASS，脚本退出0 |

旧Redis测试已按新的provider合同改为删除后不重建，并保留对现存Key正常更新时间的正例。随后补强真实撤销后的元数据/下载/ZIP/presign签发/修改/替换/复制/恢复/删除与列表过滤；混合所有者ZIP两种输入顺序及presign整体拒绝。会话测试用synctest等待自身异步使用记录协程完成，并预热30秒使用记录缓存，使无关后台DB工作不会跨越fixture生命周期；不用于证明会话期限/浏览器行为。

真实命令：

```sh
go generate ./...
go test ./internal/test/securityfixture -count=1 -tags=test,noaws -run TestSEC021 -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/test/securityfixture ./internal/configuration/database/provider/redis ./internal/configuration/database/provider/sqlite -count=1 -tags=test,noaws
```

兼容：仅修改Redis的LastUsed/LastOnline写入，原子地检查存在后更新时间字段，不改变权限、所有者、TTL及其他字段；SQLite保持原实现。Lua以KEYS/ARGV传参，prefix不插入脚本文本。自定义ACL需允许EVAL/EVALSHA/EXISTS/HSET；此轮没有真实Redis/ACL环境，NOT_RUN。无schema、依赖、身份ID、密码、OIDC、API、页面/WASM或物理删除修改，无数据迁移；回滚本提交即可恢复旧行为，也会恢复旧的使用记录写入风险。无新重大架构方案，不新增ADR。

证明范围：provider延迟快照及同步屏障并发；真实管理API的顺序撤销与当前会话权限；混合ZIP成员。没有构造完整外部HTTP利用链，不把provider RED扩大为漏洞利用结论。全仓race、govulncheck、真实Redis/S3、自定义ACL、生产数据修复为NOT_RUN。presign消费、分片/File Request跨请求授权、异步任务、并发会话续期、并发Key签发/权限修改、OIDC与浏览器CSRF仍为M2开放Gate。

提交前源码overlay SHA256一致、文档相对链接、Windows原生diff --check通过；新增证据常见私钥/GitHub Token/AWS访问Key/Authorization/session Cookie模式无匹配（有限扫描）。[PR #10](https://github.com/Activer007/Gokapi/pull/10)代码提交6fefc889f9096b4dac224ea21ddb52193673ed8e；已读取远端patch并记录COMMENT自审，非独立审批。核对原子性、参数化key、TTL、正常更新正例、旧测试语义修正、会话夹具协程收尾及未覆盖边界；未发现本PR范围的阻塞项。后续提交只补PR与审查记录，代码/测试仍与e8JIMf摘要一致；最终head CI通过后才合并。

PR #10合并核实：最终head 08f7da39c220061fda77398be4d194f0b1548e55，CI [40644142401](https://github.com/Activer007/Gokapi/actions/runs/40644142401) / [40644137808](https://github.com/Activer007/Gokapi/actions/runs/40644137808)成功，生成及四组测试全部PASS。合并d599aabfbc7f52aa06a14a006348992adc94247a；本地master同步，tree与审查head一致，用户计划目录未修改。现有Actions运行时弃用/ubuntu-latest迁移提示继续归SEC-062。

## M2 / SEC-021 管理presign消费

2026-10-28，基线d599aabfbc7f52aa06a14a006348992adc94247a，WSL Ubuntu24.04 / Go1.26.0 linux/amd64。`run-sec021-presign.sh --red`固定该基线并只叠加新handler测试；GREEN模式明确叠加模型、API、handler、store与测试。均在新/tmp archive运行并保存HEAD/overlay/runner SHA256，临时SQLite/miniredis、合成身份和blob，不涉及真实数据。上游只读查询见[sec021-presign-upstream.json](evidence/sec021-presign-upstream.json)。

| 命令 / 证据 | 真实结果 |
| --- | --- |
| `bash docs/security/tools/run-sec021-presign.sh --red`，[foVI1z](evidence/sec021-presign-foVI1z/results.tsv) | generate PASS；目标FAIL。两provider各10项消费状态返回200而应拒绝；正常所有者/管理员/ZIP与过期/待删/匿名拒绝PASS。是M2当前授权合同RED，不把合法capability转交自动称为新漏洞 |
| `bash docs/security/tools/run-sec021-presign.sh --target`，[47Ms57](evidence/sec021-presign-47Ms57/results.tsv) | generate、目标GREEN；原子store领取和到期秒边界PASS |
| 最终完整执行，[Nfh4YZ](evidence/sec021-presign-Nfh4YZ/results.tsv) | generate、补强目标、awsmock、noaws、integration、定向race全部PASS，脚本退出0 |

真实命令：

```sh
go generate ./...
go test ./internal/webserver ./internal/storage/presign -count=1 -tags=test,noaws -run TestSEC021Presign -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver ./internal/storage/presign -count=1 -tags=test,noaws -run TestSEC021Presign
```

新webserver测试带`test && !integration`，由目标、awsmock、noaws和定向race运行；integration组是全仓兼容回归，不能宣称它执行了这些handler测试。测试从真实API签发进入消费handler，消费身份由可信认证context提供，另测匿名requireLogin拒绝，不冒称浏览器或完整HTTP登录链路测试。每个provider覆盖21组：正例、临时Key、撤销/过期/失去动作位/凭据类型变化、删除主体、双方委托变化、对象/ZIP成员所有者变化、过期/待删、缺失context/绑定、并发消费；未授权访问后原所有者仍可消费，成功后重放拒绝。单文件输出字节一致、ZIP两个成员；16个并发handler仅一个200，store的32个并发Consume仅一个成功，到期秒边界拒绝。

实现复用当前API凭据校验与SEC-020 ReadFile策略；所有文件检查后原子领取。保持成功API路径/参数/字段、页面/WASM、公开分享及文件驱动，新增行为仅收紧管理grant的当前授权；拒绝沿用400 invalid presign，匿名401。无schema/用户ID迁移；重启本来会清空内存grant，回滚代码与重启即可，恢复旧授权语义。ADR-006记录兼容、失败后重签、在途撤销和S3外部短期URL限制。

全仓race、真实S3/Redis服务、浏览器下载、S3已签发URL撤销、在途对象变更、govulncheck本项NOT_RUN。文件请求/分片/异步任务授权、并发会话续期/Key签发、公开分享矩阵、OIDC与跨浏览器CSRF仍是M2开放Gate。

提交前overlay源码摘要、文档相对链接、Windows原生diff --check通过；新增证据常见私钥/GitHub Token/AWS访问Key/Authorization/session Cookie/完整presign URL模式无匹配（有限扫描）。[PR #11](https://github.com/Activer007/Gokapi/pull/11)代码提交81d70117535c262a73868c25f419cabb20a14538；已读取远端patch并记录COMMENT自审，非独立审批。核对双签发入口、当前主体/Key、逐对象交集、凭据类型、原子领取、错误格式及ADR边界，无本PR范围阻塞项。后续仅补PR/审查文档，应用与测试源码仍与Nfh4YZ摘要一致；最终head CI成功后才合并。

PR #11 已核实合并 8377de414a8a07ab1019397470f9547a206c2715；最终 head 14e7e3eacc74bfdc5f011cb67efab2d5689827b6 两条 CI（40652514812 / 40652508331）生成及四组测试均 PASS，本地 master 文件树一致。

## M2 / SEC-032 最小发布集

2026-10-28，WSL Ubuntu24.04、Go1.26.0 linux/amd64，基线 8377de414a8a07ab1019397470f9547a206c2715。完整条件性 API 矩阵、RED 证据及评估保留在本人仓库的私密安全审查记录中；生产修复在该矩阵、三组全量和相关包 race 下通过。发布分支从公开基线新建，不携带私密提交历史或详细复现材料。

公开单元测试验证预约范围、有效期边界、删除失败/重试和 32 个并发取消。run-sec032-cancel.sh 以新 /tmp archive 加显式 overlay 验证发布文件集，记录 HEAD、源码/runner 摘要、真实命令及退出码；只使用临时文件和合成数据。命令如下，最终结果另列：

```sh
go generate ./...
go test ./internal/storage/chunking/chunkreservation -count=1 -tags=test,noaws -run TestSEC032 -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/test/securityfixture ./internal/storage/chunking/chunkreservation -count=1 -tags=test,noaws
```

临时安全 fork 按 GitHub 设计不运行 CI/状态检查，故远端合并前 CI 为 NOT_RUN，不能称为通过。官网流程要求在公告页统一合并，合并会将变更带入公开父仓库；因此只合并本最小发布集，公告保持草稿。详细审查材料继续私密保存，不能直接合并原完整证据分支。

无 schema/身份/加密迁移，删除失败响应变化及回滚见 ADR-007。全仓 race、真实 Redis/S3、浏览器、在途写入/完成与取消竞争、崩溃恢复、govulncheck 本项 NOT_RUN。M2 不因此完成。

最终发布集验证：[sec032-cancel-BnrTRT](evidence/sec032-cancel-BnrTRT/results.tsv) 的 environment/generate/scope/awsmock/noaws/integration/race 均退出 0。源码与私密审查通过的生产修复及单元测试一致。完整 API 矩阵仍保存在私密记录，本公开 runner 不声称重跑该矩阵。有限秘密模式扫描、源码摘要与原生 git diff --check 检查通过后提交；自审不是独立审批。
## M2 / SEC-023 最小发布集

2026-10-28，WSL Ubuntu24.04 / Go1.26.0 linux/amd64；基线 50965ba978aa10972423cea782fab2d72e68cfd8。
生产修复已在私密 HTTP/真实 Chrome 对照、TLS 代理、改密上下文及三组全量/相关 race 下通过。
本公开分支从公开基线新建，不含详细复现测试、截图或私密提交历史。

run-sec023-form.sh 在独立 /tmp archive 加显式 overlay 验证发布集并保存 HEAD/源码/runner SHA256、日志和退出码。
公开新增测试覆盖 binding、用途、到期秒边界、32 并发单次消费、HTTP/HTTPS Cookie 属性和多标签页；
既有登录/改密/OIDC 正例保留。它不声称重跑私密浏览器复现。真实命令如下，最终结果另列：

```sh
go generate ./...
go test ./internal/webserver ./internal/webserver/authentication/csrftoken ./internal/webserver/authentication/oauth -count=1 -tags=test,noaws -run 'TestSEC023|TestSEC012|TestSEC014CancelledLogin' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver/authentication/csrftoken ./internal/webserver/authentication/oauth -count=1 -tags=test,noaws
go test -race ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC023|TestSEC012PasswordChange|TestSEC014CancelledLogin'
```

临时安全仓库合并前 CI 为 NOT_RUN；公告页合并会使变更进入公开父仓库，只可合并最小发布 PR。
无需 schema/身份/密码算法迁移，旧表单刷新和回滚见 ADR-009。真实 Redis、多浏览器族、跨实例、旧会话跨站覆盖、
Token 总量预算、生产部署、全仓 race、govulncheck 为 NOT_RUN。M2 其余权限矩阵/并发 Gate 不因此完成。

最终最小发布集验证：[sec023-form-yZKclk](evidence/sec023-form-yZKclk/results.tsv) 的 environment/generate/context/awsmock/noaws/integration/race/handler-race 全部退出 0。
overlay 摘要与完整日志的有限秘密模式检查通过；生产源码与私密审查通过版本一致。自审不是独立审批。
公开证据目录只含命令、退出码、环境与源码/runner 摘要；原始日志保留本地忽略目录。
验证后仅将 runner 的输出目录改为 .secret 后缀，防止未来原始日志误暂存；已运行的原始 runner 摘要保留。

## M2 / SEC-020 公开凭据与下载矩阵增量

2026-10-28，基线 0d918a1bfa7df96f149de7f380026a504d164f81；WSL Ubuntu24.04、Go1.26.0 linux/amd64。仅新增回归测试和证据；不修改生产安全判断。run-sec020-capability.sh 将 HEAD archive 和两个新测试复制到独立 /tmp 工作区，所有数据库/blob 均为合成数据。

实际路由表逐项检查：匿名、分享 ID、hotlink ID、密码下载 Token、管理 presign、session ID、35 字符 Key PublicId、File Request ID，以及仅携带 session/文件密码 Cookie 的请求不能获得 API 权限；File Request Key 不能进入普通管理路由。永久/临时 Key 正例进入真实 /info/version；File Request 正例只检验四个公开上传路由的认证边界，不声称完成上传。公开下载、带文件名下载、hotlink 的状态矩阵在 SQLite/miniredis 验证；密码正例通过真实表单 POST 获取 Cookie。拒绝不改文件元数据/内容，也不消耗独立下载能力。

首次定向运行 qAlVz7 的 environment/generate/matrix 均 PASS。隔离 mutation trtJgO 的两个测试退出 1（预期 RED）：仅在临时源码中去掉文件绑定或待删检查，新负例均在“拒绝请求却返回文件字节”的断言失败；runner 检查退出码和具体断言，防止把编译失败当有效 RED。此为测试有效性验证，不是当前生产代码漏洞证据。

首次全量 EGRQ9s：environment/generate/matrix/race PASS；awsmock FAIL（API 包旧删除测试的后台 GC 遇到新测试关闭的全局 SQLite）；随后 noaws/integration 因 panic 留下的夹具数据出现重复用户名而 FAIL。原结果保留。修正新 API 矩阵为同一测试二进制的独立进程、临时 cwd、两分钟 Context 期限；不加 sleep，也不修改生产 GC。子进程继承 race 插桩；修正后在全新 archive 重跑。

实际命令：

```sh
go generate ./...
go test ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC020CapabilityEveryRoute|TestSEC020PublicDownloadMatrix' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC020CapabilityEveryRoute|TestSEC020PublicDownloadMatrix'
```

mutation 命令和原始退出码见相应 evidence 目录的 *.command.txt/results.tsv；原始日志在本地 .secret 忽略目录。NOT_RUN：真实 Redis/S3、全仓 race、浏览器、在途授权线性化、旧 hotlink 撤销竞争、生产部署、govulncheck。无依赖/schema/用户标识/密码算法变更，回滚只需撤销测试和文档提交，无数据恢复步骤。M2 其余 Gate 保留。

最终 [LVQVYR](evidence/sec020-capability-LVQVYR/results.tsv)：environment/generate/matrix/awsmock/noaws/integration/race 全部退出 0。40 个真实路由 × 凭据类别 × 两种 provider 共 872 个拒绝子用例；另 70 个公开下载状态子用例及合法认证正例通过。源码/runner SHA256 与最终工作区一致；有限秘密模式扫描与 git diff --check 通过。测试自审不是独立审批。

## 2026-10-28 M2 范围修订（纯文档）

按用户确认更新 M2-GOAL 及基线/威胁模型/看板/矩阵引用；未改生产 Go、schema、API 或测试代码。检查文档目标存在、差异与秘密边界；本轮本地 go generate、全量 Go、race、govulncheck 为 NOT_RUN（纯文档，无需重复已验证代码）。上一代码基线 79a0480 的合并后 CI 40675833652 已核实 completed/success。旧私密评估保留在独立本地分支与未发布审查记录，未混入本目标修订提交。

## SEC-021 单进程会话续期（2026-10-28）

基线 aff864a，WSL Ubuntu-24.04 / Go 1.26.0 linux/amd64；源码归档至 /tmp，env -i，临时 SQLite 和 miniredis，原始日志留在忽略目录。最终 [y3nYBb](evidence/sec021-renewal-y3nYBb/results.tsv) 所有项退出 0；公开源码 SHA256 见同目录，不上传详细 HTTP 复现。

实际命令：

```sh
go generate ./...
go test ./internal/configuration/database ./internal/webserver/authentication/sessionmanager -count=1 -tags=test,noaws,security_private -run 'TestSEC021Session' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/configuration/database/... ./internal/webserver/authentication/sessionmanager -count=1 -tags=test,noaws,security_private
```

以上 PASS。security_private 额外选择仅保留在私密审查中的 HTTP 测试；公开必要回归不依赖该文件。14 个双 provider 会话状态子用例、4 个双 provider 撤销顺序子用例以及私密 HTTP 的 1 个正例/3 个撤销负例通过。普通 Cookie/OAuth 生命周期沿用既有测试。

RED：XgK3yj 的原始实现合同失败，正例成立。隔离 mutation lF3Ig0 只移除用户/全局撤销协调，4 个预期断言完整失败（Go exit 1，验证脚本 exit 0）。失败记录：初次脚本 CRLF 无法启动；Id1pCn 辅助函数重名导致编译失败；apsDM1 测试时钟/互斥锁组合阻塞后终止；HD3XOr 故障注入首个断言成立但后续夹具残留阻塞，旧包装器 exit 0 不算有效通过；已增加清理和完整结束校验。首次全量 pMknYF 的 noaws 因上述 mutation 占用旧测试固定 Redis 端口而失败，其他项通过；最终串行全量全部通过。

有限秘密模式扫描、git diff --check PASS。NOT_RUN：真实 Redis/S3、完整浏览器/生产网络、多实例、存储故障注入、全仓 race、govulncheck（无依赖修改）。无 schema/身份/密码算法/文件删除变更；兼容、先删旧后写新及回滚要求见 ADR-010。审查属于自审，不冒称独立批准；最终 PR/合并/CI 以远端为准。

最小发布分支再次运行 `bash docs/security/tools/run-sec021-renewal.sh --target`：60mKPh 的 environment/generate/contract 均退出 0，公开合同使用 `-tags=test,noaws`。四个发布 Go 文件 SHA256 与最终全量/race 的 y3nYBb 完全一致；没有通过删除失败断言获得 GREEN。详细 HTTP 文件与私密评估文档不在发布提交中。

## SEC-032 File Request add/complete（2026-10-28）

基线 a65ecfe；Windows / WSL Ubuntu-24.04 / Go 1.26.0 linux/amd64，归档源码到 /tmp，env -i，合成文件及临时 SQLite/miniredis。原始 RED 5fUuoL：environment/generate 退出 0，scope 退出 1；六个完成边界断言失败，正例/拒绝对照通过。最小修复后 lDYez2 定向 PASS；加入异步拒绝覆盖后的最终 [tW7WdK](evidence/sec032-completion-tW7WdK/results.tsv) 全部退出 0。

实际命令及结果均 PASS：

```sh
go generate ./...
go test ./internal/webserver/api ./internal/storage/chunking/chunkreservation -count=1 -tags=test,noaws,security_private -run TestSEC032 -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/webserver/api ./internal/storage/chunking/chunkreservation -count=1 -tags=test,noaws,security_private -run TestSEC032
```

私密合同共 34 个实际 API 子用例（双 provider 的 add/同步完成正反例、异步拒绝负例）；公开预约测试覆盖 5 种作用域/寿命状态，入口回归覆盖同步/异步两分支。检查拒绝前后分片字节、元数据和预约不变，合法完成的所有者/请求归属及字节正确。详细 API 文件与原始日志不进入公开发布。没有完整异步成功上传、真实 Redis/S3、浏览器、生产部署、存储故障注入或全仓 race 证明；普通管理分片所有者绑定和完整在途竞争仍 VALIDATE。govulncheck NOT_RUN（无依赖变化）。

不修改 schema、用户 ID、密码、物理文件删除策略或上传字段；ADR-011 给出进程重启/在途上传兼容与回滚影响。有限秘密模式扫描、差异检查以及最终公开提取校验以提交/PR 自审记录为准；不冒称独立批准。

公开提取验证：`bash docs/security/tools/run-sec032-completion.sh --target` 使用 `-tags=test,noaws`，1vBVjG 的 environment/generate/scope 均退出 0。四个发布 Go 文件的 SHA256 与 tW7WdK 全量/race 输入一致。有限秘密模式扫描与 git diff --check PASS；详细请求矩阵及评估文档未进入发布分支。

## SEC-032 普通上传作用域修复

2026-10-28；生产基线 3d93cdb，Windows PowerShell / WSL Ubuntu-24.04，Go 1.26.0 linux/amd64。临时 git 归档、SQLite/miniredis、合成文件；全量和 race 串行执行。最终 DKXgX6 的 environment/generate/scope/awsmock/noaws/integration/race 均退出 0（PASS）。摘要和公开输入哈希见 evidence/sec032-managed-DKXgX6；原始日志及详细请求测试私密保留。

```sh
go generate ./...
go test ./internal/webserver/api ./internal/webserver/fileupload ./internal/webserver ./internal/storage ./internal/storage/chunking ./internal/storage/processingstatus/pstatusdb -count=1 -tags=test,noaws,security_private -run 'TestSEC032|TestNewFileFromChunk|TestChunkUpload|TestChunkComplete|TestProcessNewChunk|TestCompleteChunk' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock,security_private
go test ./... -parallel 8 -count=1 -tags=test,noaws,security_private
go test ./... -count=1 -tags=test,noaws,integration,security_private
go test -race ./internal/webserver/api ./internal/webserver/fileupload ./internal/webserver ./internal/storage ./internal/storage/chunking ./internal/storage/processingstatus/pstatusdb -count=1 -tags=test,noaws,security_private -run TestSEC032
```

私密真实 API 合同覆盖 56 个普通上传与 34 个既有 File Request 叶子用例；普通上传包括长期/临时/同用户换 Key、同步/异步正反例、缺失/旧分片和错误清理。公开回归覆盖存储域、浏览器接收处理器的认证上下文、进度用户隔离和异步前置检查；不把处理器测试称为实际浏览器测试。公开 runner 不带 security_private，能重跑公开子集和标准全量；私密合同不混入发布。

保留失败：原始普通上传 RED 为 TdApbl（8 负例失败、12 对照通过）；进度隔离 RED 为 sec032-status-red.secret（第二用户状态缺失）。第一次进度测试启动遇 PowerShell/WSL 引号导致 tar 空路径，测试 NOT_RUN，改用保存脚本后取得真实 RED。初次修复回归 636zXV 的文件名净化测试失败，因为旧测试夹具直接写全局 chunk；修正夹具所属用户后通过。ebhqPz 中间全量/race 全过，最终改用与旧命名不相交的格式后再次取得 DKXgX6 全过。临时副本 mutation G37Y8g 恢复旧清理调用，6 个错误清理防御用例按预期失败；真实源码未回退。

未修改依赖，govulncheck NOT_RUN；真实 Redis/S3、实际浏览器、全仓 race、多实例、全部同用户并发交错及异步存储失败注入 NOT_RUN。缺少已授权分片的异步入口已测试拒绝，合法异步完成已测试；不声称覆盖全部异步失败流程。ADR-012 记录兼容与回滚。

公开提取验证：`bash docs/security/tools/run-sec032-managed.sh --target` 的 smTgmQ environment/generate/scope 均 PASS(0)，不带 security_private。14 个公开 Go 文件与最终全量/race 的输入 SHA256 一致。git diff --check、有限秘密模式扫描均 PASS。完整评估和两个详细请求测试不在发布树中。

## M2 收尾审计验证

代码基线 3a9627c，与 64de3b7 的 `git diff --exit-code` 通过；九个必需前序合并为祖先、五个私密评估不是公开祖先。实际重读九份最终结果表均为全 0；公开源与最终完整验证保持一致。合并后 [CI 40697431982](https://github.com/Activer007/Gokapi/actions/runs/40697431982) success，生成和四组 Go 测试通过。重新核对私密浏览器观测、OIDC 现状/RED 与 ADR、会话 HTTP 及上传合同的证据范围；不重述为本轮重新执行浏览器或真实服务。

本轮仅文档修改：运行相对链接/引用文件校验、证据/祖先核对、`git diff --check` 和有限秘密模式扫描。Go 生成/本地全量/race/govulncheck 本轮 NOT_RUN，按 M2-GOAL 复用已核实代码 Gate，文档 PR 的 CI 按配置执行。审计详见 [M2-COMPLETION-AUDIT](M2-COMPLETION-AUDIT.md)；此结论不包含任何生产迁移/部署或全部后续安全阶段。

## M3 / SEC-031 现状语义与产品合同

2026-10-28；基线 e38b03f；Windows PowerShell / WSL Ubuntu-24.04、Go1.26.0 linux/amd64。沙箱内首次WSL返回E_ACCESSDENIED，授权执行后环境核实通过。没有本机Go；测试以git archive临时副本叠加明确的新测试文件执行，不加载部署数据。用户原计划目录保持未跟踪。

命令：`bash docs/security/tools/run-sec031-semantics.sh`，依次运行：

```sh
go version
go generate ./...
go test ./internal/webserver/api -count=1 -tags=test,noaws -run '^TestSEC031UploadSizeSemantics$' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
```

目标测试在独立子进程对SQLite/miniredis执行两份768KiB上传，完成结果ID绑定各自预期内容且blob不同；总计1572864字节证明MaxSize=1MiB实际为每文件。按准确错误码验证单文件/协议开销/未知长度、磁盘最低余量、过期请求和顺序文件数拒绝。不是10/50并发、真实Redis或大文件吞吐证明。

失败历史：cosvj9生成PASS、目标FAIL，为测试DataDir与filesystem驱动路径未同步；修正夹具，未移除生产校验。4xtxNX目标PASS，新增控制后3sbGUY全部6项PASS。审查补强ID/内容绑定和拒绝原因后i3PXUM生成/目标/integration PASS、awsmock/noaws FAIL，唯一失败为Webserver固定端口53843 `address already in use`；端口归属未在失败时捕获，不归因于生产漏洞。检查Windows/WSL监听已释放后串行最终重跑，结果以下方最终证据为准。失败原始记录仍在各`.secret`目录。

用户确认保留MaxSize单文件语义、另增累计限制。仅纠正文档、增加现状测试与ADR草案，无运行时/schema/依赖变更。相关race/govulncheck本PR NOT_RUN；没有并发生产逻辑或依赖变化。M3原子预约、预算入口、恢复/峰值 Gate保持开放。

最终验证 `tn2FtP` 的environment/generate/semantics/awsmock/noaws/integration全部PASS(0)，[公开结果](evidence/sec031-semantics-tn2FtP/results.tsv)与[输入哈希](evidence/sec031-semantics-tn2FtP/source.sha256)已核实对应最终测试文件。相对链接、有限秘密模式扫描及git diff --check PASS。审查修复两项测试有效性问题：各完成ID对应自己的精确payload；负例匹配错误码及磁盘不足原因。没有把首次夹具失败当作漏洞RED，也没有把顺序数量测试当作原子预算证明。

## M3 / SEC-030 纯逻辑配额计算

基线bfd4998（SEC-031），环境Go1.26.0 linux/amd64 / WSL Ubuntu-24.04。新增internal/uploadquota，无DB/配置/文件I/O/生产调用。先编写合同测试和未实现stub，隔离命令 `go test ./internal/uploadquota -count=1 -tags=test,noaws -v -cover` 退出1，TestMeasureLogicalUsage等失败；这是新API开发RED，不是现存生产漏洞复现。最小实现后同命令退出0、语句覆盖100%；红/绿原始命令、输入哈希与日志在`sec030-calculation-agent.secret/{red,green}`保留。

独立源码审查未见计算阻塞，范围仅纯逻辑：每条metadata独立计量、当前所有者/原request分组、加密显示无关、过期/待删保留、所有者转移/metadata删除、无输入变更；字节/文件数的精确边界、负值、两阶段溢出、零容量和1–2GiB算术。不用GB级数据分配伪装流式性能测试，未运行原子并发或恢复。

最终运行器 `bash docs/security/tools/run-sec030-calculation.sh` 将git archive叠加两个明确Go文件，执行：

```sh
go version
go generate ./...
go test ./internal/uploadquota -count=1 -tags=test,noaws -v -cover
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
```

相关race/govulncheck本PR NOT_RUN：没有共享并发状态或依赖修改。无schema迁移/上传接口改变；函数未接入，不能声称用户配额已强制执行或不会超卖。后续必须显式映射旧0=取消某层限制、处理Measure异常后拒绝准入；具体预算仍要在provider事务中与写入原子执行。

最终62zgZb的environment/generate/calculation/awsmock/noaws/integration全部PASS(0)，目标覆盖100%语句；[公开结果](evidence/sec030-calculation-62zgZb/results.tsv)、[两个Go输入哈希](evidence/sec030-calculation-62zgZb/source.sha256)与当前文件逐项相符。相对链接/有限秘密模式/git diff --check PASS。前置SEC-031 [PR #15](https://github.com/Activer007/Gokapi/pull/15)已合并8019c61，CI40707671124 success，合并树与测试基线bfd4998一致。

## M3 / SEC-030 持久预约基础（2026-10-28—29）

基线357670c，分支codex/sec-030-durable-reservations；Windows PowerShell / WSL Ubuntu-24.04、Go1.26.0 linux/amd64。实际库/文件均为临时合成数据，未部署、未操作生产数据。数据库schema SQLite15→16、Redis8→9；设计/升级/回滚/边界见[ADR-014](ADR-014-DURABLE-UPLOAD-RESERVATIONS.md)。原用户计划目录未跟踪且未修改。

严格先合同后实现：jJzTw2为纯状态桩RED，xPHhRM为SQLite/Redis provider桩RED，contract均退出1，失败为目标断言而非编译/启动。最小实现首轮yHxA0K仅两个provider的冲突错误分类失败；调整声明重放的检查顺序，保留声明校验。3C1RG0、0fhRPH目标生成/合同PASS。审查增加未绑定预约Bind后重放、不可达完成时间、读取上限、WATCH目标类型竞态、EXEC清理和严格迁移目标；新迁移预检s04pla中两个目标的孤立session拒绝合同RED，随后补齐全应用数据预检。均为本次开发合同，不称为现存可利用漏洞。

最终命令：`bash docs/security/tools/run-sec030-reservations.sh`，在git archive副本叠加限定目录Go源码，规范CRLF为LF并记录实际编译的52份输入哈希。执行如下：

```sh
go version
go generate ./...
go test ./internal/uploadquota ./internal/test/securityfixture ./internal/configuration/database ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws -run 'TestSEC030(Ledger|Durable|SQLite|Redis|Schema|Migration)' -v
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
go test -race ./internal/uploadquota ./internal/test/securityfixture ./internal/configuration/database ./internal/configuration/database/provider/sqlite ./internal/configuration/database/provider/redis -count=1 -tags=test,noaws -run 'TestSEC030(Ledger|Durable|SQLite|Redis|Schema|Migration)'
```

最终 b3lLSE 全部7项PASS(0)，[公开结果](evidence/sec030-reservations-b3lLSE/results.tsv)、[52份Go输入哈希](evidence/sec030-reservations-b3lLSE/source.sha256)已与当前规范源码逐项核实一致。基线源码及未覆盖路径来自指定HEAD，不拷贝部署配置。原始每项命令、日志、RED与中间失败在相应`.secret`目录保留。

覆盖范围：双provider独立连接10/50争最后用户/request/实例字节、文件、在途、暂存额度；metadata现算、共享hash逻辑计量、大小绑定、唯一Claim/Commit与冲突；过期仍占额、可信清理确认、重连保持；4096/1024/256记录上限、分页、终态回收、JSON字段/版本/长度与精确int64；SQLite同连接回滚及单连接池复用；Redis真实WATCH事务被另一provider原始写入使失效后重算、错误类型竞态、非致命EXEC失败清理和逐项错误检查。Redis测试为miniredis，不冒称真实持久化认证。

迁移/兼容：新库与旧schema升级保留数据和预约；SQLite VACUUM INTO合成一致备份恢复保留旧15模式及原16数据；两向SQL/Redis迁库保留干净回执、拒绝活动/待清理源、部分升级、未来版本、非空目标（含孤立认证/加密/统计数据）；旧格式源不被暗中升级。CLI新拒绝退出码3、原正例在全量通过。旧Migrate夹具补合法UserId，另设非法旧记录拒绝；Redis DialContext失败返回error，旧内部panic测试改为error断言，启动层仍拒绝失败连接。没有为通过测试移除服务端校验。

NOT_RUN：真实Redis服务/AOF/RDB、旧二进制、生产迁移/恢复、全仓race、govulncheck（无依赖修改）、实际大文件/吞吐/峰值。HTTP/CLI上传尚未调用此预约原语；CleanupConfirmed不是删除成功证明，入口预算、正向metadata变更、I/O互斥、启动对账/物理清理和1–2GB验收继续OPEN，M3未完成。

补记前轮私密现状观察：在cc5c1cd的未改动真实API上，SQLite/miniredis各10/50竞争MaxFiles=1，目标及定向race均退出0，每组恰好1个成功。生产认证限流保持，未用内部hook冻结快照；结论NOT_REPRODUCED，不证明所有调度原子。最初wrapper期待RED而返回1不是产品失败，第一次race启动器未启动Go为NOT_RUN，修正后实际race0；原始证据留在sec030-reservation-red.secret。本轮新的数据库合同RED与该现状观察分开，未将源码疑点升级为漏洞。

## SEC-062 setup测试监听隔离（2026-10-29）

基线4ad12558ed9096fc74ebf1afdda273d5a56fc76b，Windows PowerShell / WSL Ubuntu-24.04、Go1.26.0 linux/amd64。PR #17已合并，PR头CI40725885197通过，但合并后CI40726798576两次失败：attempt1的noaws、attempt2的integration均在setup首次监听127.0.0.1:53842时返回address already in use；不是quota断言失败。原始失败日志保留sec030-reservations-postmerge.secret，不把重跑前的失败覆盖成通过。CI占用socket未确定；Linux临时端口假设仍VALIDATE。只读检查upstream测试、README/advanced、Issues和开放PR；[#422](https://github.com/Activer007/Gokapi/pull/422)仅修正19888 wildcard地址，不是动态监听隔离，未发现同范围待合并修复，未向upstream写入。

先RED后修复：kr9wsw在隔离归档上持续占住53842，旧TestInitialSetup实际bind失败，environment/compile退出0、occupied-port退出1。L7Jtst初版动态listener的占用合同、生成、setup全包与相关race均0，TkgSor的CI模式也0。Lx501J第一次全量中blocker自己无法绑定53842，occupied-port为2（harness BLOCKED，不是产品RED），其余environment/compile/generate/setup/race/awsmock/noaws/integration均0。为使复现器自身不依赖固定空闲端口，最终blocker改为持续持有OS分配端口，并将该实际端口作为子进程GOKAPI_PORT；lDddre在历史4ad1255重新确认environment/compile为0、occupied-port为1。最终脚本--red明确固定历史基线，CI只运行应通过的--occupied。

实现保持原配置保存的53842/ServerUrl等断言；实际测试HTTP使用持续持有的OS分配listener，启动/结束同步并关闭探测响应体。审查发现初版ready超时后可能留下迟到listener；最终改为每cycle显式传递factory、launch前注册清理，TestSEC062LateListenerCleanup用通道先取消再放行分配，证明listener关闭且Serve退出，无长时间睡眠。生产默认绑定、公开Run API、认证、一次性证明、slow-header与重放判断不变，延迟Shutdown捕获本轮server；bootstrap两个直接调用只补server参数，405/409断言保留。不修改UI/WASM、依赖或数据库schema，也没有新增上传额度强制执行。

最终命令为`bash docs/security/tools/run-sec062-setup-listener.sh`，在git archive基线上仅叠加Setup.go、Setup_test.go、bootstrap_test.go三份规范LF源码，记录实际编译哈希。依次运行go version、go test -c -tags=test,noaws、Python持有配置端口的真实setup合同、go generate ./...、go test ./internal/configuration/setup -count=1 -tags=test,noaws，以及：

```sh
go test -race ./internal/configuration/setup -count=1 -tags=test,noaws -run '^(TestInitialSetup|TestRunConfigModification|TestIntegration|TestIsErrorAddressAlreadyInUse|TestSEC062.*)$'
go test ./... -parallel 8 -count=1 -tags=test,awsmock
go test ./... -parallel 8 -count=1 -tags=test,noaws
go test ./... -count=1 -tags=test,noaws,integration
```

CI在原generate之后串行增加同一脚本--occupied，只执行环境/编译/占配置端口合同，保留原四组Go测试；不执行--red。NOT_RUN：Windows原生Go、全仓race、govulncheck（依赖未改）、本地awstest、真实Redis重启/生产恢复、实际1–2GiB吞吐和暂存/RSS峰值。SEC-030运行时额度、物理清理/启动恢复及M3性能Gate继续OPEN。本项只修复可复现测试入口，不将测试启动问题声明为安全漏洞，也不把未确定的CI占用者写成已证实根因。

最终393FfY九项全部PASS(0)：environment/compile/occupied-port/generate/setup/race/awsmock/noaws/integration；[公开结果](evidence/sec062-setup-listener-393FfY/results.tsv)、[三份编译源码哈希](evidence/sec062-setup-listener-393FfY/source.sha256)及[运行器哈希](evidence/sec062-setup-listener-393FfY/runner.sha256)。Go输入与当前规范字节/Git index、runner与实际运行哈希均逐项一致。静态复审未见剩余阻塞；这不是独立人工批准。原始日志/中间失败保留相应.secret目录；用户未跟踪计划和deployment目录未暂存。

提交691238bf6d6f9faf6c7b5f76d166da6288a410b6；自己的[PR #18](https://github.com/Activer007/Gokapi/pull/18)承载本增量。后续只补PR链接的文档提交不改变上述Go/runner哈希；远端checks、合并和主分支CI必须另按实际状态核实，不能从本地PASS推断。

## SEC-030 真实上传接收准入（2026-10-29）

起点f4ce5db，Windows PowerShell / WSL Ubuntu-24.04、Go1.26.0 linux/amd64；仅归档副本/合成临时数据，用户plan及deployment未跟踪目录未纳入。设计、接收目录升级/回滚及剩余边界见[ADR-015](ADR-015-BOUNDED-UPLOAD-RECEPTION.md)。四类入口共用receiver，保持认证先于正文；未接入持久chunk/metadata总额度，不宣称M3已完成。

先失败合同：`bash docs/security/tools/run-sec030-receiver.sh red-policy`的6NrjZ8，环境0、contract1（严格配置桩允许非法值/回显值的断言失败）；`... red-entry`的hHKIGZ，环境0、contract1，旧入口10/50全部读取正文而非限4，未知长度正常末块被拒，已知长度的文件先/后两个对照PASS；`... red-storage`的wVRynQ，环境0、contract1，9MiB加密工作文件保留已经unlink的明文描述符，修为删除前显式Close。均是隔离资源合同，不自动升级为可利用漏洞声明。

历史 `red-entry` 重放现固定使用已合并 `f563d9f5bcb4fea30cfb4c5fc1ccb0b22a284313` 的原测试文件（不叠加当前managed-session测试），输出ref与文件SHA256（`e6bd68ef9dc58ffd33379e8c259ea8cc96b6263f8a98b2e056d1d4e6daf286f0`）；仅真实10/50正文准入断言失败且known-orders对照PASS记 `ASSERTION_RED`/退出1，编译、setup、panic或未重现记退出2；本次修改后Go重放仍NOT_RUN。

原有夹具修正：声明13字节的chunk只写11字节，改为实际13；保留服务器实际payload/剩余范围判断。旧SEC031的正文开销/未知长度拒绝改为正例，仍检查payload超单文件上限拒绝、无chunk/blob及ownedroot仅有marker。每provider/每根接收合同独立子进程，不能为测试切目录而放松单Pool/DataDir约束。首次联测QrWd7P生成0，目标/race1，失败为fixture多次换DataDir及本地pipe未关闭，随后修正；没有取消服务器端校验。

`... target`的gCpjEk：environment/generate/contract/race全部0，覆盖环境、整文件工作描述符、混合入口10/50、未知长度/字段顺序、真实HTTP取消/断线和SEC031双provider路由。该次统一filter未匹配核心包TestReceive/TestSpool，输出明确为no-tests；核心包另有完整target/race PASS（core-agent/verified），不能把空包绿色当核心race覆盖。运行器及CI现补全filter。之后新增Close结果不确定的sticky保留及EIO注入合同，尚未由该旧本地PASS覆盖。

实际大文件命令：`bash docs/security/tools/run-sec030-receiver.sh large`，irDnxt的environment/generate为0、large为1。真实HTTP chunked/file-first的1GiB明文上传及读回SHA256通过：7.448s、137.49MiB/s、堆峰值3071232B、RSS峰值24100864B、表观暂存峰值2146041891B、已分配峰值2146054144B。2GiB加密阶段因WSL根文件系统返回read-only而失败并触发旧storage错误路径，不能记PASS；后续WSL启动返回E_UNEXPECTED。Windows检查D盘剩余0、E盘约100GB；未确定根挂载失败的全部因果，不自动重启/修复宿主或删除他人数据。

上述本地命令的原始输出和逐文件编译hash保留相应.secret目录。环境agent先前CR包装失败/路径转换NOT_RUN均未当通过，最终该agent环境全包独立运行退出0。当前本地全仓awsmock/noaws/integration、纠正后的完整filter、Close不确定及新增兼容合同的最终本地验证为BLOCKED/NOT_RUN（WSL服务故障），后续由GitHub临时runner执行，以真实远端记录补充；不能抹去本地失败。Windows原生Go、govulncheck（依赖未改）、真实Redis/AOF/RDB、真实S3、生产迁移/恢复、完整M3多路径1–2GiB矩阵仍NOT_RUN。

远端workflow保留生成及四组全量，新增完整接收合同race和受测源码hash；手动workflow_dispatch额外运行1GiB明文/2GiB加密HTTP、读回hash、吞吐/heap/RSS/暂存采样。大文件使用单独security_large tag，不加入默认Go suite。所有远端Gate尚需观察实际完成，不能从本地1GiB结果推导2GiB、S3或完整上传总量已通过。

代码d7ea1f4提交到自己的[草稿PR #19](https://github.com/Activer007/Gokapi/pull/19)。首次远端push40743890289的生成/占端口合同PASS，awsmock FAIL：旧TestPostUpload实际3字节却声明50，上传正确拒绝后原SSE scanner无截止等待至10分钟；另有原API非multipart错误文案兼容断言失败。修正网页夹具从真实stat长度填写两处声明并断言metadata字节，加30秒SSE context；补11/13字节错误末块拒绝回归，保留实际长度防线。非multipart返回原http.ErrNotMultipart以保留公开错误文案。原始失败保留，后续新提交另跑Gate，未通过缩弱服务端校验使旧错误数据成功。

f563d9f检查点：PR CI40746205711与手动CI40746246616均SUCCESS，生成/四组全仓/完整receiver race全部PASS；手动run另有真实1GiB明文、2GiB服务器加密HTTP上传与读回SHA256。1GiB为5.075s/201.78MiB/s，heap2965576B、RSS24260608B、暂存allocated2147487744B；2GiB为15.470s/132.39MiB/s，heap3061344B、RSS24793088B、暂存allocated6434066432B。runner事前84GiB可用。[公开证据](evidence/sec030-receiver-f563d9f/README.md)含实际runner源码hash并逐一与该提交blob核对；指标仅代表此硬件/路径，不冒充S3、浏览器WASM或所有M3矩阵已完成。

最后审查补HTTP拒绝边界：只有进程内slot不读不足以说明net/http不会在拒绝后自动drain。接收失败对HTTP/1设Connectionclose，并尽力设置当前read deadline，保留429/Retry-After；新增真实socket只发Content-Length:1头而不发body的忙碌拒绝回归。该后续改动及最新Gate另以当前PR/手动run核实，不用上个检查点绿色替代新增检查。

最终兼容复核还保留旧Go multipart的quoted-printable流式解码与其他CTE原样语义；预算/envelope按wire计，落盘/文件上限按decoded计，新增真实QP writer编码膨胀及边界矩阵。参照Go1.26源码复核，不把换接收器变成无关公开协议改写；该补丁和拒绝后排空测试的本地执行仍NOT_RUN，必须由新提交远端Gate覆盖。

### SEC-030 最终远端 Gate：ff7497b

[PR CI40751200410](https://github.com/Activer007/Gokapi/actions/runs/40751200410)与[手动CI40751191903](https://github.com/Activer007/Gokapi/actions/runs/40751191903)，attempt1均SUCCESS，head为ff7497beb4f173f81cbf90695e7b6544d393f53b；手动job123038014344全部步骤success。环境GitHub ubuntu-latest / Go1.26.0 linux/amd64。实际执行如下（awstest使用workflow内合成凭据及mock，并非真实S3）：

```sh
go generate ./...
bash docs/security/tools/run-sec062-setup-listener.sh --occupied
go test ./... -parallel 8 -count=1 --tags=test,awsmock
go test ./... -parallel 8 -count=1 --tags=test,noaws
go test ./... --tags=test,noaws,integration -count=1
go test ./... --tags=test,awstest -count=1
go test -race ./internal/environment ./internal/storage ./internal/storage/uploadreceive ./internal/webserver/fileupload ./internal/webserver/api ./internal/webserver -count=1 -tags=test,noaws -run 'TestSEC030Receive|TestSEC031UploadSizeSemantics|TestReceive|TestSpool'
bash docs/security/tools/run-sec030-receiver.sh large
# large模式实际编译命令：
go test ./internal/webserver/api -count=1 -tags=test,noaws,security_large -run TestSEC030ReceiveLargeFiles -timeout 15m -v
```

以上全为PASS；最后命令由large运行器在归档副本执行，非重复宣称另一独立执行。完整接收race这次包括核心TestReceive/TestSpool及webserver；最终新增sticky Close、HTTP拒绝、日志脱敏和quoted-printable合同均获本次覆盖。43份实际Go源码hash逐项与ff7497b Git blob核实一致，[最终证据/指标](evidence/sec030-receiver-ff7497b/README.md)已保存；此前f563d9f仅为历史检查点。

| 实际HTTP上传+读回 | 用时 / 吞吐 | 堆峰值 / RSS峰值 | 暂存表观 / 已分配峰值 |
| --- | --- | --- | --- |
| 1GiB明文 | 3.761s / 272.25MiB/s | 3036296B / 23859200B | 2147483683B / 2147487744B |
| 2GiB服务器加密 | 21.947s / 93.32MiB/s | 3051528B / 25358336B | 6435733539B / 6435753984B |

两个SHA256逐字节读回匹配。指标只代表该runner/路径，20ms磁盘采样不是所有瞬时峰值证明，不涵盖真实S3、浏览器WASM、全部chunk路径或生产负载。最后证据提交仅改docs，不改Go/运行器/workflow，随后PR CI独立检查；不能将ff7497b的大文件记录说成后续文档提交重新执行。

本机2GiB失败及WSL E_UNEXPECTED仍为FAIL/BLOCKED，最终本地全量和补丁验证仍NOT_RUN；未重启/修复宿主。Windows原生Go、全仓race、govulncheck、真实Redis/S3、生产恢复仍NOT_RUN。SEC-062已公开Go公告的修补/可达性另设发布前P0 Gate。M3持久预算接线、正向metadata、完整区间、异步完成/恢复仍OPEN；外层认证早期拒绝传输行为仅VALIDATE，后续先写隔离socket合同。没有从接收层绿色推断整体防耗尽或已可安全发布。

## SEC-062 已公开标准库修补与实际工具链 Gate（2026-10-29）

基线cb5868c6d45d1a3f4fc163c74976c90c756ae8a8，E:/LHA/Gokapi，Windows PowerShell；无原生Go，WSL先前read-only/E_UNEXPECTED持续，本轮未再次启动或修复。保留原plan/deployment两个未跟踪目录。只读上游master仍a9c25d4，检索开放golang/toolchain/govulncheck/HTTP2 PR及Issues未见同范围待合并修复；官方Go发布及公告版本见SECURITY-BASELINE。该PR处理公开已修标准库，未复现高CPU/内存恶意负载，不新报漏洞。

安全不变量先行：根/build模块、三个Go builder及实际编译器必须与审查过的1.26.10一致，真实产物需BuildInfo和扫描。实际本地执行 `python docs/security/tools/check-sec062-toolchain.py`，旧声明五项FAIL、退出1（policy-red.log），修改声明后同命令退出0（policy-green.log），明确actual compiler NOT_CHECKED。原始输出在sec062-toolchain-local.secret；这是配置策略合同RED，不冒充动态漏洞复现。无需新运行时/schema/依赖图重构，两个go.sum保持原字节。

新增 `bash docs/security/tools/run-sec062-toolchain.sh scan` 在git archive隔离副本运行GOTOOLCHAIN=local、GOFLAGS=-mod=readonly，固定安装 `golang.org/x/vuln/cmd/govulncheck@v1.9.0` 到临时GOBIN，记录scanner版本/BuildInfo、输入hash、每条真实命令及退出码。普通文本输出保留漏洞非零状态；不用JSON格式的成功退出码伪装无漏洞。实际扫描default/noaws源码、单文件minifier生成器、CGO_ENABLED=0服务端/CLI产物；go generate重建WASM和匹配shim，产物SHA256留证。CI保留原生成/四组全仓/相关race，补实际compiler policy。

手动workflow另运行historical模式：固定归档旧cb5868c，显式Go1.26.0生成与扫描，保存真实scanner非零结果，要求命中公开GO-2026-6609才承认历史RED。wrapper预期RED成功不等于旧源码无漏洞。containers模式只构建runner本地三个镜像，不push、不挂数据卷；复制临时容器中的server/CLI产物读取BuildInfo，builder读取go version。大文件Gate按原脚本1GiB明文/2GiB服务器加密实际HTTP重新执行。

当前以上远端执行均NOT_RUN；本地Go生成/全仓/race/govulncheck/Docker仍NOT_RUN/BLOCKED。后续逐run补真实PASS/FAIL，不把本地Python结果当远端通过。无schema/密码哈希/加密格式变化；构建最低版本升至1.26.10，旧离线compiler将拒绝构建，auto环境可获取新版本。回滚二进制不改变既有数据格式，但旧compiler重新构建会恢复已知标准库风险。WASM浏览器交互、全仓race、真实Redis/S3、完整镜像digest固定/供应链扫描/生产恢复仍为独立Gate。

审查补强：历史RED仅接受govulncheck漏洞退出码3且含目标ID，超时/执行失败不能转GREEN；实际归档inputs.sha256与当前harness.sha256分别记录。新增64B真实静态及带文件密码能力的本地下载Range兼容测试（200/HEAD/单Range/双Range/416及精确内容），没有高资源负载；纳入全仓和定向race。WASM另外按GOOS=js/GOARCH=wasm做源码扫描，浏览器交互仍NOT_RUN。当前新Go测试/gofmt/所有扫描均尚NOT_RUN，待真实远端结果。

本地脚本语法：首次Git Bash被沙箱MSYS对象权限阻止，未解析脚本（BLOCKED）；随后仅提升bash -n读取权限，语法退出0，未启动WSL/Go。审查发现run辅助函数的全局name覆盖产物循环变量；隔离helper合同实际退出1，local name/code修复后同合同退出0（helper-red/green.log），不把运行器缺陷称产品漏洞。3686e28的手动CI40759379612因该已知缺陷取消，状态按GitHub实际保留；后续新提交独立验证，旧PR/推送CI不得冒充修复后通过。

旧3686e28推送CI40759247121实际FAIL于toolchain gates：默认/noaws/WASM源码扫描均退出0，但生成器命名Go文件被govulncheck source mode拒绝（退出2），随后还有已修的循环变量导致错误产物路径。生成器目录有多个独立main，改为Go原生编译其实际minify可执行文件并binary扫描，不拼接虚构包。560862c的手动40760133832因同一已知source调用错误取消；这些失败/取消不计PASS。源码扫描同时报告未导入/未调用的openpgp及旧AWS S3 Crypto模块公告，不能简写为“依赖没有公告”，最终报告需保留模块级限制。

第二个旧推送CI40760132130实际失败于go generate下载minify模块：proxy.golang.org HTTP/2 INTERNAL_ERROR，后续步骤未运行；未误归为生成器扫描错误，原日志second-failed.log保留。本轮进一步只读审查发现历史RED需要旧commit对象，checkout改为完整历史；格式检查改bash -ec，合成gofmt退出2控制证明旧bash -c误回0、新调用保留2，合同退出0。这是错误传播控制，不冒充运行了真实Go格式检查；真实格式化已由首轮远端退出0，最终仍随新CI验证。

82d54dc手动CI40762083078：生成/四组全仓/相关race、实际Go1.26.10及全部源/产物扫描PASS，历史Go1.26.0扫描实际退出3且含6609、wrapper预期RED通过。其后Docker FAIL：mktemp随机目录名含大写，临时image repository名被Docker拒绝，构建尚未发生，大文件SKIPPED；原始final-attempt1.log保留。仅将临时image名转小写，并在任何build失败后停止后续容器操作，不改变应用、构建器版本或扫描判定；后续另跑真实构建。历史对象缺失另以本地no-checkout浅克隆归档实际RED128、unshallow后GREEN0验证，不操作原工作树数据。

### SEC-062 最终代码 Gate：58d2cb5

[PR CI40763628844](https://github.com/Activer007/Gokapi/actions/runs/40763628844)、[推送CI40763619586](https://github.com/Activer007/Gokapi/actions/runs/40763619586)及[手动CI40763621734](https://github.com/Activer007/Gokapi/actions/runs/40763621734)全部SUCCESS；手动job123081807272，head58d2cb54acb64ce8f5b9f3bda41d75cb38e620d5。GitHub ubuntu-latest/Linux amd64、Go1.26.10、GOTOOLCHAIN=local；govulncheck1.9.0，DB updated 2026-10-27 21:16:18 UTC。原有go generate、占配置端口合同、四组全仓awsmock/noaws/integration/awstest、相关race（含TestSEC062Range）全部PASS，实际命令保留workflow及脚本。新增完整真实命令为：

```sh
python3 docs/security/tools/check-sec062-toolchain.py --actual "$(go env GOVERSION)"
bash docs/security/tools/run-sec062-toolchain.sh scan
bash docs/security/tools/run-sec062-toolchain.sh historical
bash docs/security/tools/run-sec062-toolchain.sh containers
bash docs/security/tools/run-sec030-receiver.sh large
```

以上全部PASS。scan内default/noaws/WASM源码与minifier/server/CLI binary共6项扫描均0；生成器使用实际编译的独立可执行文件，不使用scanner不支持的命名Go文件source参数。historical先核实实际Go1.26.0、生成旧cb5868c，再扫描得真实退出3且含6609调用链，wrapper按预期RED通过；没有高资源攻击请求。输出中的GitHub漏洞annotations属于该历史负向合同，不能与当前Go1.26.10扫描混淆。源码和产物BuildInfo/最终返回码均已核实，不只看workflow绿色。

三个Docker构建均0，builder go version及server/CLI BuildInfo都为1.26.10；[本地镜像ID](evidence/sec062-toolchain-58d2cb5/docker-images.txt)为runner结果，不是发布RepoDigest。只创建停止容器复制产物及执行builder go version，未挂数据/启动应用/推送镜像；新镜像运行/非root功能及OS扫描NOT_RUN，不能继承旧PR7成绩作为本次实跑。编译的WASM及匹配shim保留hash，浏览器交互NOT_RUN。

| 当前compiler实际HTTP+读回 | 用时 / 吞吐 | heap / RSS峰值 | 暂存表观 / allocated峰值 |
| --- | --- | --- | --- |
| 1GiB明文 | 5.091s / 201.14MiB/s | 2988200B / 24260608B | 2147483683B / 2147487744B |
| 2GiB服务器加密 | 15.956s / 128.35MiB/s | 3039736B / 24944640B | 6443121331B / 6443139072B |

两个读回SHA256匹配；只代表该runner/路径，磁盘按20ms采样，不比较不同runner的速度优劣或声称覆盖所有瞬时峰值。[最终公开证据](evidence/sec062-toolchain-58d2cb5/README.md)的44份Go实际编译hash、11份current input/harness逐项与58d2cb5 Git blob匹配；7份旧输入匹配cb5868c；6份生成产物hash来自runner，不是Git源码。一次本地证据整理器曾将ignored WASM错当Git输入导致校验失败，未发布通过结论；改成明确输入白名单后逐项校验，生成hash与源码hash分开。

扫描仍显示模块级GO-2026-5932（OpenPGP）、GO-2022-0646/0635（旧AWS S3 Crypto），受测配置未报告可达affected调用；本地Go源码检索也未导入openpgp/s3crypto，不将模块告警说成已修复。静态分析存在反射等限制；未加ignore或改动两份go.sum。最后仅补docs/evidence，无Go/运行器/workflow变更，新文档head PR CI另验。所有先前失败、取消、MSYS/WSL限制保留；本地Go全仓/race/govulncheck仍NOT_RUN。真实Redis/S3、全仓race、完整跨平台/浏览器/生产恢复和M3持久总量/区间/异步/恢复仍OPEN。

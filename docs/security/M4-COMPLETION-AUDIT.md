# M4 阶段收口审计

日期：2026-10-30。状态：**实现与验证验收通过；交付完成随[PR54](https://github.com/Activer007/SupaShare/pull/54)实际合并生效**。
合并前goal保持ACTIVE；实际merge SHA/tree及文档增量核对记录在PR54最终核对comment，核实后才将goal置为complete。本文件不预先声称某个尚未产生的merge SHA。
范围以[M4-GOAL](M4-GOAL.md)及用户明确修订为准：个人VPS、单Go进程、少量可信成员，公开下载者和File Request上传者不可信。
交付仅Activer007/SupaShare；Forceu/Gokapi只读。保持Go单体、SQLite/Redis、本地/S3、现有API及页面/WASM，不重开已冻结M3延期。

本次050起点811376bdcaa895b0dcb427de112640fa95c1a691；当前冻结候选538b8ec029278068872397d56f20f8b8bfaf9ca3，tree9f7fbc556d1c63f458c6384deac22f260ccbc07e。
该候选保留已合并的setup UI PR53；相对d7b810b的主应用响应策略闭包无变化，旧测试/浏览器结果仍按实际d7输入标记，不改写成538整树PASS。
Windows PowerShell / Go1.26.10 windows/amd64、CGO0；最终CI使用Linux。命令、失败历史与精确输入范围见[TEST-REPORT](TEST-REPORT.md)。

## 执行与证据纪律

| 要求 | 可检查证据与当前判定 |
| --- | --- |
| 固定本地路径、status/branch/HEAD/remote/default/Go，读取适用指令，fetch后开发 | 每包在TEST-REPORT记录实际起点；独立codex/分支、隔离归档及前后manifest。主线整合保留既有改动，无reset/clean/stash用户现场。 |
| README/docs、实现、测试及上游差异核对 | [基线](SECURITY-BASELINE.md)区分历史快照与当前源码；Forceu/Gokapi PR419/418只读核实仍open/unmerged，未直接套用不同事务/锁边界的补丁。 |
| 不变量→最小RED→最小修复，每PR一项 | [威胁模型](THREAT-MODEL.md)及下表映射；环境/夹具失败与业务RED分开，未凭缺头推定XSS，未凭HTTP200推定完整性。 |
| CONFIRMED_CODE / VALIDATE / HISTORICAL_FIXED | 各项结论保留证据范围；源码疑点不冒称攻击链，历史已修问题不替代当前验证。 |
| 隔离数据及私密审查 | 临时SQLite、合成文件、loopback对象服务与独立浏览器夹具；精确未知安全复现留SECURITY.md指定的自有未公开入口。无生产数据、上游写入、部署、镜像/公告/CVE发布。 |
| 凭据与日志 | 公开证据为脱敏摘要；原始夹具、密钥、Token及私密分享URL保留ignored私密目录。最终新增提交执行有限秘密模式/链接/diff检查，结果记PR54终审；不声称自动发现全部秘密。 |
| 必须维护的交付物 | SECURITY-BASELINE、THREAT-MODEL、SECURITY-TASK-BOARD、TEST-REPORT均已协调；重大删除/schema/加密兼容选择见ADR022–025。当前摘要与历史开发过程分开。 |

## 不变量与直接证据

| SEC / 要求 | 实现、合同与证据 | 当前判定 |
| --- | --- | --- |
| 040-A：失败保留重试锚点，不把未知当缺失 | [Cleanup.go](../../internal/storage/Cleanup.go)的checked引用/探测/删除/再次探测/条件退休；[故障合同](../../internal/test/securityfixture/deletion_cleanup_test.go)含DELETE失败、HEAD未知、假成功、权限/路径边界。 | 已验；[PR44](https://github.com/Activer007/SupaShare/pull/44)、[CI摘要](evidence/sec040-e1fd952/ci-summary.txt)。 |
| 040-A：重启可重试，共享源保留；锚点不恢复访问 | 同一故障合同含实际子进程退出后fresh reopen重试、跨owner共享、失败时GetFile/GetFileByHotlink拒绝访问及热链锚点保持。 | 已验；不承诺物理删除可撤销或安全擦除。 |
| 040-B：状态字段条件更新，旧timer/授权快照不覆盖新状态 | [deletion_state.go](../../internal/storage/deletion_state.go)、[timer](../../internal/storage/deletion_timer.go)及双provider CAS；[状态合同](../../internal/storage/security_deletion_state_test.go)覆盖旧owner/locator、旧token、缺源取消、缺失不复活；私密timer/恢复候选另证。 | 已验；[PR48](https://github.com/Activer007/SupaShare/pull/48)、[CI摘要](evidence/sec040b-cf30b57/ci-summary.txt)。 |
| 040-B：明确受理、pending、deleted/shared及失败反馈 | [API合同](../../internal/webserver/api/security_delete_state_test.go)与CLI非2xx合同；200成功空body及状态头、409冲突、503待重试。 | 已验；受理不承诺未来物理回收成功，见[ADR024](ADR-024-DELETION-STATE.md)。 |
| 040-B：FR及跨owner权限边界 | [FR合同](../../internal/storage/filerequest/security_deletion_test.go)：删除先过期/撤key，provider Commit按当前policy拒绝晚发布；清理失败保留请求及文件锚点；另一个owner控制保持。 | 已验限定合同；普通用户删除与全部新认证/在途上传的原子隔离仍VALIDATE。 |
| 041：替换与旧locator候选同事务/CAS，登记失败不patch | [SQLite](../../internal/configuration/database/provider/sqlite/blobreclaim.go)、[Redis](../../internal/configuration/database/provider/redis/blobreclaim.go)及各自security_blobreclaim_test；候选有界、去重并刷新token，满/坏结构拒绝。 | 已验；[PR50](https://github.com/Activer007/SupaShare/pull/50)、[CI摘要](evidence/sec041-d26875d/ci-summary.txt)。 |
| 041：跨重启回收，引用/未知不误删，重复不退款 | [worker](../../internal/storage/blob_reclaim.go)在blob锁内复读候选/ledger/全ID引用；[进程合同](../../internal/storage/security_blob_reclaim_process_test.go)真实exit97+fresh verifier；[引用合同](../../internal/storage/security_blob_reclaim_test.go)含共享、原target重新引用、发布竞争、未知Claim及完整GC交互。 | 已验；checked失败保留、旧ack不能退休新token、普通GC复用未知发布门禁。 |
| 041：迁库/格式兼容，不扫描猜归属 | [迁库合同](../../internal/configuration/database/security_blob_migration_test.go)保留候选/token；SQLite16→17、Redis9→10；GC每pass128/10秒，单项5秒，最多4096候选。 | 已验限定格式/进程合同；[ADR025](ADR-025-OLD-CONTENT-RECLAMATION.md)。 |
| 052-A：完整认证决定终态，失败不拼接错误文本或误SetComplete | [ServeFile](../../internal/storage/FileServing.go)前置500、流失败ErrAbortHandler；既有TestSEC052ServeFileFailure及私密真实HTTP健康/截断/认证失败。 | 已验；[PR43](https://github.com/Activer007/SupaShare/pull/43)、[CI摘要](evidence/sec052-d9d03ad/ci-summary.txt)。 |
| 052-A/B：WASM错误/取消传播，有限退出，源及metadata保持 | [Main.go](../../cmd/wasmdownloader/Main.go)controller.error及每响应取消标记；页面reader.cancel/writer.abort/finally；[服务器取消合同](../../internal/storage/security_download_cancel_test.go)含跨块SHA256、部分写入及真实HTTP1取消；私密Node原生流同合同RED→GREEN、同实例后续SHA256。 | 已验各自范围；[PR51](https://github.com/Activer007/SupaShare/pull/51)、[CI摘要](evidence/sec052b-5db6d0a/ci-summary.txt)。模块PASS不替代浏览器。 |
| 052：一个Chromium+HTTPS路径及当前补验 | 历史052-A真实HTTPS正常SHA256/认证失败终态通过；当前7c夹具页面/WASM已加载，但健康正例在SW证书校验阶段受阻。 | 当前健康/截断/原生取消补验为**SKIPPED_USER**，非PASS；见下节。 |
| 051：公开下载/hotlink/ZIP/解密/对象最终响应 | [header helper](../../internal/webserver/headers/Headers.go)及ZIP补nosniff，保留MIME/文件名/Disposition/inline sandbox；真实HTTP本地及loopback SDK最终响应、真实上传后Chrome附件与IAB hotlink样本。 | 已验限定样本；[PR52](https://github.com/Activer007/SupaShare/pull/52)合并811376b，最终[CI摘要](evidence/sec051-7c9a6ad/ci-summary.txt)。缺头不等于XSS。 |
| 050：最小通用头、渐进CSP、保持流式能力 | [ResponsePolicy.go](../../internal/webserver/ResponsePolicy.go)原writer直传；renderPage先Report-Only，观察后仅login/download/error强制base/object；[策略合同](../../internal/webserver/security_response_policy_test.go)覆盖实际HTTP、WASM/worker/iframe MIME、原writer及sandbox。 | 局部RED→GREEN及最终CI41002041972 SUCCESS；交付见PR54。 |
| 050：浏览器观察及必要兼容 | a084 Report-Only真实Chrome捕获6项现有inline策略信息；d7三页实际强制头与Report-Only分开，中文切换/正常PNG73B原生下载及SHA256保持；既有真实SSE上传Flush合同在新隔离副本GREEN。 | 浏览器及目标合同仅对应实际输入；最终PID已退出/drained，三份源metadata及1497项源manifest保持，见TEST-REPORT。 |

## 用户修订、边界与兼容

用户在没有已信任隔离HTTPS入口的情况下明确答复“没有，可以跳过此项测试”。该豁免仅覆盖当前受Service Worker证书校验阻塞的HTTPS健康/截断/原生取消补验。
这项记SKIPPED_USER并退出本次阻塞Gate；未安装系统信任或降低证书校验，未豁免其他M4测试，也未改变生产完整性不变量。
历史052浏览器/模块/HTTP证据保留原来源与范围；当前cleanup成功不算原生取消PASS。

兼容与回滚由[ADR022](ADR-022-RETRYABLE-DELETION.md)、[ADR023](ADR-023-ENCRYPTED-DOWNLOAD-ERROR.md)、[ADR024](ADR-024-DELETION-STATE.md)、[ADR025](ADR-025-OLD-CONTENT-RECLAMATION.md)约束。
040复用metadata且不改schema；041新增持久候选需升级前停写并备份配套DB/DataDir/配置/密钥，回滚恢复一致备份，禁止手动降schema。
052无密钥/密文格式迁移；回退会恢复已验证的错误传播风险。051/050只改响应策略，无数据/schema迁移；移除策略即失去对应防御，不能当作等效安全回滚。
已执行物理删除只能从备份恢复；不承诺清除备份、S3历史版本或客户端partial残留。

明确保留的限制：

- File Request不提供E2E；Firefox已知截断限制见[usage](../usage.rst)与[setup](../setup.rst)。服务器不检查E2E明文。
- ZIP单成员受损样本被归档成员读取拒绝，但HTTP200/正常EOF仍成立；不把归档拒绝写成传输失败PASS。其他ZIP/S3/E2E组合NOT_RUN。
- S3重定向后的最终对象不继承本站头；loopback对象MIME/Disposition/字节已查，nosniff/sandbox未出现。真实云/CDN及同源部署组合仍VALIDATE。
- 050策略限主应用handler；独立setup维护服务器不纳入该通用头覆盖声明。静态StreamSaver iframe不施加页面CSP，原文件inline sandbox保持。
- 脚本CSP保持Report-Only，未证明严格script-src防护或任意自定义插件兼容；未增全站nonce、全局框架隔离或报告端点。
- 真实Redis/S3持久化、断电、生产恢复/部署、额外浏览器/H2及历史孤儿人工处置均未从miniredis/loopback/进程退出推定完成。
- 普通用户删除的全部新认证/在途上传原子隔离仍VALIDATE；M3原延期按冻结范围保留，不扩成本轮新工作。

## 必要验证与最终收口Gate

历史各项最终CI摘要记录真实checkout/tree、环境、命令及失败；纯文档收尾不重复未变化的全仓/浏览器矩阵。
冻结050候选的实际CI、浏览器及收口记录如下，不借其他SHA的绿色状态补填：

| 最终要求 | 当前状态及需补的权威记录 |
| --- | --- |
| `go generate ./...`与生成diff guard | PASS，PR54最终CI41002041972/job123888516757。 |
| `go test ./...`的`test,awsmock`、`test,noaws`、`test,noaws,integration` | 三组及既有合成`test,awstest`均PASS，真实命令见TEST-REPORT。 |
| 相关`-race`、既有上传合同与build/scan | 全部必要CI步骤PASS，含新050策略/SSE；上传10项及toolchain19项marker均0。Windows原生race缺CGO为BLOCKED，js/wasm race实际不支持，未冒充PASS。 |
| CI输入与冻结候选树一致 | PASS；实际checkout2860f5df93a713de8009fd06efc7b6ac1837e3e0，父86af244+538，fetch核实tree9f7fbc5等于冻结树。push41001915393依同head并发规则取消，不记FAIL或PASS。 |
| 最终浏览器夹具退出、源/metadata/manifest保持 | PASS；同权限核实PID75888不存在、drained=true、三份canonical源metadata保持；1497项源Changed0/Missing0/Extra0。 |
| 自有PR54审查/合并、merge SHA/tree核对 | 最终538代码/工作流只读复核未发现阻断；文档增量终审及真实merge凭证以PR54记录为准。交付状态随实际合并及核对生效；自审不冒称外部独立批准。 |
| 四份安全文档、本审计及有限秘密/链接/diff核对 | 四份文档、本审计与[CI摘要](evidence/sec050-538b8ec/ci-summary.txt)协调；有限秘密/链接/diff检查记入PR54终审。保留原FAIL/SETUP_FAIL/NOT_RUN，不改写旧证据。 |
| M4完成与goal状态 | 实现/验证验收通过；仅在PR54实际合并、SHA/tree和文档增量核实后，goal才置complete。精确证据见该PR最终核对comment，不预先伪造合并结果。 |

PR43/44/48/50/51/52已进入当前祖先链，各自限定实现已交付；050最终输入验收另由上述证据证明。
本文件建立可复核映射及交付条件；不批准部署、发布、扩大测试豁免或自动开启下一阶段。

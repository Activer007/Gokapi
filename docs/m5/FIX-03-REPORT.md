# M5-FIX-03：本地与 S3 下载失败终态

2026-10-30。本地验收 PASS；独立 PR 与其最新 head 自动 review 以文末交付记录为准。当前版本为源码更新 `2.13.1 / 21301`，未发布、未部署，REL-01 继续 NOT_RUN。

## 基线、范围与状态

建包时 master 为 `77b8fa2c729db05a5e2ae3434b50f02eda127def`；本包从已审 FIX-02 提交 `831d08860aabc93fe1f7fc31112ba24cfd91cb67` 建立 `codex/m5-fix-03`。独立 stacked [PR #61](https://github.com/Activer007/SupaShare/pull/61)，base 为 `codex/m5-fix-02`；不借用前置 PR #60 的 review 作为本包 review。

过程候选为 `caee47cc41104f4bd3b0181e4e83ce960ccb7b2b`。其全仓 SDK/awsmock 回归暴露旧测试夹具失败；在不可变 831d088 上复现后，仅修正 `FileServing_test.go`，生产代码保持 caee47c。最终候选从真实 Git bundle clone 验证，不合成 VCS 提交。

- 最终受测代码 SHA：`cc41c169a87c904cfbee22634bf99af766f8cf96`，tree `94eb9a6eb7e8c50d644ce4d2bb458cb220fd53ce`。
- 1538 项 tracked 输入与该提交 Git blobs 逐字节一致，测试后逐项复核通过；manifest/bundle SHA256 见 [source.json](evidence/fix-03/source.json)。
- 独立 [PR #61](https://github.com/Activer007/SupaShare/pull/61)；最新 head 的实际自动 review 与精确交付 SHA 在该 PR 的 Codex Summary/交付 comment 核对；保持 OPEN，不自动合并。

工具链为 Go 1.26.10 linux/amd64、CGO=1、WSL Ubuntu 24.04 原生文件系统、非 root；Node 24.16.0、现有 Playwright/Windows Chrome。不可变基线由 Windows `git -c core.autocrlf=false archive` 导出后在 Linux 解包，不借用 Windows `.git` 链接。Actions 保持关闭，本包未运行 CI。

目标是使本地原始文件、服务器解密文件、S3 代理/强制解密以及 ZIP 的读取、写出、认证或取消失败得到正确终态，并同步源码版本。范围不包含新加密协议、密钥/密文格式、schema、下载额度政策、成功后才计次或失败退款；不改变文件收集的普通用户计数与管理员豁免。

## 已确认的问题与 RED 分类

[ADR-023](../security/ADR-023-ENCRYPTED-DOWNLOAD-ERROR.md) 已覆盖本地加密单文件的认证失败中断，并未宣称覆盖 ZIP/S3。本包补齐这些路径，不能把所有旧行为统一描述为“客户端静默成功”。

| 路径 | 冻结基线上的实际观察 |
| --- | --- |
| 本地 raw 源缺失 / 已知正数大小的源预先截短 | 缺失写出 HTTP200 错误文本；截短由 ServeContent 按实际文件大小重写长度，HTTP200 正常 EOF，内容少于元数据声明 |
| 本地 raw 在 WriteHeader 后截短 / 下游部分写入错误 | handler 正常返回且清除下载保护；客户端通常已收到 unexpected EOF。这里确认的是错误完成状态，不是客户端静默成功 |
| 本地加密 ZIP 同长度尾部认证损坏 | 错误文本进入附件，handler 正常返回，实际 HTTP 正常 EOF，仍出现 ZIP 结束目录；先前健康成员已提前完成 |
| ZIP archive.Flush、中央目录或 HTTP Flush 写出失败 | 错误被忽略，handler 正常返回、保护提前清除；受控真实 HTTP 可正常 EOF，部分场景仍有成功 footer |
| S3 proxy 非200、已知短 CL、chunked 短 EOF、声明完整 CL 后断流、下游写错 | SDK proxy 丢弃错误或未校验状态/实际长度；storage 可能正常返回并 SetComplete。非200且错误正文恰好匹配声明长度时可成为完整 HTTP200；短响应不一概等于客户端正常 EOF |
| S3 强制解密 / ZIP 远端成员尾部认证失败 | SDK 已返回错误，storage 的 helper.Check 产生普通 panic，HTTP 已会中断；新增合同要求标准 ErrAbortHandler，ZIP 健康成员也不能提前完成。这不是首次发现静默认证成功 |
| S3 请求取消 | 基线读取没有绑定下游 context，取消后仍阻塞上游；受控逃生释放只能排空夹具，不能算取消通过 |

首轮修复后又补了独立边界合同，**其 RED 对象是当时 root 的修复中候选，不是原始 831d088**：完整 HTTP416 拒绝留下保护、空 S3 proxy 响应留下保护，以及本地完整合法密文的 ZIP 明文大小与元数据差1仍成功封档，均得到业务 RED。本地空加密单文件、S3 强制解密空文件原本即通过，只作为正例保留，未把推测写成已复现漏洞。

## 修复后的 HTTP、ZIP 与保护语义

[ServeFile / ServeFilesAsZip](../../internal/storage/FileServing.go) 在附件输出前的本地打开失败、已知长度不符或远端表示拒绝返回普通 HTTP500；远端错误分支移除附件 disposition/length。ZIP 自身已输出200后，成员失败直接中断，不再尝试插入500正文。

正文开始后的读取、认证、写出或取消失败统一使用 `panic(http.ErrAbortHandler)`，由 net/http 中断 HTTP/1 传输；不向附件追加错误文本，也不把 return false 当成后续重定向信号。已发送的200无法回写为500，验收依据是实际客户端传输错误、标准中断与未进入完成终态。

[downloadResponse](../../internal/storage/download_response.go) 观察 Write/ReaderFrom 的错误、写出字节数及最终 Content-Length，继续使用 ServeContent 原有 Range/条件请求逻辑，并保留底层 ReaderFrom 与 ResponseController.Unwrap。正常的隐式空200可完成；304、412、416 的完整条件/范围拒绝释放保护，表示请求已终止，不表示文件交付成功。测试覆盖健康 full、Range、HEAD、304、416 与空文件；412 的兼容分支已审代码，未单独扩充为专用场景。

ZIP 不再通过 defer Close 给失败流封档。只有全部成员成功、archive.Flush、中央目录 Close、HTTP Flush 和最终 context 检查通过后，才统一 SetComplete；成员/认证失败不再主动补成功目录，不提前清除已开始成员的保护。Close 成功后的最终传输 Flush 仍可能失败，此时中断并保留 guard，不能追回已交给底层的 footer。成员在独立函数中 defer 关闭本地 FD，避免加密 ZIP 成功路径遗漏关闭或把整个归档的 FD 累积到最后。这里没有宣称做过 FD 压力测试。

本地已知正数大小必须一致；加密 ZIP 同时检查已知密文大小及实际明文输出量。负数/溢出拒绝，本地历史 `SizeBytes=0` 未知大小兼容按现有范围保留，真实空文件作为独立正例验证。S3 按元数据区分原始/E2E 字节数与服务器加密开销，并检查已知 CL、未知长度响应的最终源/输出计数。

[S3 下载](../../internal/storage/filesystem/s3filesystem/aws/Aws.go) 使用下游 context 创建 proxy HTTP 请求及 SDK GetObjectWithContext，StreamContext 贯穿 ZIP；旧 Stream 包装保留。noaws/awsmock 提供同签名的取消边界。proxy 显式请求 identity 编码，拒绝上游非200与不符的已知长度；服务器解密预检不代替完整流认证。E2E 单文件保持原始字节，管理 ZIP 继续拒绝不支持的 E2E 成员。非阻塞 S3 redirect 仍无法确认客户端完成，沿用其既有保护期限，不被此次 proxy 合同替代。

下载次数及流量继续按既有受理尝试记账，失败不退款，也不因错误额外删除源对象或退休 metadata。失败流的已有保护交给既有到期清理；成功/完整拒绝及时释放，避免误 pin。合成合同核对源字节与 metadata 保持，不以恢复损坏源文件为本包目标。

## RED / GREEN 与目标验证证据

原始证据目录：`C:/Users/10747/.codex/visualizations/2026/10/30/01a18b05-a819-78a1-8cb6-a36026bc4840/m5-fix-03`。以下路径均相对此目录；每组的 command、exit、日志及 source manifest 对应各自冻结输入，不代表同一最终提交。

| 过程合同 | 实际结果与定位 |
| --- | --- |
| 本地真实 HTTP，初始8个故障及健康对照 | 831d088 业务 RED exit1 / 0.487s；同测试修复后 GREEN / 0.476s、race / 2.599s。`local-terminal/final-source/{source.json,red.log,green.log,race.log,results.json}` |
| SDK loopback 返回值、长度、写错、取消、真实加密边界 | 基线 RED exit1 / 4.500s；GREEN / 0.671s、race / 2.765s。`sdk-source.json`、`sdk-{red,green,race}-final.log`；另有 noaws/awsmock 的已取消 context 边界通过 |
| SDK → storage → 实际 HTTP，初始12场景 | RED exit1 / 4.649s，8故障/4健康分类如前；GREEN / 3.678s、race / 5.864s。含下游实际3秒取消、上游 context 观察与拒绝使用4秒夹具逃生。`remote-terminal/{source.json,red.log,green.log,race.log,results.json}` |
| 后补边界，与修复中候选对比 | local RED / 0.358s、remote RED / 0.276s；上述三项实证失败，空加密及 full/Range/HEAD/304 对照通过。修复后 local GREEN / 0.357s、remote GREEN / 0.304s，目标 race / 2.514s、2.381s。`terminal-edges/{source-red.json,source-green.json,*-red.log,*-green.log,*-race.log,results.json}` |

新增本地/远端合同为 [TestFIX03LocalDownloadTerminal](../../internal/storage/security_download_terminal_test.go)、[共享真实 HTTP helper](../../internal/storage/security_download_terminal_http_test.go) 与 [TestFIX03RemoteDownloadTerminal](../../internal/storage/security_download_remote_terminal_test.go)。最终远端矩阵为14场景，额外两个为空 proxy/空强制解密控制。共享 helper 仅在注入故障时包装 writer，健康请求直接使用实际 HTTP writer，覆盖保留的 ReaderFrom 路径。

测试使用独立子进程隔离 encryption master 与 TestMain，合成对象、固定测试凭据和 loopback origin；SDK 初始化的 absent-key HEAD 返回404。截短/写错通过同步事件触发，取消通过实际客户端与上游 context 观察，超时只作测试界限或有明确失败标记的排空。同步 traffic prime 避免延迟 SQLite 保存，另观察累计流量推进；不把累计目标当成逐 worker join。早期脚本 mkdir 设置失败未运行 Go 测试，另存记录、不计业务 RED。

## 旧夹具调查与最终验收

过程 `final/results.tsv` 的 caee47c：full-noaws PASS/wall86s，但 full-SDK exit1/wall114s、相关 awsmock race exit1/wall13s，旧日志完整保留。源码 tracked 输入未漂移；旧 CLI 构建在测试之后受 untracked fixture 标记为 dirty，不作为干净发布产物。

旧 SDK 三项测试在 831d088 同样失败：`testconfiguration.EnableS3` 仅在 awsmock 配置模拟凭据，SDK 的 TestMain 只启动 endpoint，未提供完整配置；单跑 `TestServeFile` 又依赖前序 `TestNewFile` 的 global ID。旧代码继续服务空模型，新严格下载合同会标准中断；不能放宽生产检查迁就夹具。修正显式 synthetic loopback S3 初始化/driver 切换、独立创建小下载对象、Cleanup 的幂等恢复，并用真实 20MiB 字节匹配原大文件 header，继续覆盖 S3 临时文件路径。SDK 核对实际对象尺寸，未降低为仅测 metadata。

同一最终测试文件 SHA256 `7e30f26fcb93bf215d63875f2b3e705a216fb569ebbf7d9aff4aefef56290e50` 在 831d088 的三个 SDK 目标及 caee47c 的 SDK/noaws/awsmock 对照均通过；SDK/awsmock 完整 storage 与相关 race 均通过。证据为 `fixture-audit/{source.json,results.tsv,baseline-final-sdk.*,final-*}`，不计入生产故障 RED。

最终 `cc41c16` 在独立原生 Git clone 中先生成、确认干净、构建并记录 CLI，再运行测试。精确命令与结果见 [results.tsv](evidence/fix-03/results.tsv)，过程原始日志仍保留，不覆盖失败。

| 检查 | 当前结果 |
| --- | --- |
| `go generate ./...` / tracked diff guard | PASS，3s / 0s |
| local 新合同与既有 SEC052 race，`test,noaws` | PASS，wall7s |
| storage + SDK 新合同、context/SEC032 相关 race，`test,awstest` | PASS，wall9s |
| `go test ./... -tags=test,noaws -count=1 -timeout=300s` | PASS，wall91s |
| `go test ./... -tags=test,awstest -count=1 -timeout=300s` | PASS，wall111s；SDK 对 synthetic loopback origin，无真实云凭据 |
| `go test ./... -tags=test,awsmock -count=1 -timeout=300s` | PASS，wall84s |
| 相关 storage/SDK/webserver/API race，`test,awsmock` | PASS，wall12s；含新 context、独立上传/下载及 Collection 回归 |
| 默认 / noaws server build、CLI `--version` | PASS；干净 tree 构建，CLI `2.13.1`、GitCommit=`cc41c16`，无 modified 标记；手工开发构建，不是发布产物 |
| `TestShowVersion` | PASS，wall1s |
| Node 全部合同 | PASS 51/51、0 skip，`node-all.log` |
| 最终 commit blobs / native 输入核对 / 测试后逐项复核 / tracked tree guard | PASS，1538 项；测试产生的 untracked fixture 不混入构建与源码输入 |

14 条最终命令各自 exit0，结果经过独立核对，见 [result-audit.json](evidence/fix-03/result-audit.json)。原 shell runner 在全部完成并输出 `VALIDATION_EXIT=0` 后，末行 CR 使 `exit` 收到 `0\r` 并返回1；原脚本与日志保留，作为包装脚本错误记录，不伪称整体 shell exit0，也不重复运行已通过的测试。上述各命令结果、1538项 audit 和空 tracked diff 单独核验成功。未把候选 PASS 外推为 master 已合入或完成发布验收。

## 2.13.1 源码同步与浏览器 QA

CLI、API `Version/VersionInt`、页面 footer、两份 OpenAPI 的版本/示例、双语 README 与 changelog 已同步为 `2.13.1 / 21301`，生成资源及模板引用已更新。changelog 明写 source update / not a published release；未创建 release、tag 或发布安装包。

`version-browser-result.json` 记录现有 Chrome/Playwright 在隔离合成 HTTP 服务上的实际登录与下载：footer 为 SupaShare v2.13.1，版本 API HTTP200 且返回 `2.13.1 / 21301`，实际 Swagger 为2.13.1；健康附件180224字节、SHA256匹配、failure=null，损坏附件由 Chrome 下载器报告 `canceled`。相关 pageerror 列表为空，截图 `version-2.13.1-browser.png` 已检查。Browser plugin 不可用，使用现有 Playwright/Chrome，未安装新依赖。

预览从 caee47c 的 bundle 建立独立副本，仅添加仓库外合成夹具与将测试监听端口53843改为53934；生产代码与最终 cc41c16 相同，后者只追加旧 Go 测试夹具修正。预览/浏览器 context 均已结束。双语 README 仅更新当前源码版本段落，历史截图的版本标签仍描述其实际截图；没有为同步版本冒改历史图片。

这是一个 Chrome、合成数据与所列 HTTP 下载路径的证据，不外推为所有浏览器、E2E/WASM、H2/TLS 或真实云存储保证；浏览器页面的“已请求下载”文案也不单独作为成功下载证据。

## 兼容、回滚与交付边界

无数据/schema/密钥迁移；文件分享 URL、加密格式、权限、收集下载计次/管理员例外和受理尝试记账保持。正常附件、Range/HEAD/条件请求、真实空文件及所列本地未知大小兼容有控制合同。回退会恢复已验证的失败终态问题，不是等效修复；恢复损坏对象须走核实备份或重新上传，本包未执行该恢复。

真实云 S3/网络故障全组合、真实 Redis 持久化、HTTP/2/TLS 全矩阵、其他浏览器/客户端/取消组合、FD压力测试均 **NOT_RUN**。安装/升级、实际备份恢复、回滚演练、RC 与生产部署/发布属于 **REL-01 NOT_RUN**。M5 总目标继续 ACTIVE；独立 PR 的最新精确 head review、合入及合入后最终 master 验收分别记录，不自动合并。

交付 [PR #61](https://github.com/Activer007/SupaShare/pull/61) 基于 PR #60，前置 #59/#60 均保持 OPEN；仅本包差异进入审查。前置 PR 合并后须重新核对 diff/base 及受影响闭包；不代替用户跨包合并。最终受测 code 为 cc41c16，此后的交付提交仅为文档/证据，不改变 build/test 输入；远端 review 对应最终 head，二者分别记录。Actions 持续关闭，未发布、部署或修改上游。

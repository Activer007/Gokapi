# M0 本地测试报告

日期：2026-10-27（Asia/Shanghai）。初始源码基线 a9c25d453aab0c25f2d6e6926ec7312628638844。目标是建立可复现证据；这里的 PASS 不代表上线许可。初始 M0 仅本地交付，后续按用户授权在 Activer007/Gokapi 创建、审查及合并 PR；无部署。最新审查结果见末尾增补，早期记录保留原始结果。

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

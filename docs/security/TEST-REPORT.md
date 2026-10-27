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

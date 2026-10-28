# SEC-030 整文件准入验证

工作基线710f4bf，运行时代码9db2542bbdcda382db2c6aebbad9da672053ea0e。
[PR #21](https://github.com/Activer007/Gokapi/pull/21)仅在自有仓库；无upstream写入、部署或镜像发布。

[PR CI40778813175](https://github.com/Activer007/Gokapi/actions/runs/40778813175)与
[完整手动40778841823](https://github.com/Activer007/Gokapi/actions/runs/40778841823)均SUCCESS。
手动job123135051507，ubuntu-latest/Linux amd64、Go1.26.10、GOTOOLCHAIN=local。
实际步骤与退出码见manual-job.json/manual-results.txt；原始日志仅保留ignored目录。

真实执行：go generate、占配置端口合同、四组全仓test,awsmock / test,noaws /
test,noaws,integration / test,awstest；receiver race；run-sec030-whole.sh target/race
（包含provider、整文件10/50、共享blob、失败、GC及AWS mock/SDK loopback）；
run-sec062-toolchain.sh scan/historical/containers；run-sec030-receiver.sh large。
历史扫描退出3是预期RED，当前6项扫描均退出0。

govulncheck固定1.9.0，数据库更新时间2026-10-27 21:16:18 UTC。模块级OpenPGP
GO-2026-5932及旧AWS S3 Crypto GO-2022-0646/0635仍出现；受测配置未报告affected
可达调用，不声称模块无公告或覆盖反射/真实部署。未修改依赖或忽略公告。
三个Docker builder构建和compiler版本检查通过，镜像只在runner本地使用；未做OS扫描、
真实容器应用运行或发布，不能继承旧部署成绩。

| 真实HTTP上传及SHA256读回 | 时间 / MiB每秒 | heap / RSS峰值 | 暂存表观 / allocated峰值 |
| --- | --- | --- | --- |
| 1GiB明文 | 9.219s / 111.07 | 3053736B / 24420352B | 2147483683B / 2147495936B |
| 2GiB服务器加密 | 27.981s / 73.19 | 3079136B / 25628672B | 6427254819B / 6427271168B |

原始数值及合成内容hash见large-metrics.txt。20ms采样，不声称捕获所有瞬时峰值、比较
不同runner速度或覆盖真实S3/Redis/全部M3路径。

manual-source.sha256是该运行日志中实际可提取的48个唯一Go输入，逐项与9db2542 Git
blob核对；旧scope主要覆盖receiver/入口，没有把它称为完整provider/whole存储清单。
artifacts.sha256的6项是runner实际生成产物，不是Git源码。后续仅扩展源码哈希日志及
补文档，不改运行时Go；最终PR CI另核对完整scope的输入hash。

Windows本地相关目标和生成通过，但三组全仓实际FAIL，race因缺GCC未执行。
五项有限测试在干净710f4bf Windows基线同样失败；full-suite ParallelDownloads超时
及后续fixture残留另保留VALIDATE，不从Linux绿色推定Windows全部兼容。详见TEST-REPORT。

范围仅为整文件API。其他入口累计准入、完整崩溃目标对账及物理删除可靠性仍OPEN，
不能将本增量记为M3关闭。

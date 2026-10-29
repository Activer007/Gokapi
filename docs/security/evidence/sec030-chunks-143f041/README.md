# SEC-030：分片与 File Request 代码 Gate

工作基线 `57e5116b466e9be15ae8b63f5ba088f1120c1635`；已验证代码
`143f041162605a160c98f9cf0420033260d8bded`，[PR22](https://github.com/Activer007/Gokapi/pull/22)。
Ubuntu24.04 runner / Go1.26.10 linux/amd64；仅本人仓库及合成隔离数据，无部署。

[PR CI40798733852](https://github.com/Activer007/Gokapi/actions/runs/40798733852)及
[完整手动40798813978](https://github.com/Activer007/Gokapi/actions/runs/40798813978)实际SUCCESS。
生成、四组全仓、receiver/quota相关race、当前扫描、历史编译器预期RED合同、三镜像本地构建、
原整文件1/2GiB及新增分片大文件均通过；详见[数值结果](ci-runs.json)。

两份job日志均按Git blob二进制内容核验：本清单范围202份Go、2份harness全部匹配，
57e5116→143f041的50份非删除Go变更全部覆盖，0 mismatch。
[source.sha256](source.sha256)包含204项；[verification.json](verification.json)保留范围及数值。
这不是全仓282份native输入或语句/分支测试覆盖率声明。历史编译器的旧workflow输入
独立绑定cb5868c6，不混作当前harness；两者各自匹配其Git版本。

| 分片场景 | HTTP上传/完成 | SHA256读回 | 耗时 | 吞吐 | heap/RSS采样峰值 |
| --- | --- | --- | --- | --- | --- |
| 普通1GiB明文 | PASS | PASS | 15.780s | 64.89MiB/s | 3512472/27406336 bytes |
| File Request 2GiB服务器加密 | PASS | 解密后PASS | 35.248s | 58.10MiB/s | 3594128/28213248 bytes |

分片采用未知Content-Length、file-first multipart和8MiB流式正文；按真实字节连续上传，完成后
直接打开隔离canonical blob流式解密/哈希，不把它称为HTTP下载或真实浏览器运行。
磁盘表观/分配峰值及SHA256在JSON中。20ms采样不证明全部瞬时峰值，也不是生产吞吐承诺。

在新的隔离clone checkout上述代码SHA后，用同一Go版本执行：

```sh
go generate ./...
bash docs/security/tools/run-sec030-whole.sh target
bash docs/security/tools/run-sec030-whole.sh race
bash docs/security/tools/run-sec030-whole.sh large
```

large显式传入5120/20480 MiB字节上限（用户选择），并要求恰有两条实际metric；不足空间或未运行
不能记PASS。完整四组/scan/镜像命令由该SHA的workflow定义，CI步骤与结果可从上述链接重读。
Windows完整noaws的真实失败、修正夹具和局部结果保留于[TEST-REPORT](../../TEST-REPORT.md)，
不因Linux通过删去失败。原始本地与decoded CI日志留在ignored目录，不提交潜在敏感正文。

完整M3仍包含positive metadata准入、跨写者物理容量协调、Claim/发布后崩溃对账、真实Redis/S3及
全部路径慢速/浏览器矩阵；本增量的绿色不关闭这些Gate。升级/回滚见[ADR017](../../ADR-017-CHUNK-UPLOAD-ADMISSION.md)。

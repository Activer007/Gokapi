# 上传恢复：停机核对流程

适用：个人VPS、单Go进程。先在隔离副本演练；本流程不自动修复真实数据。
启动恢复在GC和HTTP之前运行。出现`upload recovery requires manual review`即停止启动，
摘要只有固定原因码和哈希引用；不要把数据库、原始路径、密钥或私密链接贴到公开Issue。

1. **停止进程与自动重启。** 确认没有第二个Gokapi进程、上传客户端、GC或其他写者。
2. **完整备份。** 保存SQLite DB（停机后连同存在的WAL/SHM）或经过验证的Redis快照、整个DataDir、配置和加密材料。
   保存当前二进制/源码SHA；先核实备份可读。备份不进入Git或公开日志。
3. **在离线副本核对。** 读取quota ledger及对应metadata；诊断ref是reservation ID的SHA256前16个十六进制字符。
   `claimed`/`unknown_record`不能凭超时或缺少input退款；先核对publication和提交是否存在。
   `whole_work`没有持久归属映射，前缀和mtime都不是删除依据；canonical永不由该恢复器删除。
4. **只处理已确认对象。** 对已提交记录，先验证metadata的owner/request/大小/提交摘要、canonical内容及加密材料。
   本地whole工作硬链接仅在同一文件身份（same inode/Windows等价身份）和完整内容hash均与已确认canonical一致时，
   才可在备份副本中演练移除该私有链接；不重命名或删除canonical。仅大小/同名/相同hash而无可信归属不足以授权删除其他工作文件。
   未确认Claim、S3远端、服务器加密或孤儿情况保留文件与额度，另行人工审查；不手写`StagingReleased=true`绕过清理。
   私有namespace的foreign entries、错误marker或符号链接需先确认来源，在副本中修正；不能直接递归删除目录。
5. **重新启动并核对。** 让启动恢复器清理已确认chunk input并事务释放stage，观察摘要与服务启动；下载并核对内容hash，
   检查metadata只有一条、额度未重复释放；再次停止/启动验证幂等。确认后才恢复客户端和自动重启。

正常已清理文件不能靠回退代码恢复。需要回滚时先停机备份，再切回已知版本；若有不确定现场，必须离线处置完毕再启动旧版GC。
当前参考验收只覆盖SQLite/local的进程退出；断电持久化、真实Redis/S3、自动补提交、canonical孤儿GC仍为OPEN/NOT_RUN。
活跃未Claim记录保持预留直到过期后再次恢复或现有session清理；不新增后台自动退款任务。

# ADR-005：显式非 root Compose 与持久化卷

2026-10-28；SEC-011；基线462c5d57dc428ccedb352b2c84c4367331687872；仅本 fork，尚未部署。

## 决策与不变量

新增独立 docker-compose.nonroot.yaml，应用和 entrypoint 从启动即以显式 UID/GID（默认10001:10001）运行。禁用旧 DOCKER_NONROOT 分支，不递归 chown/chmod。主机只发布回环端口，根文件系统只读，删除 capabilities，禁止提权。config/data 是独立可写外部命名卷；要求部署者预先准备，并显式指定已测试的 fork 镜像。没有通用 .env 文件自动注入任意旧选项。

Dockerfile 和 Dockerfile.multiarch 的默认 USER 保持原值；默认镜像仍是 root。这是兼容选择，不宣称所有启动方式默认非 root。既有 --user 支持为 HISTORICAL_FIXED；缺少可复现 Compose 验收为 CONFIRMED_CODE。旧 DOCKER_NONROOT=true 仍含递归改权限逻辑，本配置始终显式 false，禁止同时使用它迁移旧卷。

不匹配权限必须导致启动失败且不改变属主；正确权限下的 bootstrap、认证、上传、匿名下载、重启持久化必须真实成功。测试的 root helper 只准备两个全新、空的、带随机标记的卷根目录，绝不对既有数据运行。

## 新部署与兼容

安装步骤见 docs/setup.rst。容器 UID/GID 不需要主机存在同名账号，Linux 按数字检查权限。外部命名卷不会由 Compose 创建或随 down 删除。SQLite、配置、密钥保存在 config；文件、日志、临时上传在 data。设置向导中 SQLite 路径必须位于可写挂载内（例如 /app/config/gokapi.sqlite）。当前示例面向本地存储和 SQLite，Redis/S3 设置按既有方式额外配置；本轮不声称真实 Redis/S3 容器矩阵已验收。

代理配置须遵守 ADR-003，明确配置真实直接代理 IP/CIDR；主机回环发布并不自动认证代理。使用 HTTPS 或 SSH tunnel 进行设置。读取本地证明文件时使用该容器用户，不将证明放入日志或命令参数。

## 旧数据迁移与回滚

先记录旧镜像ID、UID/GID、卷名及配置，再停写并取得配套 config/DB/data/密钥备份。只在独立的新卷或目录副本上迁移，保持原卷不动。在副本上核对符号链接、外部SQLite路径、文件数量、内容摘要、权限与剩余磁盘空间，再明确将副本属主调整为目标数字UID/GID；此步骤需要部署者针对真实路径制定命令，本项目不提供会扫描或递归改真实卷的自动脚本。重放合成/受控上传下载与重启验证后切换挂载。

回滚到本项前的同一应用版本：停止新容器，恢复旧镜像及原挂载/UID配置；不改数据库schema、不改密码/文件格式。新容器发生写入后不能简单丢弃新卷，否则丢失新文件及元数据；须停写、保留两套快照并规划同步。跨 Argon2id/旧稳定版的回滚另遵守 ADR-002，不能只换镜像。测试仅证明已有配置重启，不把它描述成旧生产卷完整迁移演练。

## 验证与边界

run-sec011.py 用 git archive 的源码构建镜像，记录源SHA、构建日志、镜像ID和脚本/Compose hash。归档显式设置 core.autocrlf=false，dockerentry.sh 固定LF，避免 Windows 转换使容器 shebang 失效。使用独立 Compose 项目和新建标记卷，验证错误属主负例、进程身份、初始化权限、真实HTTP登录/上传/下载SHA256和重启持久化。HTTP凭据仅在内存流转，不输出响应体、认证后的容器日志或分享URL；仅保留空卷安装前的脱敏错误日志。退出清理仅按本次生成的容器ID、卷标签及项目网络标签执行；测试镜像可保留用于审计。

基础镜像标签尚可变，记录本次镜像ID并不等于完成可复现发布/供应链 Gate；固定digest与发布签名仍归 SEC-062。真实旧卷迁移、不同Linux发行版/SELinux、rootless Docker、Redis/S3和浏览器 E2E 不由本轮本地存储验收推定通过。

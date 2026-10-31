# 已完成：M5 快速收尾（REL-01 Lite）

更新：2026-10-31。仓库仅为 `Activer007/SupaShare`。PR #56～#61 已合并，FIX-01/02/03 已完成；本轮基线为 master `54e65f45b63978823c9eee8155dc19d81ba51442`，源码版本 **2.13.1 / API 21301**。

## 本轮范围

- 仅验证 SQLite＋本地存储的安装初始化、登录、上传、分享下载 SHA-256、File Request 收集及重启持久化。
- 使用合成数据，从已知 2.12.0 快照 `94ba10fd57feec60e463f63bd11e5f41d20cae46` 升级到当前版本，核对 schema 17→18、旧文件/链接、用户及收集设置。
- 应用停止后备份完整 config/data（本次 SQLite 位于 config），在独立目录恢复新版备份；仅用升级前快照运行旧版，不将迁移后的数据库交给旧版。
- 必须取得 Docker Compose 全新安装的实际结果；原生运行通过不能代替 Compose 验收。

## 当前结果与完成条件

原生隔离安装、核心流程、重启、升级、备份恢复和快照回滚已通过；2026-10-31 补齐独立 Docker Compose 全新初始化、登录、上传/分享下载 hash、文件收集与重启验证，SQLite schema 18 / integrity_check=ok。详见[验收表](M5-ACCEPTANCE.md)。[PR #62](https://github.com/Activer007/SupaShare/pull/62) 已审查合入，合并提交 `15f3549`；M5 按本轮 Lite 范围完成，不扩展发布验收范围。

复用 FIX-01/02/03 的有效证据，不重跑源码未变的完整测试，不新增大型报告或框架。旧 REL-01 分支及命名 stash 保留，未直接应用。实际修改以小型独立 PR 交付，不自动合并。

真实 Redis/云 S3、HA、断电矩阵及生产发布不在本轮范围。GitHub Actions 保持关闭，不发布 Release/tag/镜像或部署。完成本 Goal 后停止，不自动进入下一里程碑。

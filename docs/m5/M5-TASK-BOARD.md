# M5 任务看板

更新：2026-10-31。PR #56～#61 均已合并；本轮受测 master 为 `54e65f4`，源码 2.13.1。REL-01 Lite 验收完成，复用 FIX-01/02/03 已完成的验证。

| 包 | 状态 | 基线及证据 | 交付 |
| --- | --- | --- | --- |
| FIX-01 删除生命周期及测试稳定性 | MERGED | 受测代码 `a122c0179c87c697132309eb6612efe99b1d1215`；[报告](FIX-01-REPORT.md) | [PR #59](https://github.com/Activer007/SupaShare/pull/59)，已审 head `ce2a3a9`，合并提交 `9478e55` |
| FIX-02 首次 File Request 额度初始化 | MERGED | 原始受测代码 `9c18a42d834531290e426863486ad37e85f3f3e7`；[报告](FIX-02-REPORT.md) | [PR #60](https://github.com/Activer007/SupaShare/pull/60)，代码审查对应 `831d088`，文档冲突解决后合并，提交 `bed031f` |
| FIX-03 本地/S3 下载失败终态；2.13.1 源码版本同步 | MERGED | 原始受测代码 `cc41c169a87c904cfbee22634bf99af766f8cf96`；[报告](FIX-03-REPORT.md) | [PR #61](https://github.com/Activer007/SupaShare/pull/61)，代码审查对应 `793613e`，文档冲突解决后合并，提交 `54e65f4` |
| REL-01 Lite：SQLite/本地安装、升级与恢复 | NATIVE_PASS / COMPOSE_PASS | 2.12.0 `94ba10f` → 2.13.1 `54e65f4` 的原生升级、冷备恢复与旧快照回滚通过；独立 Compose 全新安装、登录、上传/分享下载 hash、收集及重启通过 | [PR #62](https://github.com/Activer007/SupaShare/pull/62)，不自动合并；原 `codex/m5-rel-01` 命名 stash 保留，未应用 |

GitHub Actions 关闭；原生升级/恢复与 Compose 安装分别记录。建议 PR #62 审查合入后关闭 M5；本轮不发布、部署或自动合并 PR。

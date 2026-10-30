# M5 任务看板

更新：2026-10-30。当前开发包为 FIX-03；FIX-01/02 已完成审查交付，分别保持独立 PR。

| 包 | 状态 | 基线及证据 | 交付 |
| --- | --- | --- | --- |
| FIX-01 删除生命周期及测试稳定性 | LOCAL_PASS / REVIEW_PASS / OPEN | master `77b8fa2c729db05a5e2ae3434b50f02eda127def`；受测代码 `a122c0179c87c697132309eb6612efe99b1d1215`；[报告](FIX-01-REPORT.md) | [PR #59](https://github.com/Activer007/SupaShare/pull/59)，已审 head `29367b5`，未合并 |
| FIX-02 首次 File Request 额度初始化 | LOCAL_PASS / REVIEW_PASS / OPEN | 基于已审 `29367b5`（master 仍为 `77b8fa2`），受测代码 `9c18a42d834531290e426863486ad37e85f3f3e7`；[报告](FIX-02-REPORT.md) | [PR #60](https://github.com/Activer007/SupaShare/pull/60)，已审 head `831d088`，未合并 |
| FIX-03 本地/S3 下载失败终态；2.13.1 源码版本同步 | LOCAL_PASS / OPEN | 基于已审 `831d088`；受测代码 `cc41c169a87c904cfbee22634bf99af766f8cf96`；[报告](FIX-03-REPORT.md)；HTTP RED→GREEN、三组全仓/race/构建通过 | [PR #61](https://github.com/Activer007/SupaShare/pull/61)，base 为 `codex/m5-fix-02`；最新 head review 见该 PR，未自动合并 |
| REL-01 安装/升级/备份/恢复/回滚/RC | NOT_RUN | 必须隔离环境真实演练并记录精确源码 SHA | 未创建；发布另需批准 |

GitHub Actions 关闭；不把旧 CI 或模拟后端测试当作生产发布验收。M5 总状态 ACTIVE。

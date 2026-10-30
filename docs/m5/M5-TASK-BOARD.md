# M5 任务看板

更新：2026-10-30。按用户最新指示，当前 Goal 仅 FIX-01。其他阶段历史工作保留，但不自动继续；本看板不将它们的交付纳入当前完成条件。

| 包 | 状态 | 基线及证据 | 交付 |
| --- | --- | --- | --- |
| FIX-01 删除生命周期及测试稳定性 | LOCAL_PASS / PR_REVIEW / OPEN | master `77b8fa2c729db05a5e2ae3434b50f02eda127def`；受测代码 `a122c0179c87c697132309eb6612efe99b1d1215`；[报告](FIX-01-REPORT.md) | [PR #59](https://github.com/Activer007/SupaShare/pull/59)，旧 head `29367b5` review completed/no findings；最新文档 head 审查与精确交付见该 PR；不自动合并 |
| FIX-02 首次 File Request 额度初始化 | OUT_OF_SCOPE | 仅保留历史工作，不继续本轮执行 | 已有独立 [PR #60](https://github.com/Activer007/SupaShare/pull/60)，非当前 Goal |
| FIX-03 本地/S3 下载失败终态 | OUT_OF_SCOPE | 仅保留历史工作，不继续本轮执行 | 已有独立 [PR #61](https://github.com/Activer007/SupaShare/pull/61)，非当前 Goal |
| REL-01 安装/升级/备份/恢复/回滚/RC | DEFERRED_USER | 用户要求回到 FIX-01，已停止所有本轮自有进程并保留证据；未完成不记 PASS | 未提交改动保留在 `codex/m5-rel-01` 的命名 stash；未创建 PR |

GitHub Actions 关闭；不把旧 CI 或模拟后端测试当作当前源码证明。当前 Goal 的完成条件仅为 FIX-01，不宣称整个 M5 已完成。

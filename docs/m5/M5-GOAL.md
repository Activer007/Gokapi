# M5：Core Stability & Release Readiness

用户冻结范围，2026-10-30。仓库仅为 Activer007/SupaShare；单个 Go 进程、现有 Web UI、SQLite/Redis、本地/S3 和兼容 API 保留。

依次交付独立工作包：

1. FIX-01：删除与回收测试稳定性。
2. FIX-02：首次 File Request 额度初始化。
3. FIX-03：本地/S3 下载失败终态。
4. REL-01：隔离安装、升级、实际备份恢复、回滚演练与发布候选验收。

当前仅执行 FIX-01。每包先核对最新 master、相关 PR、工作树和工具链，阅读实现并列出不变量；先复现/分类，再最小修复、目标测试、相关回归/race、差异审查和独立 PR。其他 PR 改变基线时重新核对受影响闭包，不覆盖其工作。

不修改密文格式、历史 ID、权限模型、兼容配置或 Forceu/Gokapi。未知引用、存在性或提交结果均保守保留；不削弱 M3/M4 的锁序、重试和恢复边界，不进行无关架构重构。

用户要求 GitHub Actions 保持关闭；本阶段本地验证与远端代码 review 分别记录，历史绿色 CI 不能代替当前源码验证。不自动合并 PR，不发布 Release/镜像，不进行生产部署；发布必须另行明确批准。

M5 完成须四包各自验收、最新 master 必要完整测试、无未解释回归，以及隔离安装/升级、实际恢复和回滚全部有精确 SHA 的直接证据。FIX-01 或一个 PR 通过不表示 M5 完成。

任务状态见 [看板](M5-TASK-BOARD.md)，验收见 [检查表](M5-ACCEPTANCE.md)。复用 [M3 冻结范围](../security/M3-GOAL.md)、[M4 审计](../security/M4-COMPLETION-AUDIT.md)及 ADR-022/024/025，不重写其历史测试结果与限制。

中英文 Web 与站点定制开发计划
============================

范围
----

保留现有 Go 模板和 JavaScript。内置 en、zh-CN 两份词典，复用 config.json，
新增可选 UI 配置。站点名称、标题及管理员撰写的页脚按原文展示。
配置启动读取，修改后重启。系统页面、交互和组件随语言切换。
面向桌面浏览器；不做手机适配，常见手机/平板 User-Agent 在主服务和安装入口
返回 403 与电脑访问提示。此策略不替代认证，无法识别伪装成桌面浏览器的设备。

分批交付
--------

1. 语言与展示基础：配置校验、重新配置保留 UI、请求级语言、公共标题/页脚/
   导航/切换入口、登录页、基础回归检查。
2. 公共页面：下载、密码、错误、改密、文件收集上传及动态提示；加密和传输保护。
3. 管理页面：上传管理、文件收集管理、用户、API 密钥、状态与前端组件。
4. 安装向导与验收：向导/校验文案，补齐词条，实际浏览器及本地全量验证。

每批创建中等规模 PR。开始和合并前 fetch origin；远端变化时同步并重新验证。
本地验证通过、自查差异后合并，再从最新 master 开始下一批。
不依赖 GitHub Actions：在本机 WSL Go 环境生成、构建、执行 Go 测试；
Node 内置测试检查语言切换/插值；最终使用桌面浏览器验证真实页面。

验收要求
--------

* 老配置正常使用；UI 缺省英文，Title 缺省 PublicName，默认署名保留。
* 重新配置不丢失 UI；非法配置明确报错。
* lang 参数、Cookie、默认语言按顺序选择；中英并发不串语言。
* Title、分享预览、页脚、公共和管理页面、安装向导接入语言。
* 切换保留其他查询参数和 hash，重新 GET，传输/加密/保存中阻止切换。
* 已知错误按现有错误码翻译；未知/第三方/日志详情保留原文。
* 配置不泄露认证数据；HTML/脚本按上下文转义；词典不包含 HTML。
* 组件资源随项目提供；生成的静态资源与源码一致。

交付记录
--------

开发开始于 c657d54（master）。本文件随每批交付记录 PR 与实际本地验证结果。

第一批本地验证（WSL Ubuntu 24.04 / Go 1.26.10、Windows Node / Chrome）：

* ``go generate ./...``：通过；JS/CSS 源码内容参与资源版本号，避免同版本开发包缓存旧脚本。
* ``go test ./internal/i18n ./internal/models ./internal/configuration ./internal/configuration/setup ./internal/webserver ./internal/webserver/browseraccess -tags=test,noaws -count=1``：通过。
* ``go test -race ./internal/i18n ./internal/models ./internal/webserver/browseraccess ./internal/webserver -tags=test,noaws -run 'TestUI|TestSelectLanguage|TestDictionaries|TestDesktopPolicy' -count=1``：通过。
* ``node --test build/tests/ui.test.cjs``：4 项通过。
* 合成数据、本机 53844 端口，Chrome 1440×1000：登录页中英切换、Title、页脚、Cookie、
  查询参数/hash、POST 不重放、处理中阻止切换、独立浏览器语言、手机 UA 403 均通过，0 console error。

以上是第一批范围的验证；其余页面和全仓验证仍属于后续批次。

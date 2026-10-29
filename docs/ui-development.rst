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
3. 管理页面分两批中等规模 PR：上传/文件收集/组件；用户/API 密钥/状态/加密设置。
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

第一批已合并：`PR #23 <https://github.com/Activer007/Gokapi/pull/23>`_，
主线合并提交 ``9cc33a1``。

第二批本地验证（相同工具链，基于最新 ``9cc33a1``）：

* 公共下载/密码/错误/忘记密码/改密/文件收集上传及动态提示接入中英文。
* 显式语言选择在重定向前保存；解密后的浏览器标题使用定制 Title；流写入结束前保持切换保护。
* ``go generate ./...`` 与生产 noaws 二进制构建通过。
* ``go test ./internal/i18n ./internal/webserver ./internal/webserver/authentication/oauth -tags=test,noaws -count=1``：最终完整相关包通过。
* ``go test -race ./internal/i18n ./internal/webserver -tags=test,noaws -run 'TestUI|TestPreference|TestSelect|TestDictionaries' -count=1``：通过。
* ``node --test build/tests/ui.test.cjs build/tests/public-upload.test.cjs``：6 项通过。
* 桌面 Chrome 1440×1000：普通下载实际内容、中文文件名上传、密码页双语、错误页、
  忘记密码、错误重定向保留语言、待传文件阻止切换、上传后恢复切换通过，0 page error。
* 加密页面实际加载 WASM、识别 hash 中的文件名，验证自定义 Title；以模拟延迟流验证
  写入完成前禁止语言切换、结束后恢复。这不是加密文件字节正确性测试。
* 浏览器重复验证最初因复用夹具消耗名额、成功弹窗阻挡按钮而失败；每次使用独立合成数据目录后通过。

第二批已合并：`PR #25 <https://github.com/Activer007/Gokapi/pull/25>`_，
主线合并提交 ``0f618ed``。

第三批第一部分（基于最新 ``0f618ed``）：

* 上传管理、文件收集、子模板和新增行的动态按钮接入词典。
* DataTables、Dropzone、Flatpickr 使用本地词条；日期存储/排序仍使用时间戳。
* 后台 API 失败统一解析：翻译已知错误码，保留未知错误详情，正文只读取一次。
* ``go generate ./...``、生产 noaws 构建通过。
* ``go test ./internal/i18n ./internal/webserver -tags=test,noaws -count=1``：通过。
* ``go test -race ./internal/i18n ./internal/webserver -tags=test,noaws -run 'TestUI|TestPreference|TestSelect|TestDictionaries' -count=1``：通过，包含最终的单文件/多文件子模板分支。
* ``node --test build/tests/ui.test.cjs build/tests/public-upload.test.cjs build/tests/admin-ui.test.cjs``：8 项通过。
* 桌面 Chrome 1600×1100 实际登录、中文搜索/编辑日历、实际上传与动态按钮、
  Dropzone 超限中文提示、中文文件收集创建/编辑、英文切换重载均通过，0 page error。
* 初次浏览器复验发现关闭按钮文字与可访问名称不一致，修正 aria-label 后通过。
* 合并前同步主线 ``c3244f0``（PR24 重复文件准入修复），合并后的代码提交为 ``8a9ca5a``。
* WSL 直接在 Windows/DrvFs 工作区的全量运行，权限与删除时序两个断言失败；
  保留失败记录，未修改断言或业务逻辑来绕过测试。
* 同一 ``8a9ca5a`` 的 Git archive 在 WSL 原生 Linux ``/tmp`` 解压后，
  ``go generate ./...``、``go test ./... -tags=test,noaws -count=1``、上述 UI race 与生产构建全部通过。
  58 个包返回 ok（含一个无匹配测试），12 个包无测试；不将其称为所有构建标签的测试。
  快照 SHA256：``4d652b3304aae0ea1a05f55692cd3918d08ed3487177939fb7d7bec31caff667``。

第三批第一部分已合并：`PR #26 <https://github.com/Activer007/Gokapi/pull/26>`_，
主线合并提交 ``95b139b``。

第三批第二部分（基于最新 ``95b139b``）：

* 用户、API 密钥、状态和加密设置页面及动态消息接入词典。
* 用户名和 API 名称复用已有 base64 请求头协议，正确传输 UTF-8/emoji；
  旧 ASCII 请求保持兼容，拒绝非法 UTF-8 和控制字符。
* 一次性解密密码在继续设置前禁止语言切换，保存失败则解除保护；
  已有密码的保存按钮等待 WASM 加载完成再启用。
* 取消日志删除后重置正确的选择控件；最近登录与密钥未使用状态使用独立译文。
* ``go generate ./...``、生产 noaws 构建、相关 Go 包完整测试与 UI/API race 通过。
* 三组 Node 内置测试共 9 项通过。
* Chrome 1600×1100：中文/emoji 用户创建、重置密码、提升权限、重载；
  中文/emoji API 名称保存重载、权限修改；状态日志过滤、取消删除、中英切换通过。
* 实际 WASM 生成密钥，模拟保存失败和延迟保存，验证保护状态、继续设置后恢复切换、
  已有密钥英文输入分支，0 page error。模拟的 500 响应会产生预期 API 错误日志。
* 初次浏览器脚本复用用户夹具及编辑后失效的文本定位，修正测试夹具/定位后通过；
  加密分支还暴露并修正了加载前启用保存按钮的问题。

* 合并前同步主线 ``79f2577``（PR27 文件所有权转移修复），同步提交 ``51b9afb``。
  其 Git archive 在 WSL 原生 Linux 目录执行生成、全量 ``test,noaws``、UI race、
  生产构建全部通过。快照 SHA256：
  ``c6d83a338560aeb6fdbde9b20b06f0f55fed07c30d3180efea3f1e595772e819``。

第三批第二部分已合并：`PR #28 <https://github.com/Activer007/Gokapi/pull/28>`_，
主线合并提交 ``76a77a9``。

第四批（基于最新 ``76a77a9``）：

* 安装/重新配置向导、验证消息、步骤按钮、维护提示接入同一份中英词典。
  安装入口记住语言选择；重新配置使用现有默认语言和站点标题。
* 切换语言前确认丢弃已填写的值；提交中禁止切换。已知 S3 测试结果附带可翻译键，
  原有结果与未知提供方错误保留，不改变认证和配置提交协议。
* ``go test ./internal/i18n ./internal/configuration/setup -tags=test,noaws -count=1`` 通过。
* 4 组 Node 内置测试共 11 项通过。
* Chrome 1600×1100 / 实际生产构建 / 独立合成配置目录：全新安装默认英文，
  中文欢迎/步骤/按钮，取消丢弃表单，地址/密码校验，实际提交配置并进入登录页通过。
  S3 提示通过模拟测试响应验证，未连接真实云账户。
* 实际重新配置：中文默认和定制标题、英文切换、字段预填、提交后完整保留 UI 对象、
  登录页标题和页脚通过；手机 UA 在安装入口返回 403；两组流程均为 0 page error。
* 初次截图早于模态框动画结束，改为等待动画完成后复拍；没有改动既有布局。

可重复的本地验收
--------------

在原生 Linux 文件系统的源码快照中安装 Go（版本以 go.mod 为准）和 Node.js，运行::

  bash build/test-ui-local.sh

脚本执行资源生成、Node 检查、全量 ``test,noaws`` 与 ``test,awsmock``、
UI 相关 race 测试，以及默认 AWS/noaws 两种生产构建。不会连接真实 S3，
也不依赖 GitHub Actions。WSL 请先把源码复制/解压到 Linux 目录（例如 ``/tmp``），
不要在 ``/mnt/c``、``/mnt/e`` 等 Windows 挂载路径运行全量测试。

浏览器验证使用 Windows Chrome 的桌面视口。Browser plugin 不可用，使用已安装的
Playwright；截图与临时浏览器脚本位于工作区外，合成配置和服务日志不纳入版本控制。

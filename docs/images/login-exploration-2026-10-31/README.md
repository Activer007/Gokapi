# 登录插画精选 / Selected login artwork

2026-10-31。用户筛选后保留 9 张原稿，已移除被删除图片的索引及提示词记录，不恢复删除项。

本组与原有 4 张一起组成 13 张登录图库。内置 image_gen 生成，完整提示词见 [prompts.json](prompts.json)。原稿保留；运行资源为 1280×1056 WebP（quality 82）。原始图为艺术表达，不是工程结构图。

| 原稿 | 实际尺寸 | 运行文件 | WebP 字节 |
| --- | --- | --- | ---: |
| [vault-dial](01-mechanical/vault-dial.png) | 1381×1139 | vault-dial-v1.webp | 120156 |
| [planetary-gears](01-mechanical/planetary-gears.png) | 1381×1139 | planetary-gears-v1.webp | 119302 |
| [vault-linkage](01-mechanical/vault-linkage.png) | 1381×1139 | vault-linkage-v1.webp | 133632 |
| [dial-edge-macro](02-macro/dial-edge-macro.png) | 1382×1138 | dial-edge-macro-v1.webp | 102862 |
| [cipher-rotor-macro](02-macro/cipher-rotor-macro.png) | 1382×1138 | cipher-rotor-macro-v1.webp | 98042 |
| [locking-cam-macro](02-macro/locking-cam-macro.png) | 1382×1138 | locking-cam-macro-v1.webp | 148814 |
| [encrypted-ribbon](03-art-exploration/encrypted-ribbon.png) | 1426×1103 | encrypted-ribbon-v1.webp | 74216 |
| [amber-lightfold](03-art-exploration/amber-lightfold.png) | 1382×1138 | amber-lightfold-v1.webp | 129538 |
| [photon-stream](04-abstract-tech/photon-stream.png) | 1427×1102 | photon-stream-v1.webp | 127250 |

## 本轮验证

原生 Linux 生成资源及构建通过，Go 登录目录测试和 11 项 Node 检查通过。13 个图片资源均返回 200、正确 WebP MIME，响应与文件一致。真实浏览器抽查旋转盘、轮盘微距和光纤流；中英切换、错误登录保留图、正确登录及退出通过。按用户要求不逐图做浏览器检查。

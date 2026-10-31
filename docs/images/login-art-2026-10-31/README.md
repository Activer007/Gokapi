# 登录插画素材 / Login artwork

日期：2026-10-31。状态：原始设计素材，尚未接入运行页面。

用户选定 [第三张登录页](../login-concepts-2026-10-31/login-03.png)。本组围绕其中右侧的深灰建筑背景、蓝色阳极氧化金属和拉丝银色链接生成；所有插画均不含文字或表单，可用于以后每次打开登录页随机选择一张。

工具：内置 `image_gen`，四次独立生成，原始 PNG 直接复制，未裁剪/重绘/压缩。参考图为已选第三张登录效果图；`shared-folder.png` 另附原品牌封面 `../supashare-cover.png`。

| 文件 | 内容 | 实际尺寸 | 原始字节数 |
| --- | --- | --- | --- |
| linked-pair.png | 双链接，首选默认图 | 1381×1139 | 2223230 |
| linked-chain.png | 三段链条 | 1382×1138 | 2254781 |
| shared-folder.png | 文件夹与链接 | 1381×1139 | 2321193 |
| document-flow.png | 文件与链接 | 1381×1139 | 2224917 |

生成时请求 1280×1056，工具实际返回上表尺寸；不将请求尺寸当作实际尺寸。比例接近 40:33，第二张存在约 1px 差异。用户允许后续裁剪/压缩；接入时从这些原稿导出统一的 1280×1056 WebP（首轮质量 80–85，逐张检查），不覆盖原稿。四张原稿合计约 8.61 MiB，不应直接在登录页全部加载，也不因保存到 docs 而打包入程序。

已逐张查看生成结果，并通过 Pillow 解码检查 RGB、尺寸及文件完整性。未声称已完成压缩质量、浏览器裁剪或页面验收。完整实施计划见 [登录页开发计划](../../login-page-upgrade-plan.md)。

## 生成提示词

### linked-pair.png

```text
Use case: stylized-concept. Production illustration asset for the RIGHT ARTWORK PANEL of SupaShare's selected login UI. Reference image is the chosen full login screen; use ONLY the aesthetic of its blue and silver metallic linked sculpture. Generate only standalone artwork, absolutely NO UI, typography, words, logo, watermark, buttons, frame, footer or form.
Canvas 1280 x 1056 pixels, landscape aspect ratio 40:33, full-bleed opaque background. Charcoal #1f2329 and graphite #252a31 architectural recesses, electric cobalt-blue anodized aluminum and finely brushed satin silver, premium realistic studio CGI. Subtle cool rim light, soft contact shadows, crisp fine materials, restrained reflective highlights, never blown-out. Darker leftmost 12% gently blends into adjacent charcoal form. Central object centered around 55% width, 52% height and kept inside middle 76% to allow safe cover crop. Background has simple stepped architectural planes, no busy pattern. All four assets in this series should have equal visual weight, brightness, palette and camera distance. Sophisticated self-hosted file sharing brand, avoid padlocks/shields/security badges, no neon cyberpunk or purple, no characters. One illustration, not a collage.
Subject variation: TWO physically plausible interlocking thick rounded-rectangle links, one cobalt blue and one brushed silver. Blue link stands diagonally rising to upper right, silver link passes through it and extends toward lower left, a strong balanced three-quarter macro sculpture. Closely match the selected login panel's interlocking-link identity but recompose as an independent complete asset with no cropped primary object. This is the signature image of the series.
```

### linked-chain.png

```text
Use case: stylized-concept. Production illustration asset for the RIGHT ARTWORK PANEL of SupaShare's selected login UI. Reference image is the chosen full login screen; use ONLY the aesthetic of its blue and silver metallic linked sculpture. Generate only standalone artwork, absolutely NO UI, typography, words, logo, watermark, buttons, frame, footer or form.
Canvas 1280 x 1056 pixels, landscape aspect ratio 40:33, full-bleed opaque background. Charcoal #1f2329 and graphite #252a31 architectural recesses, electric cobalt-blue anodized aluminum and finely brushed satin silver, premium realistic studio CGI. Subtle cool rim light, soft contact shadows, crisp fine materials, restrained reflective highlights, never blown-out. Darker leftmost 12% gently blends into adjacent charcoal form. Central object centered around 55% width, 52% height and kept inside middle 76% to allow safe cover crop. Background has simple stepped architectural planes, no busy pattern. All four assets in this series should have equal visual weight, brightness, palette and camera distance. Sophisticated self-hosted file sharing brand, avoid padlocks/shields/security badges, no neon cyberpunk or purple, no characters. One illustration, not a collage.
Subject variation: THREE physically plausible interlocking rounded rectangular links forming a gentle rising diagonal chain, bottom-left to upper-right: cobalt-blue link in foreground, brushed-silver middle link, cobalt-blue smaller rear link. The sculptural chain rests across three broad graphite architectural steps, suggesting smooth file handoff. All three links visible as complete shapes, generous margins. Same premium close-up material style, centered visual weight and calm charcoal background as the reference. Absolutely no folder or text in this variation.
```

### shared-folder.png

```text
Use case: stylized-concept. Production illustration asset for the RIGHT ARTWORK PANEL of SupaShare's selected login UI. Reference image is the chosen full login screen; use ONLY the aesthetic of its blue and silver metallic linked sculpture. Generate only standalone artwork, absolutely NO UI, typography, words, logo, watermark, buttons, frame, footer or form.
Canvas 1280 x 1056 pixels, landscape aspect ratio 40:33, full-bleed opaque background. Charcoal #1f2329 and graphite #252a31 architectural recesses, electric cobalt-blue anodized aluminum and finely brushed satin silver, premium realistic studio CGI. Subtle cool rim light, soft contact shadows, crisp fine materials, restrained reflective highlights, never blown-out. Darker leftmost 12% gently blends into adjacent charcoal form. Central object centered around 55% width, 52% height and kept inside middle 76% to allow safe cover crop. Background has simple stepped architectural planes, no busy pattern. All four assets in this series should have equal visual weight, brightness, palette and camera distance. Sophisticated self-hosted file sharing brand, avoid padlocks/shields/security badges, no neon cyberpunk or purple, no characters. One illustration, not a collage.
Subject variation: one elegantly sculpted cobalt-blue anodized metal file folder holding exactly two pale silver document sheets with subtly folded upper corners, no writing or marks on sheets. Beside its lower-right corner a pair of small physically interlocking brushed-silver and cobalt rounded-rectangle links leans against it. Folder dominates the composition, links are secondary. The entire sculpture rests on a graphite ledge inside a simple architectural recess. Sophisticated restrained industrial material rather than cartoon toy, same close-up cinematic studio quality as the selected login image. Both folder and links fully visible, no text.
```

### document-flow.png

```text
Use case: stylized-concept. Production illustration asset for the RIGHT ARTWORK PANEL of SupaShare's selected login UI. Reference image is the chosen full login screen; use ONLY the aesthetic of its blue and silver metallic linked sculpture. Generate only standalone artwork, absolutely NO UI, typography, words, logo, watermark, buttons, frame, footer or form.
Canvas 1280 x 1056 pixels, landscape aspect ratio 40:33, full-bleed opaque background. Charcoal #1f2329 and graphite #252a31 architectural recesses, electric cobalt-blue anodized aluminum and finely brushed satin silver, premium realistic studio CGI. Subtle cool rim light, soft contact shadows, crisp fine materials, restrained reflective highlights, never blown-out. Darker leftmost 12% gently blends into adjacent charcoal form. Central object centered around 55% width, 52% height and kept inside middle 76% to allow safe cover crop. Background has simple stepped architectural planes, no busy pattern. All four assets in this series should have equal visual weight, brightness, palette and camera distance. Sophisticated self-hosted file sharing brand, avoid padlocks/shields/security badges, no neon cyberpunk or purple, no characters. One illustration, not a collage.
Subject variation: a sculptural fan of exactly three thin document slabs, one cobalt blue at the back and two satin silver in front, each simple rectangular with one carefully folded upper-right corner and ABSOLUTELY blank without writing, set diagonally into a graphite architectural base. In foreground a medium-sized linked pair of cobalt and silver rounded-rectangle metal rings rests horizontally, establishing the connection/file-transfer theme. The document fan leans gently upper-left and links balance lower-right. View from a slightly elevated three-quarter perspective. Sculptural rather than icon collage, all elements convincingly share one studio scene and lighting, realistic brushed aluminum microtexture. No folder in this variation, no screens or envelopes, no text. Entire object group has safe margins and equal visual weight to reference.
```

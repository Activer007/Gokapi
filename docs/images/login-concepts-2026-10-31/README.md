# SupaShare login design references — 2026-10-31

Generated using built-in image_gen, not application screenshots. Discussion references only; no UI code changed. Display order maps to login-01.png, login-02.png, login-03.png.

Source: origin/master e95ff55, source version 2.15.1. Existing login template unchanged since 2.12 delivery. References: ../ui-upgrade/login-zh.jpg (historical real 2.11 login capture) and ../supashare-cover.png (brand artwork). Version in mockups reflects current source, not a published release. Preserve configuration-based branding when implementing; demonstration footer/copy is not a new business default.

Original untracked assets preserved. No application regression suite rerun for image-only exploration.

## Selected direction

On 2026-10-31 the user selected **Image 3 / login-03.png** and authorized all three reference images to be committed to Git. Preserve Image 1 and Image 2 as alternatives; they are not implementation targets. The selected direction uses a left login form and right blue/silver illustration inside one horizontal panel. The new [artwork series](../login-art-2026-10-31/README.md) supplies four standalone images. See the [development plan](../../login-page-upgrade-plan.md) for scope and acceptance. No login implementation is included in this asset/design delivery.

## Image 1

File: login-01.png

### Prompt

```text
Use case: ui-mockup. Create realistic, production-quality UI design with clear hierarchy, strong typography, intentional imagery and purposeful spacing. ONE independent desktop login screen for SupaShare, concept name Quiet Blue Aperture (do not print concept name). Target dimensions 1586 x 992 landscape, matching reference login screenshot. Input 1 is actual product login screenshot: preserve its focused login controls and charcoal/blue design system, not its empty background. Input 2 is brand art: use blue folder and brushed-metal link materials as visual inspiration, not a pasted cover.
Design: centered opaque charcoal login panel, about 460px wide, excellent readable Chinese typography. Background adds a huge extremely restrained soft electric-blue elliptical pool of light behind panel with subtle architectural charcoal layers curving at far sides. Small sculptural blue folder with metallic link above the login heading within the panel, modest and sophisticated. Background stays quiet around text. Keep top-left SupaShare wordmark and top-right 中文 / English. Thin header divider. Bottom-centered footer.
Exact visible Chinese text: 登录; 登录以管理文件和分享。; 用户名; 密码; 登录; 忘记密码. Empty inputs with persistent labels. Single bright blue full-width primary login button. Footer: 团队文件分享 / Team file sharing; SupaShare v2.15.1.
Palette #1f2329 background, #252a31 surface, #1677ff accents, off-white text and readable muted grey. System sans serif compatible with Microsoft YaHei. Strong contrast, 44px controls, 8–12px corner radii, realistic app design rather than marketing. No extra cards, badges, metrics, features, registration, social login, remember-me, side navigation, security claims, browser chrome, mock device, neon cyberpunk, purple, or watermarks. One screen only. Use spacing and hierarchy over decorative boxes. Artwork is decorative and does not obscure form. Current date 2026-10-31, no date display needed.
```

## Image 2

File: login-02.png

### Prompt

```text
Use case: ui-mockup. Create realistic production-quality UI with clear hierarchy, strong typography, intentional imagery, purposeful spacing. ONE independent SupaShare desktop login screen, concept name Brand Studio (do not print name). Target 1586 x 992 landscape. Input 1 actual current login screenshot supplies exact core controls and system typography; Input 2 brand artwork supplies blue folder and brushed metallic chain style.
Redesign within existing charcoal/electric-blue product system: spacious asymmetric two-column composition, not a centered card. Left 55% holds beautiful blue folder with pale documents and metallic link on a dark studio surface, moderate scale ~340px tall, with quiet blue rim lighting; above it editorial headline 文件有序，分享从容。 and small descriptive copy 自托管文件分享与收集. Right 40% holds a clean unboxed login form on flat charcoal, separated from left by whitespace and a very subtle vertical divider. Align form to visual center; 400px form width, labels and text left aligned. Background is matte charcoal, soft localized atmospheric lighting behind illustration only, no dramatic waves. Top brand SupaShare left; 中文 and English right. Single subtle fullwidth top divider. Footer centered at bottom.
Login copy verbatim: 登录; 登录以管理文件和分享。; 用户名; 密码; 登录; 忘记密码. Inputs empty, persistent labels, 44px min height, fullwidth saturated blue button. Footer: 团队文件分享 / Team file sharing; SupaShare v2.15.1.
Palette #1f2329 #252a31 #1677ff #f4f6fa and readable muted grey, system sans serif / Microsoft YaHei, crisp professional proportions. One primary action, generous whitespace, no feature cards, no registration/social login/remember-me, no dashboard/sidebar, no badges/metrics/security guarantees, no purple, no browser frame, no watermark. No fake product functions. Artwork is decorative, form very usable. Exactly one screen, no collage, no option number. Date anchor 2026-10-31, no visible date necessary.
```

## Image 3

File: login-03.png

### Prompt

```text
Use case: ui-mockup. Create realistic production-quality UI with clear hierarchy, strong typography, intentional imagery and purposeful spacing. ONE independent SupaShare desktop login screen. Concept name Connected Portal (do not print name). Target dimensions 1586 x 992 landscape. Input 1 current actual login screenshot gives the core controls/type system. Input 2 brand cover gives blue and brushed metallic link materials.
Make this distinctly different from a centered floating card or left-illustration/right-form design: one wide horizontal integrated panel centered on a quiet matte charcoal canvas, approximately x260..1325 y230..755. LEFT half of integrated panel contains focused login form on opaque #252a31 with no nested card. RIGHT half contains a sophisticated close-up sculpture of TWO interlocking softly rounded rectangular links, one cobalt-blue anodized metal and one brushed silver, emerging from dark architectural recess with subtle blue rim light. Macro crop aesthetically composed within right half, restrained dramatic perspective, evokes file sharing and connection. Very faint thin embossed link contour in upper-right background, almost invisible. Do not use folder illustration here: reinterpret metallic link part of brand. Flat clean form region, no texture under form. Small headline above integrated panel: 连接文件，连接协作。 restrained size and left aligned with panel. User should notice login action first.
Header top-left SupaShare, top-right 中文 / English, subtle divider. Footer bottom center. Form exact text: 登录; 登录以管理文件和分享。; 用户名; 密码; 登录; 忘记密码. Empty inputs, persistent labels, 44px controls, single blue fullwidth button, supporting forgot password below. Footer 团队文件分享 / Team file sharing and SupaShare v2.15.1.
Preserve charcoal #1f2329, blue #1677ff, off-white #f4f6fa, readable muted grey, system sans / Microsoft YaHei, clean borders, 10px outer radius. Keep language/footer and all controls fully visible. No new features, registration/social login/remember-me, security promises, badges, metrics, sidebar, extra decorative cards, purple, cyberpunk neon, lock/shield imagery, browser/device chrome, watermarks, or numerical option labels. One image, one screen, not collage. Date anchor 2026-10-31, no date needed.
```

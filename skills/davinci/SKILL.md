---
name: davinci
description: >
  用 davinci（本地的图层化图片编辑器）做封面和配图、管理素材库。凡是要做 / 改
  小红书封面、B 站封面、视频号竖图、公众号头图、海报、缩略图、带文字的配图，
  或者要往素材库里上传人像 / 背景 / APP Logo、从素材库找图、收藏或参考模板库里的封面、导出 PNG/JPG 时使用。
  用户发来 davinci.localhost 的链接（如 http://davinci.localhost/editor/p_xxx?b=yyy）或 davinci://p_xxx?b=yyy
  链接时也一律用本 skill：链接就是一个 davinci 项目（b= 是画板），用 CLI 处理，不要用浏览器或 WebFetch 去读。
  Use when the user asks to create or edit a cover image, thumbnail, poster or
  social-media graphic, to upload or search design assets (cut-out portraits,
  backgrounds, app logos), or to export a design as an image, and whenever the
  user shares a davinci.localhost or davinci:// link. Everything goes
  through the `davinci` CLI: every layer and every edit is a command, and the
  same design is live in the web editor.
---

# davinci：做封面、管素材

davinci 是一个 AI Native 的本地图层化图片编辑器：**每一个图层、每一个编辑操作都是一条命令，
你用 `davinci` 命令行就能完成整张设计、改任何一个图层**。网页编辑器里打开的是同一份文档、同一个撤销栈，
你的每一步都实时出现在用户眼前。

给 davinci 本身做介绍图、写介绍文案时，卖点是 **AI Native：每个图层都能用 AI 直接操作**；
不要写成「AI 出初稿、人来精修」这类说法。

只需要会调 CLI。服务进程、数据存在哪、端口，CLI 都会自己处理：服务没开会自动拉起，
不要自己去 `serve`、不要传 `--data`。

**用户发来 `http://davinci.localhost/editor/p_xxx?b=yyy` 这样的链接**：`p_xxx` 是项目 id，
`b=` 后面是用户正在看的那个画板（画板 id 去掉了 `board_` 前缀，`-b` 直接认）。直接交给 CLI，
**每条命令都带上 `-p p_xxx -b yyy`**（`davinci -p p_xxx -b yyy layers`、`davinci -p p_xxx -b yyy render -o cur.png` 看图），
按用户的要求改；链接没带 `b=` 时先 `davinci -p p_xxx boards` 看有几个画板。
不要用浏览器打开或抓取这个页面。桌面版的 `davinci://p_xxx?b=yyy` 链接一样处理。

## 0. 先确认能用

```bash
davinci projects          # 能列出项目（可能是空的）就说明一切就绪
```

如果提示 `command not found`：用的是 davinci 桌面版，就请用户在 App 菜单里点「davinci → 安装命令行工具…」
（Windows 在「帮助」菜单里，装好后要新开终端）；
用的是代码仓库，就请用户在仓库里执行一次 `./start.sh install`（会把 `davinci` 装到 PATH 上）。装好再继续。
不要自己猜路径去找二进制。

所有命令都可以加 `--json` 拿结构化输出；`-p <项目id或名称>` 指定项目，不加就是**最近
更新的那个项目**——同时有多个项目时，一律显式带 `-p`。

## 1. 做一张封面：固定流程

1. **建项目**：`davinci new "标题" --preset xhs-3-4`（尺寸见下表）。记下返回的项目 id / 名称。
   同一个视频要做几版时只建这一次，其余版本是画板，见下面「一个视频做几版」。
2. **找素材**（需要人像、Logo、背景时）：`davinci lib ls -c 人像 -q "指向左上 微笑"`，
   挑中后 `davinci lib show <id>` 读它的说明和标注——人像的标注会告诉你人物该放哪、
   标题该放哪（`placement`、`p_strategy`），照着摆。
3. **一次排完**：把整张图写成一个 JSON 命令数组，`davinci -p <项目> exec --file cover.json`。
   一批命令 = 一个撤销步骤；任何一条失败整批回滚，改好那条重发即可。
4. **看结果**：`davinci -p <项目> render -o cover.png`，然后**用你能看图的方式打开这张 PNG 看一眼**。
   文字压到人脸、字太小、对比度不够、元素出界——看到就改（见第 3 节），再 render 再看。
   没看过渲染结果之前，不要说"做好了"。
5. **把编辑器地址给用户**：`davinci open <项目>` 打印（并尝试打开）编辑器地址，把地址原样给用户；
   要指到某个画板就加 `-b <画板>`，地址里会带上 `?b=<画板id>`，打开就停在那个画板。
   本机的编辑器地址是 `http://davinci.localhost/editor/<项目id>`（不带端口）；别自己拼 `127.0.0.1:7789`，
   以 CLI 打印的为准（没配本地域名的机器上它会给出能用的地址）。

模板：[examples/xhs-cover.json](examples/xhs-cover.json)。排版原则：[references/cover-design.md](references/cover-design.md)。

### 一个视频做几版：同一个项目，一版一个画板

用户要给**同一个视频 / 同一个选题**出几版不同的封面（"出三版看看""再来一版""换个风格试试"），
所有版本都放在**同一个项目**里，一版一个画板——**不要每版 `new` 一个项目**。
人在编辑器底部那排画板里来回切着对比、挑选，首页也不会多出一堆同名的半成品。

```bash
davinci new "视频标题" --preset xhs-3-4               # 只建一次
davinci -p P board rename 1 "A-大字报"                 # 第一版画在画板 1 上
davinci -p P -b "A-大字报" exec --file a.json
davinci -p P board add --name "B-人像左"              # 另起一版：同尺寸空白画板，接在后面
davinci -p P -b "B-人像左" exec --file b.json
davinci -p P board dup "A-大字报" --name "C-大字报黄底"  # 在某一版上做变体：复制再改
davinci -p P -b "C-大字报黄底" exec --file c.json
davinci -p P -b "B-人像左" render -o b.png             # 每版各自 render、看图、改到满意
```

- 画板名写出这一版的特点，带上 A/B/C 方便人说"要 B"：`A-大字报`、`B-人像左`，别用默认的"画板 2"。
- 每条命令都显式带 `-b`，免得排到别的版本上。
- 交付时给**一个**编辑器地址，列出每个画板是哪一版、区别在哪；第一个画板是项目缩略图，
  人挑定之后可以 `board mv <画板> 1` 把选中的那版挪到最前。
- 用户明确指向某个已有项目（发了它的链接、说"在原来那个上再来一版"）：在**那个项目**里加画板，
  不要另起项目，也不要覆盖已有的版本。
- 用户发起一次新的做封面请求：新建项目，即使已经有同主题的旧项目，也不要自作主张往旧项目里加。
- 某一版想照模板库里的封面做：不要 `tpl use`（它会另起一个项目）。在本项目 `board add` 一个
  和模板同尺寸的画板，用 `davinci -p <模板> doc --json` 读出模板的背景（`canvas.backgroundImage`）
  和图层，再在新画板上 `setBackground` + `insertLayers`（跨项目粘贴图层），然后改字、换图。

### 画布预设

| preset | 尺寸 | 用途 |
|---|---|---|
| `xhs-3-4` | 1242×1656 | 小红书封面（默认首选） |
| `xhs-1-1` | 1080×1080 | 小红书方图 |
| `bili-16-9` | 1920×1080 | B 站 / YouTube 封面 |
| `wx-9-16` | 1080×1920 | 视频号 / 抖音竖图 |
| `mp-900-383` | 900×383 | 公众号头图 |

也可以 `davinci new "名字" --w 1200 --h 800` 自定义。

## 2. 素材库

所有项目共用，按分类存放（默认：人像、背景、APP Logo、装饰元素，可自建）。

```bash
davinci lib cats                                  # 分类和数量
davinci lib ls -c 人像 -q "惊讶 指向"               # 多个词都要命中；名称/标签命中的排前面
davinci lib show <素材id>                          # 名称、尺寸、图片 url、说明、标注 JSON
davinci lib insert <素材id> -p <项目>              # 直接插入：等比缩进画布 80%，人像贴底居中
davinci lib insert <素材id> -p <项目> --h 1100 --x 560   # 自己定高度和位置（宽度按比例）
```

排版时更常用的做法是：`lib show --json` 拿到 `url`、`width`、`height`，按比例算好尺寸，
写进同一批命令的 `addImage` 里，和文字一起一次排完。

**上传素材**（用户给了图片、或让你把一批文件入库）：

```bash
davinci lib add ~/Downloads/me-*.png -c 人像 --trim            # 抠好的人像：--trim 裁掉四周透明边
davinci lib add logo.png -c "APP Logo" --name 飞书 --tags 办公,协作
davinci lib add p.png -c 人像 --desc "单手指向左上，适合推荐类封面" --tags 指向左上,微笑
davinci lib category add 表情包                                # 没有合适的分类就新建
davinci lib update <id> --tags 指向,推荐 --name "新名字"         # 改信息
```

- 抠图（透明底 PNG）一律加 `--trim`，否则人物会带着一大圈透明画布，放进封面显得很小。
- 名称、标签、说明写中文、写"这张图是什么、适合干什么"——后面搜索全靠它们。
- `--source <唯一标识>` 可让重复导入只更新不重复（批量同步外部素材时用）。
- 删除（`lib rm`）、删分类属于破坏性操作，用户明确要求才做。

## 2.5 模板库（照着做）

模板库是用户收藏的封面。每个模板都是完整的分层项目（和作品一样），只是归在模板库里，
带标签和备注。用户说"照着我收藏的那种做""用模板库里的大字报风"时用它。

```bash
davinci tpl tags                       # 有哪些标签
davinci tpl ls --tag 大字 -q 人物       # 找模板（名称/标签/备注/链接都会搜）
davinci tpl show <模板id>              # 读备注：版式、配色、适合什么选题、文字块位置和字数
davinci tpl get <模板id>               # 渲染成 PNG 并打印路径——用你能看图的方式打开看一眼
davinci tpl use <模板id> "新作品名"     # 整份复制成新作品，然后用 -p <新作品id> 改字、换图
davinci tpl add <文件|URL> --tags a,b --note "喜欢它哪里"   # 用户让你收藏时
davinci tpl move <作品>                 # 用户让你把某个作品放进模板库时
```

- **不要直接改模板本身**（除非用户明确要求）：先 `tpl use` 复制出新作品，再在新作品上改。
- 不带 `-p` 时默认的项目只会是作品，不会是模板。
- 这个视频已经有项目、只是要照模板再出一版：不要 `tpl use`，在原项目里加画板（见第 1 节「一个视频做几版」）。

## 3. 改已有的设计

先看现状，再动手：

```bash
davinci -p <项目> layers            # 图层列表：id、名称、位置、尺寸（最上层在前）
davinci -p <项目> doc               # 整份文档 JSON（样式细节都在里面）
```

常用修改（都是 `exec` 的一条命令，也有对应的子命令）：

```bash
davinci -p P exec '{"type":"moveLayer","id":"主标题","x":96,"y":180}'
davinci -p P exec '{"type":"updateLayer","id":"主标题","props":{"fontSize":150,"fill":"#ffffff"}}'
davinci -p P exec '{"type":"applyTextPreset","id":"副标题","preset":"yellow-box"}'
davinci -p P exec '{"type":"setText","id":"主标题","text":"第一行\n第二行"}'
davinci -p P exec '{"type":"bringToFront","id":"主标题"}'
davinci -p P undo                   # 撤销上一步（和人的操作共用一个栈）
```

- 图层用 **id 或名称** 指代，名称在文档内唯一——给图层起有意义的名字（"主标题""人像"），后面好引用。
- 人可能正开着编辑器在改同一个项目。改之前重新 `layers` 看一眼，别拿旧信息覆盖人的修改。
- 完整命令表和参数：`davinci schema`（机器可读，最权威），速查见 [references/commands.md](references/commands.md)。

## 4. 多画板

一个项目可以有多个画板（像稿定设计底部那一排）：同一个视频的几版封面、同一张封面的几种尺寸、
设计稿和参考原图放一起。
**第一个画板是项目的门面**——首页缩略图和尺寸都取它。

```bash
davinci -p P boards                         # 列画板：序号（从 1 开始）、名称、尺寸、图层数，* 是当前画板
davinci -p P board add --name 原图 --w 1080 --h 1440   # 新建（默认同当前尺寸，放在当前画板后面，并切过去）
davinci -p P board dup 1                    # 复制画板 1
davinci -p P board rename 2 竖版            # 改名；board mv <画板> <位置> 调顺序；board rm <画板> 删除
davinci -p P board use 2                    # 切换当前画板
davinci -p P -b 2 layers                    # 任何命令加 -b（序号 / 名称 / id）就作用于那个画板
davinci -p P -b 竖版 exec --file cover.json
davinci -p P -b 2 render -o v.png
```

- 不带 `-b` 的命令作用于**当前画板**（人正在看的那个）。项目有多个画板时，一律显式带 `-b`。
- 命令 JSON 里也可以直接写 `"board": 2`（单条命令或一个批次都行）。
- `getProject` 读整个项目（全部画板）；`getDocument` / `davinci layers` 只看一个画板。

## 4. 规则和坑

- 单位是画布像素；`x/y` 是图层**左上角**；命令都用**绝对值**（`moveLayer` 给目标坐标，不给位移）。
- 文字图层的 `width` 是**折行宽度**，高度由内容决定；想让字变大改 `fontSize`，不要拉高度。
- 文字换行用 `\n`。中文标题 2 行以内、每行不超过 8 个字最好认。
- `addImage` 的 `url` 用素材库给的 `/assets/...`，或本机路径 / http 链接（CLI 会自动上传）。
- 字体：`davinci fonts` 看可用字体族，写进 `style.fontFamily`；不确定就用默认字体。
- 文字样式预设：`davinci text presets`（黄底黑字、红底白字、黑描边、荧光笔……）。
- 导出：`render -o x.png --scale 2` 出两倍图；`--format jpg` 出 JPG。
- 不要删除或覆盖用户已有的项目；给新的视频做图就 `new` 一个，同一个视频的新版本加画板（见第 1 节）。

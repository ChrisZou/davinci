# davinci 命令参考

davinci 里**只有一层命令实现**，它在服务端（`internal/doc`）。
界面上的按钮、拖拽、快捷键，以及 AI 下发的每一条命令，走的是同一个 `Engine.Apply`。
所以这份文档列举的能力 = AI 的能力 = 人的能力，不会少。

每条命令的权威说明由命令注册表自己生成：

```bash
davinci schema                  # 全量 JSON：参数、默认值、枚举、示例
davinci schema | jq '.commands | length'   # → 38
curl -s localhost:7789/api/schema          # 同一个 blob
```

**本文是给人读的地图**，讲清约定和典型用法；逐字段还是以 `davinci schema` 为准
（改代码时那份会自动长出来，这份不会）。

## 目录

- [全局约定](#全局约定)
- [画布与背景](#画布与背景)
- [图层增删](#图层增删)
- [变换与排列](#变换与排列)
- [z-index](#z-index专门一节)
- [多图层：对齐、分布、编组、粘贴](#多图层对齐分布编组粘贴)
- [文字](#文字)
- [图片](#图片)
- [形状](#形状)
- [查询](#查询)
- [历史与导出](#历史与导出)
- [素材库](#素材库davinci-lib)
- [CLI → 命令对照](#cli--命令对照)

## 全局约定

**1. 图层用 `id` 或 `name` 定位。** 每个命令的第一个参数都叫 `id`
（`listLayers` 返回的 `id` 字段），名字在文档内唯一——新建时若重名会自动加后缀
（`标题 2`）。用名字可读性更好，也不怕图层被删了 id 失效。

**2. 一律绝对量。** `moveLayer` 给的是左上角的绝对 `x/y`，不是偏移量；
`setZIndex` 给的是绝对层级，不是"上移一层"（上移有 `bringForward`）。
绝对量的好处：命令之间互相独立、可乱序执行、可安全重放。

**3. 单位是画布像素。** `x/y/width/height` 都是文档坐标，不是屏幕坐标，
也不受当前缩放影响。`x/y` 永远是**左上角**，`rotation` 顺时针角度。

**4. 参数名写错会报错，不会被忽略。**

```
$ davinci exec '{"type":"export","format":"png","scale":2}'
export: unknown parameter "scale" — this command takes format, multiplier
```

AI 猜错参数名是常事（`scale` vs `multiplier`），静默忽略会得到一张 1× 图还让人觉得成功了。
所以命令层**严格校验**：只接受 `davinci schema` 里声明的参数。这也意味着
发可选参数时不必犹豫，写全了更保险。

**5. 修改类命令返回结果 + 整份文档。** 不需要再查一次：

```jsonc
{"ok":true,"data":{"id":"t_9ohh1m45r","name":"标题","type":"text"},
 "document":{"canvas":{…},"layers":[…]}}
```

**6. 图层顺序：文档数组是"从底到顶"。** `listLayers` 反过来报（**从最上层到最底层**，
和图层面板一致），而且每行带一个 `index` = 文档数组下标，`0` 是最底层。
所以 `listLayers` 第一行的 `index` 等于 `count-1`。`setZIndex` 收的就是这个 `index`。

## 画布与背景

| 命令 | 参数 | 说明 |
|---|---|---|
| `setCanvasSize` | `width` `height` `preset` | 改画布尺寸，图层不动。给 `preset`（`xhs-3-4` 等）就忽略宽高 |
| `setBackground` | `color` `image` | 纯色或图片背景（图片按 cover 铺满）；`color` 支持 `transparent` |

```bash
davinci canvas size --w 1242 --h 1656
davinci canvas bg --color '#0f0f0f'
davinci canvas bg --image ~/Pictures/bg.jpg
```

```json
{"type":"setCanvasSize","preset":"bili-16-9"}
{"type":"setBackground","color":"#ffffff"}
```

**画布预设**（`davinci canvas presets` 也是同一份）：`xhs-3-4` 1242×1656、
`xhs-1-1` 1080×1080、`bili-16-9` 1920×1080、`wx-9-16` 1080×1920、`mp-900-383` 900×383。
自己量尺寸也完全可以，预设只是省事。

## 图层增删

| 命令 | 关键参数 | 说明 |
|---|---|---|
| `addText` | `text`**必填** `x` `y` `width` `style` `name` | 默认放画布中部，颜色按背景自动选深/浅 |
| `addImage` | `url` \| `path` `x` `y` `width` `height` `name` | 默认按原始尺寸，居中 |
| `addShape` | `kind` `x``y``width``height`**必填** `fill` `stroke` `strokeWidth` `cornerRadius` `opacity` | `kind` ∈ `rect`\|`ellipse`\|`triangle` |
| `duplicateLayer` | `id`**必填** `x` `y` | 副本落在原图层上方，默认略偏移 |
| `removeLayer` | `id`**必填** | 删 |
| `renameLayer` | `id``name`**必填** | 重命名 |

```bash
davinci layer add-text "AI 编程实测" --x 96 --y 420 --w 1050 \
  --size 150 --color '#ffffff' --stroke '#000:8' --bg 'rgba(0,0,0,.5)'
davinci layer add-image ~/Pictures/bg.jpg --x 0 --y 0 --w 1242 --h 1656 --name 背景
davinci layer add-shape rect --x 96 --y 900 --w 900 --h 4 --fill '#ffffff'   # 细矩形就是分割线
davinci layer cp 标题
davinci layer rename t_9ohh1m45r 主标题
davinci layer rm 副标题
```

`path` 支持 `~`。地址栏能访问的图片可以直接给 `url`，但**本机文件必须走 `path`**：
服务端读取文件、存成资产，文档里记的是 `/assets/` URL（这样换台机器、换个目录也不丢图）。

`addText` 为空内容时默认名字取内容前若干字；`width` 是**折行宽度**，不给也是 80% 画布。

## 变换与排列

| 命令 | 参数 | 说明 |
|---|---|---|
| `moveLayer` | `id` `x` `y` | 左上角绝对坐标 |
| `resizeLayer` | `id` `width` `height` | 文字图层只改折行宽度（高度随内容长） |
| `fitText` | `id` `align` | 文字图层的宽度收到正好包住文字；字在画布上不动、换行不变，可顺便改对齐（left/center/right） |
| `rotateLayer` | `id` `rotation` | 顺时针，可负 |
| `updateLayer` | `id` `props{}` | 通用补丁：位置/尺寸/旋转/不透明度/显隐/锁定 + 该类型支持的样式键 |
| `setOpacity` | `id` `opacity` | `0-1` |
| `setVisible` | `id` `visible` | 布尔 |
| `setLocked` | `id` `locked` | 锁定后拖不动，也改名不了 |
| `alignLayer` | `id` `h` `v` | `h` ∈ `left`\|`center`\|`right`，`v` ∈ `top`\|`middle`\|`bottom` |
| `fitToCanvas` | `id` `mode` | `cover` 铺满裁切 / `contain` 装入留边 / `width` / `height`，完事居中 |
| `reorderLayers` | `ids[]` | 整栈按给定顺序摆放，**第一个最底层**；没列出的保持原相对顺序排它们上面 |

```bash
davinci layer hide 副标题           # 再执行一次同一个命令就翻回来（开关式）
davinci layer order 背景 副标题 标题                    # 从底到顶
```

`updateLayer` 是"不确定用哪条命令时"的兜底，`props` 里放什么改什么，
一个键都不认得就整体报错：

```json
{"type":"updateLayer","id":"标题","props":{"x":96,"y":420,"opacity":0.9,"fontSize":150}}
```

> `davinci layer hide` / `lock` 是**开关式命令**而不是布尔旗标：再执行一次就翻回去。
> 需要确定地把图层设为某个状态时，走 `updateLayer` 或用 `exec`：
> `davinci exec '{"type":"setVisible","id":"副标题","visible":false}'`

### z-index（专门一节）

五个命令，语义互不重叠：

| 命令 | 参数 | 语义 |
|---|---|---|
| `setZIndex` | `id` `index` | 绝对层级，**`0` 最底层**，值就是 `listLayers` 的 `index` |
| `bringForward` | `id` | 上移一层 |
| `sendBackward` | `id` | 下移一层 |
| `bringToFront` | `id` | 置顶 |
| `sendToBack` | `id` | 置底 |

```bash
davinci layer z 人像 front        # 也接受 back | up | down | <数字>
```

**最容易踩的一点**：`setZIndex` 的 `index` 是"从底到顶"的下标，
而 `davinci layers` 打印的是"从顶到底"。要把一个图层放到最顶层，
`index` 应该是 `count-1`，不是 `0`：

```bash
n=$(davinci layers --json | jq '.layers | length')
davinci exec "{\"type\":\"setZIndex\",\"id\":\"标题\",\"index\":$((n-1))}"
# 或者，别算，直接：
davinci layer z 标题 front
```

做整图时我更推荐 `reorderLayers` / `layer order`：**一次把整栈描述清楚**，
比逐个往上挪更不容易错。

## 多图层：对齐、分布、编组、粘贴

| 命令 | 参数 | 说明 |
|---|---|---|
| `alignLayers` | `ids[]` `h` `v` `to` | 多个图层对齐。`to`=`selection`（默认）对齐到它们整体的外框，`canvas` 一起对齐到画布 |
| `distributeLayers` | `ids[]`（≥3）`axis` | `h`/`v` 等间距分布：首尾不动，中间的让间隙相等 |
| `groupLayers` | `ids[]`（≥2）`name` | 编成一组，占最上面那个成员的层级；返回 `{id,name}` |
| `ungroupLayers` | `id` | 解散编组，成员按所见位置/缩放/旋转回到画布；返回 `{ids,names}` |
| `insertLayers` | `layers[]` `offset` `x` `y` | 插入完整的 Layer JSON（`getLayer`/`getDocument` 的格式），自动换新 id、重名加后缀、放最上层 |

对齐、分布按**外框**算（旋转过的图层用旋转后的外接矩形），和人眼看到的一致。

编组后的图层 `type` 是 `group`，成员在 `children` 里（坐标相对组的左上角，尺寸不含组的缩放）。
组可以整体移动、缩放、旋转、改不透明度；要改组里某个图层，先 `ungroupLayers`。

```json
{"type":"alignLayers","ids":["标题","副标题"],"h":"left"}
{"type":"distributeLayers","ids":["卡片1","卡片2","卡片3"],"axis":"h"}
{"type":"groupLayers","ids":["标题","副标题"],"name":"标题组"}
```

编辑器里 ⌘C 复制到系统剪贴板的就是 `{"kind":"davinci/layers","layers":[…]}`，
把其中的 `layers` 交给 `insertLayers` 就是一次粘贴——所以复制的图层可以跨项目、跨页面刷新粘贴。

## 文字

| 命令 | 参数 | 说明 |
|---|---|---|
| `setText` | `id` `text`**必填** | 改内容，支持 `\n` |
| `setTextStyle` | `id` `style{}` **必填** | 只传要改的键，其余不动 |
| `applyTextPreset` | `id` `preset`**必填** | 一键套封面常用样式 |

`style` 支持的键：

| 键 | 取值 |
|---|---|
| `fontFamily` | 字体族名，`davinci fonts` 看可选值 |
| `fontSize` | 像素 |
| `fontWeight` | `100`–`900`，或 `bold`/`normal` |
| `fontStyle` | `normal`\|`italic` |
| `fill`（别名 `color`） | CSS 颜色 |
| `textAlign` | `left`\|`center`\|`right` |
| `lineHeight` | 倍数 |
| `charSpacing` | 像素字距 |
| `shadow` | `"颜色:模糊:偏移x:偏移y"`，如 `"rgba(0,0,0,.6):18:4:4"` |
| `textBackgroundColor` | 文字底色（封面标题高频） |
| `stroke` | `"颜色[:粗细]"`，如 `"#000:8"` |
| `paintFirst` | 先画描边再画填充——**描边在文字下面**，粗描边必开 |
| `underline` / `linethrough` | 布尔 |
| `padding` | 文字文本框内边距 |
| `skew` | 倾斜角度，正值向右倒（只歪竖笔，像斜体） |
| `skewY` | 纵向斜切角度，正值右端抬高：整行斜向上走，竖笔仍竖直（PS / 稿定的「斜切」）；改它时文字中心不动 |

```bash
davinci text set 标题 "AI 编程实测（2026）"
davinci text style 标题 --size 150 --color '#fff' --bg '#e5322d' \
  --shadow 'rgba(0,0,0,.6):18:4:4' --stroke '#000:8' --weight 700
davinci text preset 标题 yellow-box
```

**文字预设**（`davinci text presets`，`listTextPresets` 同源）：
`plain-white`、`yellow-box`、`red-box`、`outline-black`、`soft-shadow`、`subtitle`、`marker`。
拿不准参数时的快车道：先 `preset`，再单独调 `--size`。

## 图片

| 命令 | 参数 | 说明 |
|---|---|---|
| `setImageProps` | `id` `flipX` `flipY` `cornerRadius` `filters{}` `stroke` `strokeWidth` `shadow` | 翻转、圆角、滤镜、描边、投影。描边沿图片不透明的边缘画一圈（抠过背景的人像 = 贴纸式白边，`"stroke":"#fff","strokeWidth":16`），`"none"` 去掉，不改变图层的框。投影是 `"颜色:模糊:X:Y"`（画布像素，不随图片旋转），`none` 去掉；透明底的图（抠过背景的人像）投影沿主体轮廓 |
| `replaceImage` | `id` `url`\|`path` | 换图，**保留尺寸和位置**（裁剪会被清掉） |
| `removeBackground` | `id` `model` \| `restore` | 去除背景：服务端用 rembg + BiRefNet 抠图（`general` 通用 / `portrait` 人像），换上透明底的抠图，图层位置、大小、裁剪都不变。总是从原图抠，换模型重抠不会越抠越少；`restore:true` 换回原图。同一张图的结果有缓存。需要本机装 rembg（或用 `DAVINCI_REMBG` 指定） |
| `cropImage` | `id` `x` `y` `width` `height` \| `reset` | 只显示原图里的一个矩形区域，单位是**原图像素**；留下的像素在画布上位置不变。`reset:true` 恢复完整图片 |

`filters` 的键与范围：

| 键 | 范围 | 键 | 范围 |
|---|---|---|---|
| `brightness` | `-1`…`1` | `grayscale` | `0`\|`1` |
| `contrast` | `-1`…`1` | `sepia` | `0`\|`1` |
| `saturation` | `-1`…`1` | | |
| `blur` | `0`…`1` | | |

```bash
davinci image set 背景 --radius 24 --blur .08 --brightness .06
davinci image replace 人像 ~/Pictures/new.jpg
```

```json
{"type":"setImageProps","id":"背景",
 "cornerRadius":24,
 "filters":{"brightness":0.06,"contrast":0.1,"saturation":-0.15,"blur":0.08}}
```

```json
{"type":"cropImage","id":"人像","x":400,"y":0,"width":1600,"height":1600}
```

裁剪存在文档的 `image.crop` 里（`{x,y,width,height}`，原图像素），非破坏性：随时 `reset` 回原图。
图层的 `width`/`height` 始终是画面上显示的大小。

滤镜有亮度/对比度/饱和度/模糊/黑白/复古几种，都是**非破坏性**的：
改了还能改回来，参数不一定，但状态存在文档里。

## 形状

`setShapeProps`：`id` `fill` `stroke` `strokeWidth` `cornerRadius`。

```bash
davinci exec '{"type":"setShapeProps","id":"色块","fill":"#e5322d","cornerRadius":16}'
```

形状只有三种：`rect` `ellipse` `triangle`。**没有 `line`**——想要分割线/细线，
用一个很矮的矩形（`--h 4`）。这是刻意的：线在缩放、旋转和导出时的表现都不如矩形可预期。

## 查询

| 命令 | 返回 |
|---|---|
| `listLayers` | `{count, order:'front to back', layers:LayerRow[]}` |
| `getLayer` | 单个图层的完整 `Layer` + `index` |
| `listFonts` | `{fonts:[字体族名…], count}` |
| `listTextPresets` | `{presets:[{key,name,style}…]}` |
| `listCanvasPresets` | `{presets:[{key,name,width,height}…]}` |
| `getDocument` | 整份文档：`canvas` + `layers` |

查询命令 `mutates:false`，不进历史栈，随便调。

AI 的典型闭环是"改一步、看一眼"：

```bash
davinci layers                       # 现在有哪些、什么顺序
davinci layer show 标题 --json       # 这一层的准确位置尺寸
davinci exec '{"type":"moveLayer","id":"标题","x":96,"y":470}'
```

`show` 的默认输出是给人看的表格，`--json` 才是给程序用的。
同理 `layers` 的表格只留关键列，`--json` 给全字段（含 `preview`）。

## 历史与导出

| 命令 | 参数 | 说明 |
|---|---|---|
| `undo` / `redo` | 无 | **和人的 ⌘Z 共用一个栈**（50 步） |
| `export` | `format` `multiplier` `quality` `transparent` `ids[]` | `png`\|`jpeg`\|`webp`，倍率 `0.05`–`8`；`quality` 0–1（jpeg/webp）；`transparent` 不画背景；`ids` 只导出这些图层并裁到它们的外框 |
| `batch` | `commands[]` | 多条命令 = 一个撤销步骤；**任何一条失败，整批回滚**，文档回到批次之前 |

```bash
davinci render -o out.png --scale 2      # 等价 export + 存盘
davinci render --format jpg --scale 1
davinci undo && davinci redo
```

`batch` 不在 `davinci schema` 的命令表里——它由传输层提供。
发 HTTP 时给它一个数组或 `{"commands":[…]}`，发 CLI 时……不，CLI 没有列数组的语法，
用 `davinci exec --file cmds.json`（文件里是数组）：

```bash
davinci exec --file cover.json      # [{"type":"setCanvasSize",…},…]
```

为什么值得用：一批命令**只落一个撤销步骤、页面只重绘一次**。做一整个封面就用它。

## 素材库（`davinci lib`）

素材库是所有项目共用的图片架子（人像、背景、APP Logo、装饰元素，可以自建分类）。
它不是编辑命令，而是 CLI 直接调 HTTP API；插入时才变成一条普通的 `addImage`，
所以照样能撤销。

```bash
davinci lib cats                              # 分类及数量
davinci lib ls -c 人像 -q "指向左上 微笑"       # 关键词按空格拆开，都要命中（名称/标签/说明）
davinci lib show lib_xxx                      # 完整说明 + 标注 JSON（适合怎么 P、放哪个区域）
davinci lib insert lib_xxx                    # 插入当前项目：等比缩进画布 80%，水平居中；人像贴底，其他垂直居中
davinci lib insert lib_xxx --h 1100 --x 560   # 自己定尺寸和位置
davinci lib add ~/cutouts/*.png -c 人像 --trim # --trim 裁掉 PNG 四周的透明边
davinci lib add logo.png -c "APP Logo" --name 飞书 --tags 办公,协作
davinci lib update lib_xxx --tags 指向,推荐    # 改名称 / 分类 / 标签 / 说明
davinci lib category add 表情包               # 新建分类（rename / rm 同理，rm 要求分类已清空）
```

导入的封面人像带有标注：`show` 里的「适合怎么P」和 `placement.best_subject_regions`
告诉你人物该放哪、标题该放哪，先读再摆。

## 模板库（`davinci tpl`）

模板就是普通项目（分层、可编辑、可导出），只是归在「模板库」而不是「作品」里，带标签、
备注（喜欢它哪里）和来源链接，用来照着做新作品。编辑模板本身和编辑作品一样：`-p <模板id>`
加任意命令。不带 `-p` 时默认的「最近项目」只在作品里找，不会落到模板上。

```bash
davinci tpl ls                                # 全部模板
davinci tpl ls --tag 大字 -q 人物              # 按标签 + 关键词（名称/标签/备注/链接）
davinci tpl tags                              # 用过的标签及数量
davinci tpl show p_xxx                        # 备注、标签、来源、画板数
davinci tpl get p_xxx [-o cover.png]          # 渲染第 1 个画板成 PNG，打印路径（拿来看图）
davinci tpl use p_xxx "新封面"                 # 整份复制成新作品（全部画板和图层），在副本上改
davinci tpl add cover.jpg --tags 大字 --note "标题压满上半屏" --link <原帖>   # 一张图收藏成模板
davinci tpl move p_xxx                        # 作品移进模板库；--to design 移回作品
davinci tpl update p_xxx --note "人物压字"     # 改名称 / 标签 / 备注 / 链接
davinci projects --templates                  # 同样列出模板（--all 作品和模板都列）
```

删除模板用 `davinci projects rm`，和删作品一样。

## CLI → 命令对照

CLI 只是 cobra 薄壳，`exec` 是万能兜底——任何命令都有 CLI 哈希，但不一定都有顺手子命令。
**新能力总是先有命令，再考虑加子命令。**

| CLI | 等价命令 |
|---|---|
| `davinci doc` | `getDocument` |
| `davinci canvas size --w --h` | `setCanvasSize` |
| `davinci canvas bg --color\|--image` | `setBackground` |
| `davinci layer add-text\|add-image\|add-shape` | `addText`\|`addImage`\|`addShape` |
| `davinci layer mv` | `moveLayer` |
| `davinci layer size` | `resizeLayer` |
| `davinci layer rotate` | `rotateLayer` |
| `davinci layer update --x --y --rotate --opacity --name` | `updateLayer` |
| `davinci layer align --h --v` | `alignLayer` |
| `davinci layer fit [cover\|contain]` | `fitToCanvas` |
| `davinci layer rm\|cp\|rename` | `removeLayer`\|`duplicateLayer`\|`renameLayer` |
| `davinci layer hide`（开关）| `setVisible` |
| `davinci layer lock`（开关）| `setLocked` |
| `davinci layer z <n>\|front\|back\|up\|down` | `setZIndex`\|`bringToFront`\|`sendToBack`\|`bringForward`\|`sendBackward` |
| `davinci layer order a b c` | `reorderLayers` |
| `davinci layers` / `layer show` | `listLayers` / `getLayer` |
| `davinci text set` / `text style` / `text preset` | `setText` / `setTextStyle` / `applyTextPreset` |
| `davinci image set` / `image replace` / `image rmbg` | `setImageProps` / `replaceImage` / `removeBackground` |
| `davinci fonts` / `font add` | `listFonts` / `POST /api/fonts` |
| `davinci render` | `export` + 存盘 |
| `davinci undo` / `redo` | `undo` / `redo` |
| `davinci watch` | 订阅文档变更（不是命令） |
| `davinci exec '<JSON>'` / `--file x.json` | 原始命令 / `batch` |
| `davinci schema` | 从缓存里取命令表 |

只有两个 CLI 没有对应命令，因为它们不和画布打交道：

```bash
davinci projects          # 项目列表
davinci new "封面" --preset xhs-3-4
davinci open <id|name>    # 浏览器里打开
davinci doc export-doc -o a.json / import-doc a.json   # 整份文档进出
```

### `watch`

```bash
davinci watch           # 另一个终端；人一拖动，这里就打印变更
```

供 AI 观察"人又在手动改什么"。注意要有项目（`--project`）。

## 常见组合

**小红书封面**（`davinci.exec --file`，一批完成、一个撤销步骤）：

```json
[
  {"type":"setCanvasSize","preset":"xhs-3-4"},
  {"type":"setBackground","color":"#0d0d0f"},
  {"type":"addImage","path":"~/Pictures/bg.jpg","x":0,"y":0,"width":1242,"height":1656,"name":"背景"},
  {"type":"setImageProps","id":"背景","filters":{"brightness":-0.15,"blur":0.04}},
  {"type":"addShape","kind":"rect","x":96,"y":1180,"width":1050,"height":320,
   "fill":"#e5322d","cornerRadius":24,"opacity":0.92,"name":"色块"},
  {"type":"addText","text":"AI 编程实测","x":132,"y":1224,"width":980,"name":"主标题",
   "style":{"fontSize":150,"fontWeight":700,"fill":"#ffffff","stroke":"#000000:8","paintFirst":true}},
  {"type":"addText","text":"10 个工具，一周省 20 小时","x":132,"y":1450,"width":980,"name":"副标题",
   "style":{"fontSize":56,"fill":"rgba(255,255,255,.92)"}},
  {"type":"reorderLayers","ids":["背景","色块","主标题","副标题"]}
]
```

> 注意 `name` 是显式给的。不给的**自动名只取前 12 个字加省略号**
> （`addText` 用 `firstLine(text,12)`，图片用文件名），之后再按这个名字引用图层会找不到。
> 凡是打算回头操作的图层，创建时就命名。

**换掉一张图的配色再导出**：

```bash
davinci image set 背景 --radius 0 --brightness .1 --contrast .12 --saturation -.2
davinci render -o out.png --scale 2
```

# davinci 命令速查

权威来源永远是 `davinci schema`（执行器自己上报的完整参数表，带类型、默认值和枚举）。
这里是速查。`*` = 必填。图层参数 `id` 可以是图层 id，也可以是图层名。

## 怎么发命令

```bash
davinci -p <项目> exec '<一条命令 JSON>'
davinci -p <项目> exec '[<命令>, <命令>, …]'     # 数组 = 一批 = 一个撤销步骤，失败整批回滚
davinci -p <项目> exec --file cover.json          # 从文件读（长的一批用这个）
```

返回的 `data` 是命令的结果（新图层的 id 等），`document` 是改完后的整份文档。

## 画布

| 命令 | 参数 | 说明 |
|---|---|---|
| `setCanvasSize` | `width` `height` 或 `preset` | 改尺寸，图层不动 |
| `setBackground` | `color` 或 `image` | 纯色（`transparent` 为透明）或背景图（cover 铺满） |

## 画板

任何命令（或一个批次）都可以带顶层 `"board"`：画板 id、名称或从 1 开始的序号，先切到那个画板再执行。

| 命令 | 参数 | 说明 |
|---|---|---|
| `listBoards` | | 全部画板 |
| `getProject` | | 整个项目（全部画板的文档） |
| `selectBoard` | `id*` | 切换当前画板 |
| `addBoard` | `name` `width` `height` `preset` `background` `index` | 新建并切过去 |
| `duplicateBoard` | `id` `name` | 复制（默认当前画板） |
| `removeBoard` | `id*` | 至少保留一个 |
| `renameBoard` | `id* name*` | |
| `moveBoard` | `id* index*` | 第一个画板是项目缩略图 |

## 添加

| 命令 | 参数 | 说明 |
|---|---|---|
| `addText` | `text*` `x` `y` `width` `style{}` `name` | `width` 是折行宽度；`style` 键见下 |
| `addImage` | `url` 或 `path`，`x` `y` `width` `height` `name` | 只给宽或高时按原图比例 |
| `addShape` | `kind`(rect/ellipse/triangle/line) `x* y* width* height*` `fill` `stroke` `strokeWidth` `cornerRadius` `opacity` `name` | 矩形/椭圆/三角 |
| `addShape` 直线 | `kind:"line"` `x* y* width*`（长度）`stroke`（颜色）`strokeWidth`（粗细）`lineStyle`(solid/dashed) `arrow`(none/end/both) | 线画在框的中线上；斜线用 `rotateLayer` 转 |
| `duplicateLayer` | `id*` `x` `y` | 副本放在原图层上方 |
| `insertLayers` | `layers*[]` `offset` `x` `y` | 插入完整图层 JSON（跨项目复制） |

## 位置、尺寸、层级

| 命令 | 参数 | 说明 |
|---|---|---|
| `moveLayer` | `id* x* y*` | 绝对坐标（左上角） |
| `resizeLayer` | `id*` `width` `height` | 文字只改折行宽度；只给一边则等比 |
| `rotateLayer` | `id* rotation*` | 顺时针角度 |
| `alignLayer` | `id*` `h`(left/center/right) `v`(top/middle/bottom) | 对齐到画布 |
| `alignLayers` | `ids*[]` `h` `v` `to`(selection/canvas) | 多个图层互相对齐 |
| `distributeLayers` | `ids*[]`（≥3） `axis*`(h/v) | 等间距分布 |
| `fitToCanvas` | `id*` `mode`(cover/contain/width/height) | cover 铺满、contain 完整放入 |
| `bringToFront` / `sendToBack` / `bringForward` / `sendBackward` | `id*` | 层级 |
| `setZIndex` | `id* index*` | 0 = 最底层 |
| `reorderLayers` | `ids*[]` | 从底到顶整栈重排 |
| `groupLayers` / `ungroupLayers` | `ids*[]` `name` / `id*` | 编组 / 解散 |

## 通用修改

| 命令 | 参数 | 说明 |
|---|---|---|
| `updateLayer` | `id* props*{}` | 位置/尺寸/旋转/不透明度/显隐/锁定 + 样式键，一次改多个 |
| `setOpacity` / `setVisible` / `setLocked` | `id*` + 值 | |
| `renameLayer` | `id* name*` | 名称唯一 |
| `removeLayer` | `id*` | |

## 文字

| 命令 | 参数 | 说明 |
|---|---|---|
| `setText` | `id* text*` | 换行用 `\n` |
| `setTextStyle` | `id* style*{}` | 只传要改的键 |
| `applyTextPreset` | `id* preset*` | 预设见 `davinci text presets` |

`style` 的键：`fontFamily` `fontSize` `fontWeight`(100–900) `fontStyle` `fill`（别名 `color`）
`textAlign`(left/center/right) `lineHeight` `charSpacing` `padding`
`textBackgroundColor`（文字底色）`stroke`（`"#000:8"` = 颜色:粗细）`paintFirst`（描边压在字下面，粗描边必开）
`shadow`（`"rgba(0,0,0,.5):18:4:6"` = 颜色:模糊:x:y）`underline` `linethrough`。
斜体：`fontStyle: "italic"`。
变形（梯形，右端收窄的透视标题）：`warp: "trapezoid"`，`warpAmount` 梯形强度 -100~100（常用 15–35），
`warpBias` 相对高度 -100~100（-100 下沿平直、上沿往右压低；100 上沿平直；0 上下各收一半）；`warp: "none"` 取消。
另有 `skew` 字形倾斜角度（只歪竖笔）、`skewY` 纵向斜切角度（正值右端抬高，整行斜向上走、竖笔仍竖直）、
`stretch` 横向拉伸倍数（0.85 压窄）。整行连字带竖笔一起斜着走用 `rotateLayer`；只想让行往右上爬用 `skewY`。

预设：`plain-white` 白字、`yellow-box` 黄底黑字、`red-box` 红底白字、`outline-black` 白字黑描边、
`soft-shadow` 柔和投影、`subtitle` 小字副标题、`marker` 荧光笔。

## 图片和形状

| 命令 | 参数 | 说明 |
|---|---|---|
| `setImageProps` | `id*` `flipX` `flipY` `cornerRadius` `filters{}` `stroke` `strokeWidth` `shadow` | 滤镜：brightness/contrast/saturation(-1..1)、blur(0..1)、grayscale/sepia(0或1)；投影 `"rgba(0,0,0,0.35):40:0:16"`（颜色:模糊:X:Y，`none` 去掉），抠过背景的图投影沿主体轮廓；人像贴纸白边：`"stroke":"#ffffff","strokeWidth":16`（`none` 去掉） |
| `cropImage` | `id*` `x y width height`（原图像素）或 `reset:true` | 裁剪 |
| `replaceImage` | `id*` `url` 或 `path` | 换图，保留尺寸位置 |
| `removeBackground` | `id*` `model`（general / portrait）或 `restore:true` | 去除背景（rembg + BiRefNet），图层位置大小不变；人物用 portrait；`restore` 换回原图。CLI：`davinci image rmbg <图层> [--portrait]` |
| `setShapeProps` | `id*` `fill` `stroke` `strokeWidth` `cornerRadius` `lineStyle` `arrow` | 直线的颜色是 stroke、粗细是 strokeWidth |

## 读取

`getDocument`、`listLayers`、`getLayer{id}`、`listFonts`、`listTextPresets`、`listCanvasPresets`——
一般直接用 `davinci doc` / `davinci layers` / `davinci fonts` / `davinci text presets` / `davinci canvas presets`。

## 历史与导出

- `undo` / `redo`：和人的 ⌘Z 共用一个栈（`davinci undo`）。
- 导出用 `davinci render -o out.png [--scale 2] [--format jpg|webp]`。

## 项目

```bash
davinci projects                         # 列表
davinci new "名字" --preset xhs-3-4       # 新建（--json 返回 {project:{id,…}}）
davinci open <项目>                       # 编辑器地址 http://davinci.localhost/editor/<id>（交给人精修）
davinci doc -p <项目>                     # 整份文档 JSON
davinci watch -p <项目>                   # 实时打印人在编辑器里的改动
```

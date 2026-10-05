# davinci 架构

davinci 是一个本地图片编辑器：人在浏览器里拖，AI 通过命令行/HTTP 改同一份文档。
这份文档解释它为什么长这样，以及改哪里不会弄坏它。

一句话概括：**文档只在服务端改；画布只是视图；浏览器和服务端用同一个渲染器。**

```
        ┌────────── AI ──────────┐              ┌──────── 人 ────────┐
        │ davinci CLI / HTTP API   │              │ 编辑器页面          │
        │                          │              │ React + CanvasKit  │
        └────────────┬────────────┘              └─────────┬──────────┘
                     │ HTTP 命令                     命令 ↑ │ ↓ 新文档
                     ▼                                     │ WebSocket
        ┌───────────────────────────────────────────────────────────────┐
        │ Go 服务（Echo + SQLite）                                        │
        │  internal/doc：文档模型 + 52 条命令 + 撤销历史（每项目一个 Engine）│
        │  改完 → 落库 → 推给所有打开这个项目的页面                         │
        └───────────────────────────┬───────────────────────────────────┘
                                    │ stdin/stdout JSON（量文字、导出、缩略图）
                                    ▼
                      ┌──────────────────────────────┐
                      │ Node 渲染进程（renderer.cjs） │
                      │ web/src/render —— CanvasKit   │ ← 浏览器画布用的是同一份代码
                      └──────────────────────────────┘
```

## 命令只在服务端执行

`internal/doc` 是唯一能改文档的地方。每条命令是一次 `define(Spec{...})` 注册
（类型、说明、参数、`Run`），`Engine.Apply(raw)` 负责：

- 归一化（顶层数组 = 一批）、**严格校验参数名**（未知参数直接报错——AI 猜参数名是常态，
  静默忽略会得到"成功了但没生效"，报错只要一个来回就能修好）；
- 顶层 `board` 参数切换作用的画板；
- 一批命令要么全成要么全不成，且是**一个撤销步骤**；
- 改完后重新量那些排版相关属性变了的文字图层（高度存在文档里）。

界面按钮、画布上的拖拽、快捷键、AI 的命令，最后都是同一个 `Apply`。所以：

1. 界面能做的，AI 一定能做——不存在"只有人有"的功能。
2. 人和 AI 共用**同一个历史栈**（服务端每项目 50 步）：人按 ⌘Z 能撤回 AI 刚才那一批，
   `davinci exec '{"type":"undo"}'` 也能撤回人的拖拽。

命令说明（`GET /api/schema` = `davinci schema`）由同一个注册表生成，加一条命令，
执行、校验、文档一起长出来，不存在文档漂移。

## 画布是视图

编辑器页面（`web/src/editor/Editor.ts`）从不改文档，它只做三件事：

- **画**：用 CanvasKit（WebGL）把当前画板画出来，外加 2D 叠加层画选框、手柄、吸附线、间距标注、框选；
- **把手势变成命令**：拖动 → `moveLayer`，拉手柄 → `updateLayer`，转 → `rotateLayer`，
  打字 → `setText`。手势进行中用本地"覆盖值"（ghost）实时显示，松手时发出命令，
  等服务端结果回来再撤掉覆盖——所以不会闪回原位；
- **收新文档**：服务端每次改动都推 `doc`，页面换上新文档重画。

滑块也一样：拖动时本地预览，松手才发一条命令（一个撤销步骤）。

## 一个渲染器，两处运行

`web/src/render/` 是唯一的绘制实现（文档 → 像素）：

| 文件 | 内容 |
|---|---|
| `geometry.ts` | 图层几何：盒子、旋转、描边计入、斜切。Go 侧有逐行对应的 `internal/doc/geometry.go` |
| `text.ts` | 文字排版：折行、行高、字距、合成粗体/斜体、回退字体 |
| `renderer.ts` | 画图层、背景、滤镜、阴影、描边、梯形变形；`encode` 出图；`measure` 量文字 |
| `node.ts` | Node 入口：服务端的渲染进程 |

浏览器里它画编辑器画布、画板缩略图、导出；服务端的 Node 进程（`internal/render/sidecar.go`
按需拉起，空闲 10 分钟退出）用它量文字高度、出 `render/export`、画项目缩略图。
两边字体都从服务端取（`/api/fonts/face`，按字族/字重/斜体挑文件，TTC 会拆成单个字体），
所以**屏幕上看到的就是导出的**，没有页面开着时 CLI 也能出图。

几何和排版沿用了当年 Fabric.js 的规则（行高 1.13、描边计入盒子、左上角为旋转中心……），
老文档打开后位置分毫不差。

## WebSocket 协议（/ws?project=…）

```
server → page   {"type":"hello","origin":"c7","document":…,"revision":n,"history":{canUndo,canRedo}}
page → server   {"type":"command","reqId":"r1","command":{…}}
server → page   {"type":"doc","document":…,"revision":n,"origin":"c7","command":{…},"history":{…}}
server → page   {"type":"result","reqId":"r1","ok":true,"data":…,"document":…,"revision":n,"history":{…}}
```

`origin` 是连接的名字（CLI/HTTP 为空）。页面据此区分"我自己的改动"和"别人的改动"，
后者会在画布上方提示"命令行刚改了……"并给出撤回按钮。`davinci watch` 也是这个流的听众。

## 文档模型

Go 的 `internal/doc/model.go` 与 TS 的 `web/src/types.ts` 描述同一份 JSON：

```ts
Project { version: 2, active, boards: Board[] }
Board   { id, name, canvas: {width, height, background, backgroundImage?}, layers: Layer[] }
Layer   { id, name, type: 'text'|'image'|'shape'|'group', x, y, width, height,
          rotation, opacity, visible, locked, text?, style?, image?, shape?, children? }
```

- **`layers` 从底到顶**，`index` = 数组下标。列表接口和图层面板反过来报（前→后）。
- `x/y` 是盒子左上角，`rotation` 绕左上角顺时针，单位画布像素。缩放平移只是视图，
  永远不进文档——人放大到 400% 时，AI 的 `moveLayer` 给的还是文档坐标。
- 文字的 `height` 由服务端量好存进文档，列表和对齐直接可用。
- 老的 v1 单页文档读入时自动升级成一个画板。

## 目录

```
internal/doc/        文档模型、几何、命令注册表与 52 条命令、撤销历史
internal/render/     Node 渲染进程的管理（拉起、请求、PNG→JPEG、空闲回收）
internal/server/     Echo 路由、SQLite、每项目 Engine、WS hub、资产、字体（扫描/挑选/拆 TTC）、素材库、模板库
internal/cli/        cobra 薄壳，全部是 HTTP 客户端
web/src/render/      共享渲染器（浏览器 + Node）
web/src/editor/      Editor.ts 画布视图与手势、bridge.ts 会话、ck.ts CanvasKit 加载与出图、keyboard.ts 快捷键
web/src/pages|components/  界面
```

改动指南：

- **加一个编辑能力** → `internal/doc` 里加一条 `define`；界面只描述命令（`session.run({...})`）。
- **加一种图层属性** → `model.go`/`style.go` 管校验和存储，`types.ts` 加类型，`renderer.ts` 画出来。
- **改画法** → 只改 `web/src/render/`，浏览器和服务端同时生效（`./start.sh build` 会重新打包 `renderer.cjs`）。

## 依赖

| 侧 | 依赖 | 为什么 |
|---|---|---|
| Go | echo/v4、gorilla/websocket、modernc.org/sqlite（纯 Go）、cobra | 路由、WS、存储、CLI |
| | golang.org/x/image | JPEG 编码、读图片尺寸 |
| web | canvaskit-wasm | Skia 的 wasm 版，浏览器和 Node 共用的绘制引擎 |
| | react 18 + vite + tailwindcss 4 | 界面 |
| 运行时 | node（服务端出图/量字用） | 找不到时编辑照常，导出和缩略图报错说明原因；可用 `DAVINCI_NODE` 指定 |

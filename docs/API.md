# davinci HTTP API

本地服务，**只绑 `127.0.0.1`**，默认端口 `7789`（被占用时换一个空闲端口，实际地址见数据目录的 `server.json`）。所有响应都是 JSON（导出图片或 ZIP 除外），
失败时统一是 `{"ok": false, "error": "人能看懂的原因"}`。

服务端拥有文档：每条命令都由它执行（`internal/doc`）、落库，再推给开着这个项目的编辑器页面。
出图和量文字用的是和浏览器同一份 CanvasKit 渲染器，跑在服务端拉起的 Node 进程里，
所以没有页面开着也照常工作。

> 命令的完整参数说明不在这份文档里，而在 `/api/schema`：它由服务端的命令注册表生成，
> 是命令表的唯一真相。这份文档讲的是**怎么把命令送进去**。

## 快速上手

```bash
# 起服务（或者让任何一条命令自动拉起它）
./bin/davinci serve

# 建项目
curl -XPOST localhost:7789/api/projects -H 'content-type: application/json' \
  -d '{"name":"封面","preset":"xhs-3-4"}'
# → {"ok":true,"project":{...},"editorURL":"http://127.0.0.1:7789/editor/p_xxx"}

# 发一条命令
curl -XPOST localhost:7789/api/projects/<id>/commands \
  -H 'content-type: application/json' \
  -d '{"type":"addText","text":"AI 编程实测","x":96,"y":420,"style":{"fontSize":150,"fill":"#fff"}}'
# → {"ok":true,"data":{...},"document":{...},"revision":3}

# 把编辑器页面给人看（配了本地域名时是 http://davinci.localhost/editor/<id>，`davinci open` 会自动选）
open http://127.0.0.1:7789/editor/<id>
```

AI 从 `/api/schema` 拿到命令表就能开工，不需要读这份文档：

```bash
curl -s localhost:7789/api/schema | jq '.commands[] | {type, summary}'
```

## 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 服务状态：`{port, dataDir, boot, projects}` |
| GET | `/api/schema` | 命令表（参数、返回值、画布与文字预设、示例） |
| GET | `/api/canvas-presets` | 画布预设 `xhs-3-4`、`bili-16-9`… |
| GET | `/api/fonts` | 字体族（收藏的在前，带 `favorite` / `hidden` / `aliases` 中文名）；加 `?rescan=1` 重新扫系统字体目录 |
| PUT | `/api/fonts/prefs` | 收藏或隐藏字体：`{"family":"…","favorite":true}` / `{"family":"…","hidden":true}`，返回新的列表 |
| POST | `/api/fonts` | 上传 `.ttf`/`.otf`，按 `@font-face` 注入页面 |
| POST | `/api/assets` | 导入图片（multipart `file`，或表单字段 `path` / `url`） |
| GET | `/api/projects` | 项目列表，最近更新的在前。默认只列作品；`?kind=template` 列模板库，`?kind=all` 全部；`?q=`（名称/标签/备注/链接，空格拆词都要命中）`&tag=`（整个标签匹配） |
| POST | `/api/projects` | 新建项目：`{name, preset?, width?, height?, kind?, tags?, note?, link?}` |
| GET | `/api/projects/:id` | 项目详情，含整份文档 |
| PUT | `/api/projects/:id` | 用一整份文档 JSON 覆盖（导入） |
| PATCH | `/api/projects/:id` | 改项目元数据（不碰文档）：`{name, kind, tags, note, link}`；`kind` 改成 `template` / `design` 就是移进模板库 / 移回作品 |
| DELETE | `/api/projects/:id` | 删除项目 |
| GET | `/api/projects/:id/thumbnail` | 编辑器最近一次保存的缩略图（没有时 404）；列表页用 `?rev=<revision>` 做缓存键 |
| POST | `/api/projects/:id/commands` | **AI 的主入口**：执行命令 |
| GET | `/api/projects/:id/layers` | 图层列表（前 → 后） |
| GET | `/api/projects/:id/export.zip` | 所有画板 ZIP，`?format=webp&scale=2`；默认 PNG |
| GET | `/api/projects/:id/export.png` | 导出图片，支持 `scale`、`board`、`quality`、`transparent`、重复的 `ids`；`.jpg` `.jpeg` `.webp` 同理 |
| GET | `/api/library/categories` | 素材库分类及数量 |
| POST | `/api/library/categories` | 新建分类：`{name}` |
| PATCH / DELETE | `/api/library/categories/:id` | 改名 `{name, sort?}` / 删除（分类里还有素材时 409） |
| GET | `/api/library/items` | 素材列表：`?category=<id或名称>&q=<关键词>&limit=&offset=`；`q` 按空格拆词，每个词都要命中名称/标签/说明之一 |
| POST | `/api/library/items` | 加素材：multipart `file` + `category name tags description meta source trim`，或 JSON `{ref, category, …}`（`ref` 是本机路径 / http(s) / `/assets/…`）。同一 `source` 在同一分类只存一份，重复导入会更新；`trim=true` 裁掉 PNG 四周的透明边 |
| GET / PATCH / DELETE | `/api/library/items/:id` | 单条素材（含 `meta` 标注）/ 改 `{name, category, tags, description}` / 删除（设计里已用的图不受影响） |
| GET | `/api/library/items/:id/thumb` | 缩略图 PNG（保留透明），`?w=` 最长边，默认 320 |
| GET | `/api/projects/tags` | 某类项目用过的标签及数量：`?kind=template`（默认）或 `design` |
| POST | `/api/projects/:id/duplicate` | 复制项目（文档 + 缩略图）：`{name?, kind?}`，`kind` 默认 `design`——「用这个模板新建」就是它；同类复制会带上标签和备注 |
| POST | `/api/projects/from-image` | 用一张图新建项目（画布同图片尺寸，长边不超过 3000，整张图是一个图层）：multipart `file` + `name kind tags note link`，或 JSON `{ref, …}`；`kind` 默认 `template` |
| GET | `/assets/*` `/fonts/*` | 资产与用户字体文件 |
| WS | `/ws?project=<id>` | 编辑器页面与服务的双向通道（见下） |

## 执行命令

`POST /api/projects/:id/commands` 接受三种 body：

```jsonc
{"type":"addText","text":"你好"}                    // 单条
{"commands":[{"type":"addText","text":"a"},…]}      // 一批，合成一个 batch
[{"type":"addText","text":"a"},{"type":"addText","text":"b"}]  // 裸数组，同上
```

一批命令走前端的 `batch`：**落成一个撤销步骤、页面只重绘一次**。做一整个封面就发一批。
批里任何一条失败，整批回滚——文档回到发这批之前，改好那条重发即可。

成功返回 `200`：

```jsonc
{
  "ok": true,
  "data": { "id": "t_9ohh1m45r", "name": "你好", "type": "text" },  // 命令自己的返回值
  "document": { "version": 2, "active": "…", "boards": [...] },     // 结果文档，省得再查
  "revision": 3,                                                     // 落库后的版本号
  "history": { "canUndo": true, "canRedo": false }
}
```

失败分两种，看状态码就知道该重试还是该改请求：

- `400` — 命令本身被拒（参数不对、图层不存在、类型不匹配、图片取不到）。`error` 里是原因。
- `503` — 服务端这边的问题，比如出图要用的 Node 渲染进程起不来（没装 node：装上，
  或用 `DAVINCI_NODE` 指定路径）。

命令层的行为约定（`docs/COMMANDS.md` 有逐条参数）：

- 图层用 `id` 或 `name` 定位，`name` 在文档内唯一（重名自动加后缀）。
- 坐标一律**绝对量**：`moveLayer` 给的是左上角绝对 `x/y`，不是偏移量。
- `x/y` 是图层**左上角**，`rotation` 是顺时针角度，单位都是画布像素。
- 每条修改类命令都返回结果文档，AI 不需要再查一次。

## 图层列表

```
GET /api/projects/<id>/layers
→ {"ok":true,"layers":[{"id":"t_1","name":"标题","type":"text","x":96,"y":420,
   "width":1050,"height":180,"rotation":0,"opacity":1,"visible":true,"locked":false,
   "index":2,"preview":"AI 编程实测"}, …]}
```

**顺序是从最上层到最底层**（和图层面板一致）。`index` 是文档数组里的下标，
`0` 是最底层 —— `setZIndex` 收的正是这个值。所以最上层那行的 `index` 等于 `len-1`。

`preview` 是给人看的摘要：文字图层给文字，图片图层给 URL 尾巴，形状图层给填充色。

## 导出

```text
GET /api/projects/<id>/export.png?board=2&scale=2&transparent=true&ids=标题&ids=人像
→ image/png 二进制，指定图层的外框范围
GET /api/projects/<id>/export.jpg?quality=0.8&scale=3
→ image/jpeg 二进制，当前画板
GET /api/projects/<id>/export.zip?format=webp&scale=2&quality=0.8&transparent=true
→ application/zip 二进制，全部画板，每个画板一张图片
```

查询参数：`board` 为画板 id、名称或从 1 开始的序号，缺省当前画板；`scale` 默认 1，`0 < scale <= 8`（渲染器最低 0.05）；`quality` 默认 0.92，范围 0.05–1（仅 JPG/WebP 有效）；`transparent=true` 不画背景色和背景图；`ids` 可重复，按图层 id 或名称筛选并裁到外框。PNG/WebP 支持透明，JPG 将透明区域铺白。未知图层、无效格式和非法数值会返回 400。

`.zip` 自动导出所有画板，`format` 选择 ZIP 内图片格式 `png|jpg|jpeg|webp`（默认 `png`）。图片端点也支持 `allBoards=true`，此时返回的 MIME 同样是 `application/zip`。批量导出与 `board`、`ids` 互斥，按画板顺序导出同一版本，不切换当前画板；名称清理、重名后缀与编辑器一致，详见 [导出命令](COMMANDS.md#历史与导出)。

CLI `davinci render --all-boards -o 目录` 会把同一 ZIP 解包到目录；`-o out.zip` 直接保存 ZIP。`--clipboard` 是 CLI 本机操作，不是服务端剪贴板 API。

命令接口也支持 `{"type":"export","allBoards":true,"format":"png","multiplier":2}`，返回 `{dataURL, format:"zip", files:[...]}`；不指定 `allBoards` 仍返回单画板 `{dataURL, width, height, format}`。完整参数由 `/api/schema` 提供。

服务端和编辑器画布使用同一个渲染器；没有页面开着也能导出。

## 资产

```bash
# 本机文件
curl -XPOST localhost:7789/api/assets -F file=@~/Pictures/bg.jpg
# 远程 URL（服务端去取，页面不跨域）
curl -XPOST localhost:7789/api/assets -F url=https://example.com/a.png
# → {"ok":true,"asset":{"sha":"…","ext":"jpg","mime":"image/jpeg","size":12345,
#                       "url":"/assets/<sha>.jpg"}}
```

命令里的图片参数可以直接给**本机绝对路径**（`addImage.path`、`replaceImage.path`、
`setBackground.image`）：服务端读文件、存成资产，文档里记的是 `/assets/` URL。

## WebSocket `/ws?project=<id>`

编辑器页面用它发命令、收文档。页面这一端的实现见 `web/src/editor/bridge.ts`。

| 方向 | 消息 |
|---|---|
| server → page | `{"type":"hello","origin":"c7","document":…,"revision":n,"history":{…}}` 连上即推当前项目 |
| page → server | `{"type":"command","reqId":"r1","command":{…}}` 页面上的每个操作都是一条命令 |
| server → page | `{"type":"result","reqId":"r1","ok":true,"data":…,"document":…,"revision":n,"history":{…}}` |
| server → page | `{"type":"doc","document":…,"revision":n,"origin":"c7","command":{…},"history":{…}}` 任何来源改了文档都广播给所有页面 |
| page → server | `{"type":"ping"}` → `{"type":"pong"}` |

`origin` 是连接的名字（CLI 和 HTTP 来的改动为空），页面据此分辨"自己的改动"和"命令行的改动"。
命令在服务端按项目串行执行，同项目开几个标签页也不会互相踩；`davinci watch` 就是这个流的听众。

## 端口与数据目录

```bash
davinci serve --data ./data               # 不给 --data 就用仓库里的 data/（桌面版里是 ~/Library/Application Support/davinci）
davinci serve --port 7789 --data ./data   # 指定端口：被占用就报错，不会换
# 或者 DAVINCI_PORT / DAVINCI_DATA
```

不指定端口时，服务先试 7789，被占用就换一个空闲端口，并把实际地址写进数据目录的 `server.json`
（`{"url", "pid", "boot"}`，服务退出时删掉）。一个数据目录同时只能有一个服务（`server.lock`）。
CLI 的其他子命令按 `--server` / `DAVINCI_SERVER` / `DAVINCI_PORT` → `server.json` → 7789 的顺序找服务，
都找不到就自己拉起一个。给人看的编辑器地址（`open`、`new`、`serve --open`）在 `davinci.localhost` 指向
同一个服务时用 `http://davinci.localhost`，否则用 API 地址；`DAVINCI_WEB_URL` 可以直接指定。数据目录里是 `davinci.db`（SQLite）、`assets/`、`fonts/`。
数据目录里若是更早版本建的库（文档格式不同），启动时会明确报错，换个 `--data` 即可。

## 评论改图（本机 AI）

仅本地服务可用。评论调用本机已安装并登录的 Codex / Claude Code；无需在 DaVinci 填 API Key。
浏览器请求须来自本服务、本地 Vite 开发页，或保留同源 Host 的 `http://davinci.localhost` / `http://davinci-dev.localhost` 反向代理，POST 使用 `application/json`。

- `GET /api/ai/providers` → `{providers:[{id,name,available}]}`。`available` 只表示找到可执行文件，登录/额度在实际执行时检查。
- `GET /api/projects/:id/comments` → `{comments:[...]}`，按时间顺序返回最近 200 条。
- `POST /api/projects/:id/comments` → HTTP 202 `{comment:{...}}`。
  请求：`{boardId,layerIds:[],text,provider:"codex"|"claude"}`；评论 1–4000 字。
- `POST /api/projects/:id/comments/:comment/cancel`，JSON body `{}` → `{comment:{...}}`。

评论包含 `id,projectId,boardId,layerIds,target,text,provider,status,reply,createdAt`。
`status`：`running`、`done`、`needs_info`、`failed`、`cancelled`。
同项目同时只处理一条，全服务最多两条；冲突返回 409。单次最多 5 分钟，不自动重试付费请求。
失败后重新提交同一评论即可重试，会使用最新画稿。

AI 收到当前画板预览、图层结构、选中图层、可用编辑命令及该画板最近六条评论。
模型返回的命令经校验后作为一个事务应用，共用原有 WebSocket 更新和撤销历史。
等待 AI 时文档有变化，会拒绝旧结果；取消或失败不应用修改。评论保存在 SQLite 中，独立于撤销栈；
撤销修改不会删除评论，也不会把历史“已修改”改为新的任务状态。

支持文字、配色、布局、形状、滤镜、裁剪及复用已导入图片；不含图像生成、换脸或新素材下载。

## 全局配色

配色独立于项目，存于同一 SQLite 的 `palettes` 表。五个角色为 `bg` 背景、`text` 主文字、
`highlight` 高亮、`text2` 次文字、`accent` 点缀，全部必填，色值采用 `#RRGGBB`，存储时转小写。
名称为 1–120 个字。修改、删除配色不会追溯修改已经应用的画板。

| 方法 | 路径 | 请求 / 返回 |
|---|---|---|
| GET | `/api/palettes` | `{ok,palettes:[...]}`，创建时间倒序 |
| GET | `/api/palettes/:id` | `{ok,palette:{id,name,bg,text,highlight,text2,accent,createdAt,updatedAt}}` |
| POST | `/api/palettes/analyze` | multipart `file` + 可选 `name`；返回 `{ok,palette:{name,bg,text,highlight,text2,accent}}`，不写数据库或资产文件 |
| POST | `/api/palettes` | JSON `{name,bg,text,highlight,text2,accent}`；201 `{ok,palette}` |
| PATCH | `/api/palettes/:id` | JSON `{name?,bg?,text?,highlight?,text2?,accent?}`；必须至少一个字段，200 `{ok,palette}` |
| DELETE | `/api/palettes/:id` | 200 `{ok:true}` |
| POST | `/api/projects/:id/palettes/:palette/apply` | JSON `{board?:string}`，省略 `board` 时用该项目的当前画板；返回标准命令结果 `{ok,data,document,revision,history}` |

解析支持 PNG、JPEG、GIF（首帧）、WebP，最大 20 MB / 2000 万像素。完全透明、损坏或不支持的图片返回 400。
解析使用纯 Go 缩略采样、加权聚类和角色启发式；颜色不足时派生可读的文字色与强调色。
非法名称、缺色、非六位 hex、未知字段返回 400；不存在的配色返回 404。

`/api/schema` 的 `endpoints` 字段由配色路由注册表生成，包含这些 REST 路径、请求和返回说明；
`commands` 中的 `applyPalette` 由文档命令注册表生成。CLI 和编辑器可直接执行：

```json
{"type":"applyPalette","id":"pal_xxx","board":"2"}
```

应用规则、图片保留行为和撤销约定见 [配色命令](COMMANDS.md#配色davinci-palette)。

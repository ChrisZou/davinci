# davinci

一个 **AI Native** 的本地图片编辑器，专注于自媒体封面编辑。

**每一个图层、每一个编辑操作都能直接交给 AI。** 界面上能做的事，背后都是一条命令：AI 通过命令行 / HTTP API
直接调用，不用去操控浏览器。浏览器里打开的是**同一份文档、同一套命令、同一个撤销栈**，AI 的每一步都实时可见。

![每一个图层，AI 都能直接控制](docs/images/every-layer-ai.jpg)

## 快速开始

需要先装好 **Go 1.26+**、**Node 20+** 和 **pnpm**。

**1. 启动服务**

```bash
./start.sh
```

构建并启动服务，浏览器会打开 http://127.0.0.1:7789。

**2. 给 AI Agent 装 skill**

```bash
./start.sh skill
```

这一条命令会自动装好 `davinci` CLI（Agent 靠它干活），再把 skill 装给
Claude Code、Codex、Hermes 这些 AI Agent。

之后直接跟 Agent 说你想要创作的封面或调整就行，比如“做一张小红书封面，标题是……”，“把整体配色换一版”，“把人像换一张”等等，任何一个图层都能这样让 AI 改；它的每一步都会实时出现在浏览器里打开的同一个项目上。

## 功能

<table>
  <thead>
    <tr><th>模块</th><th>功能</th><th>说明</th></tr>
  </thead>
  <tbody>
    <tr><td rowspan="4"><b>画布</b></td><td>尺寸预设</td><td>小红书 3:4 / 1:1、B 站 16:9、视频号 9:16、公众号头图 900×383，也可以自定义宽高</td></tr>
    <tr><td>背景</td><td>纯色（从当前设计里取色推荐），或背景图（等比铺满）</td></tr>
    <tr><td>多画板</td><td>一个项目放多个画板（参考原图 + 设计稿、一稿多版）</td></tr>
    <tr><td>画板条</td><td>看缩略图切换；双击改名、拖动排序；右键复制 / 在后面新建 / 删除；可收起成「‹ 画板 N/M ›」</td></tr>
    <tr><td rowspan="8"><b>文字</b></td><td>字体</td><td>按中英文名搜索；中文字体显示中文名并用字体本身预览；收藏置顶；系统界面字体和其他语种字体默认隐藏；可上传 .ttf / .otf（<code>davinci font add</code>）</td></tr>
    <tr><td>排版</td><td>字号、行高、字距、内边距；左 / 中 / 右对齐</td></tr>
    <tr><td>字形</td><td>字重（细 / 常规 / 粗 / 黑）、斜体、下划线、删除线</td></tr>
    <tr><td>颜色与效果</td><td>文字颜色、文字底色（色块底）、描边（颜色和粗细，可放在字的下面）、投影（颜色、不透明度、模糊、偏移）</td></tr>
    <tr><td>变形</td><td>梯形透视（可调强度和相对高度）、斜切（整行斜向上走、竖笔不歪，可调角度），二选一；倾斜角度、横向拉伸目前在命令里设置</td></tr>
    <tr><td>文字预设</td><td>白色无衬线、黄底黑字、红底白字、黑描边、柔和投影、小字副标题、荧光笔</td></tr>
    <tr><td>画布上编辑</td><td>双击直接改字；拖侧边改换行宽度，拖四角等比缩放字号</td></tr>
    <tr><td>换行与缺字</td><td>中文按字、英文按词自动换行；字体缺字时自动换用中文字体</td></tr>
    <tr><td rowspan="7"><b>图片</b></td><td>添加</td><td>本地上传、拖进画布、⌘V 粘贴截图，或从素材库选</td></tr>
    <tr><td>裁剪</td><td>双击图片裁剪，可恢复完整图片</td></tr>
    <tr><td>换图</td><td>从素材库或本地换一张，新图按原比例放进原来的框里</td></tr>
    <tr><td>去除背景</td><td>rembg + BiRefNet，通用和人像两种模型，可恢复原图</td></tr>
    <tr><td>外观</td><td>圆角；描边（沿图片轮廓描一圈，抠过背景的人像就是贴纸白边）；投影（同样沿轮廓）</td></tr>
    <tr><td>调色</td><td>亮度、对比度、饱和度、模糊（拖动时实时预览），黑白、复古</td></tr>
    <tr><td>翻转与适配</td><td>水平 / 垂直翻转；一键铺满画布或完整放入</td></tr>
    <tr><td rowspan="2"><b>形状与线条</b></td><td>形状</td><td>矩形（可圆角）、椭圆，以及三角形（命令里添加）；填充色、描边颜色和粗细</td></tr>
    <tr><td>直线</td><td>实线 / 虚线；无箭头 / 单向箭头 / 双向箭头；颜色和粗细</td></tr>
    <tr><td rowspan="9"><b>图层与排版</b></td><td>图层面板</td><td>拖动排序、改名、显示 / 隐藏、锁定</td></tr>
    <tr><td>层级</td><td>置底 / 下移 / 上移 / 置顶</td></tr>
    <tr><td>多选与编组</td><td>⇧ 或 ⌘ 点击、框选；编组 / 解散编组</td></tr>
    <tr><td>对齐与分布</td><td>对齐到画布（左 / 中 / 右、上 / 中 / 下）、多个图层互相对齐、水平 / 垂直等间距分布</td></tr>
    <tr><td>吸附</td><td>拖动时吸附画布边缘、中线和其他图层，并标出间距；按住 ⇧ 只沿一个方向拖</td></tr>
    <tr><td>旋转</td><td>旋转手柄，按住 ⇧ 每 15° 一档，接近直角时自动吸附</td></tr>
    <tr><td>精确调整</td><td>位置、尺寸、旋转、不透明度直接输入；方向键微移 1px（⇧ 10px）</td></tr>
    <tr><td>复制粘贴</td><td>复制 / 剪切 / 粘贴（可以跨项目）、创建副本、右键菜单</td></tr>
    <tr><td>撤销重做</td><td>人在页面上的操作和 AI 的命令共用同一个历史</td></tr>
    <tr><td rowspan="3"><b>视图</b></td><td>缩放与平移</td><td>⌘ + 滚轮或双指捏合缩放（以画板为中心）；滚轮或按住空格拖动平移；⌘0 适应画布、⌘1 原始大小</td></tr>
    <tr><td>超出画布的部分</td><td>默认不显示，选中图层时才淡淡显示出来</td></tr>
    <tr><td>快捷键</td><td>按 ? 查看全部</td></tr>
    <tr><td rowspan="3"><b>导出</b></td><td>格式与倍率</td><td>PNG / JPG / WebP，1× / 2× / 3×；JPG、WebP 可调质量，PNG、WebP 可导出透明背景</td></tr>
    <tr><td>局部导出</td><td>只导出选中的图层；复制 PNG 到剪贴板</td></tr>
    <tr><td>命令行导出</td><td><code>davinci render</code> 在服务端出图，和画布上看到的一致（两边用同一个渲染器）</td></tr>
    <tr><td rowspan="3"><b>素材库</b></td><td>管理</td><td>分类（人像、背景、APP Logo、装饰元素……可自建）、标签和说明、搜索、批量上传</td></tr>
    <tr><td>在编辑器里用</td><td>打开素材面板点击插入，或直接拖进画布；「换一张图」也能从库里选</td></tr>
    <tr><td>给 AI 用</td><td><code>davinci lib</code> 搜索、插入、上传；AI 可以按「指向右边」「惊讶」这样的描述找人像</td></tr>
    <tr><td rowspan="3"><b>模板库</b></td><td>模板</td><td>模板就是完整的分层项目，只是归在模板库里；打开时默认只读预览，点「编辑模板」才进入编辑。作品可以一键移进来，也能移回去</td></tr>
    <tr><td>收藏与整理</td><td>喜欢的封面拖进来、⌘V 粘贴或贴图片链接就成了模板；标签筛选、搜索，每个模板可以写备注（喜欢它哪里）和原帖链接</td></tr>
    <tr><td>照着做</td><td>「用这个模板新建」把整份模板复制成新作品，在副本上改；AI 用 <code>davinci tpl</code> 查看和照着做</td></tr>
    <tr><td rowspan="2"><b>项目</b></td><td>项目管理</td><td>首页项目宫格、搜索、按尺寸预设新建、删除；编辑器左上角点项目名改名</td></tr>
    <tr><td>封面缩略图</td><td>第一个画板就是项目的封面缩略图，改动后自动更新</td></tr>
  </tbody>
</table>

## 桌面版（macOS）

```bash
./start.sh app
```

打包出 `app/dist/mac-arm64/davinci.app`（Electron 外壳 + 同一个 davinci 服务，本机构建，未签名）。双击打开就能用，
不需要另装 Go、Node 或 Python：

- 数据放在 `~/Library/Application Support/davinci`，和仓库里的 `data/` 互不影响。
- 菜单「davinci → 安装命令行工具…」把 `davinci` 命令装到 `/usr/local/bin`，「安装 Agent skill…」装给
  Claude Code / Codex / Hermes。之后 Agent 改的每一个图层都实时出现在 App 里。
- `davinci://p_xxx?b=画板` 这样的链接直接在 App 里打开到对应画板。
- 去除背景：没装 rembg 时用 macOS 自带的主体抠图（需要 macOS 14+），不用下载模型。

开发时用 `./start.sh app-dev` 直接以 Electron 打开（用仓库里构建的 davinci 和 `data/`）。

**发给别人用**：没签名的包在别人电脑上会被系统拦下（提示「已损坏」）。用 Developer ID 证书签名并交给 Apple 公证：

1. 在 [appleid.apple.com](https://appleid.apple.com) →「登录与安全」→「App 专用密码」生成一个密码（这个 Apple ID 要在开发者团队里）。
2. 在终端把公证凭证存进钥匙串（会提示输入上一步的密码）：
   `xcrun notarytool store-credentials davinci-notary --apple-id <Apple ID> --team-id <团队 ID>`
3. `./start.sh release`：签名、公证，输出 `app/dist/release/davinci-<版本>-arm64.dmg`。签名时系统问
   「codesign 想使用钥匙串中的密钥」，点「始终允许」。

## 给 AI 用

本项目提供了 CLI 和 REST API，使用这两种方式的效果是一样的，具体的命令和接口说明见 [docs/COMMANDS.md](docs/COMMANDS.md)、[docs/API.md](docs/API.md)、
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 目录

```
cmd/davinci/        CLI 入口
cmd/davinci-cutout/ macOS 主体抠图小工具（Vision；没装 rembg 时「去除背景」用它）
app/                桌面版：Electron 外壳（main.js）与打包配置
internal/doc/       文档模型与命令层（撤销历史也在这里）
internal/render/    服务端渲染进程（Node + CanvasKit）的管理
internal/server/    Echo + SQLite、WS 推送、资产、字体
internal/cli/       cobra 薄封装（HTTP 客户端）
web/                React 界面 + CanvasKit 画布；web/src/render 是共享渲染器
docs/               接口、命令与架构文档
```

数据默认在仓库里的 `data/`（`davinci.db` + `assets/` + `fonts/`，已被 git 忽略）。

## 常见问题

- **端口**：默认 7789，被占用时自动换一个空闲端口；实际地址写在数据目录的 `server.json` 里，`davinci` 命令会自己找到。
  一定要用某个端口时指定 `./start.sh --port 7790`（被占用就报错，不会换）。
- **数据**：项目、素材和上传的字体都放在仓库里的 `data/`（已被 git 忽略），第一次启动时自动创建。
- **中文字体**：macOS 自带的字体就够用；精简的 Linux 要先装中文字体，否则中文显示不出来，
  比如 `sudo apt install fonts-noto-cjk`。自己的字体可以用 `davinci font add <文件>` 加进来。
- **去除背景**：macOS 上开箱即用（系统自带的主体抠图）。想要头发边缘更细的效果，另装 [rembg](https://github.com/danielgatis/rembg)
  并用 BiRefNet 作为 backend，装了就自动改用它：直接跟你的 Agent 说“帮我安装 rembg，并用 BiRefNet 作为 backend”即可。
- **Windows**：`start.sh` 是 bash 脚本，请在 WSL 或 Git Bash 里运行。

## 许可证

[Apache License 2.0](LICENSE)

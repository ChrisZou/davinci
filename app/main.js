// davinci 桌面版：一个窗口，装着本机的 davinci 服务。
//
// 服务就是仓库里那个 Go 程序（打包在 Resources/bin/davinci），编辑器页面也由它提供。
// 这里只做外壳该做的事：把服务拉起来、开窗口、接住 davinci:// 链接，
// 以及把 davinci 命令和 Agent skill 装到这台电脑上——AI 正是通过它们操作每一个图层。

const { app, BrowserWindow, Menu, dialog, shell } = require('electron')
const { spawn, execFile } = require('child_process')
const fs = require('fs')
const http = require('http')
const os = require('os')
const path = require('path')

// 打包后，命令、抠图工具和 skill 都在 Resources 里；开发时用仓库里构建好的。
const repo = path.join(__dirname, '..')
const resources = app.isPackaged ? process.resourcesPath : repo
const isWin = process.platform === 'win32'
const isMac = process.platform === 'darwin'
const davinciBin = path.join(resources, 'bin', isWin ? 'davinci.exe' : 'davinci')
const skillDir = path.join(resources, 'skills', 'davinci')
// 文档、素材、字体都在这里。和 davinci 命令自己算出来的位置一致：App 里的命令用
// ~/Library/Application Support/davinci（Windows 是 %APPDATA%\davinci），仓库里的命令用仓库的 data/。
const dataDir = app.isPackaged ? path.join(app.getPath('appData'), 'davinci') : path.join(repo, 'data')
// Electron 自己的缓存放进数据目录的一个子文件夹，别和文档混在一起。
app.setPath('userData', path.join(dataDir, '.electron'))

let server = null // 这个 App 拉起的服务进程；用的是别人拉起的服务时为 null
let baseURL = ''
let mainWindow = null
let pendingRoute = null // App 还没准备好时收到的 davinci:// 链接

// --- 服务 ---------------------------------------------------------------------

function readServerFile() {
  try {
    return JSON.parse(fs.readFileSync(path.join(dataDir, 'server.json'), 'utf8'))
  } catch {
    return null
  }
}

/** 地址上那个服务的 boot id；没有服务应答时为 null。 */
function bootOf(url) {
  return new Promise((resolve) => {
    const req = http.get(url + '/api/health', { timeout: 800 }, (res) => {
      let body = ''
      res.on('data', (c) => (body += c))
      res.on('end', () => {
        try {
          resolve(res.statusCode === 200 ? JSON.parse(body).boot ?? null : null)
        } catch {
          resolve(null)
        }
      })
    })
    req.on('timeout', () => req.destroy())
    req.on('error', () => resolve(null))
  })
}

/** server.json 里记的服务还活着就是它的地址，否则 null。 */
async function liveServer() {
  const f = readServerFile()
  if (f?.url && f.boot && (await bootOf(f.url)) === f.boot) return f.url
  return null
}

/**
 * 数据目录已经有服务在跑（比如 Agent 用命令行拉起的）就直接用；没有就拉起一个。
 * 服务自己挑端口（默认 7789，被占用就换一个），写进 server.json。
 */
async function ensureServer() {
  const running = await liveServer()
  if (running) return running
  fs.mkdirSync(dataDir, { recursive: true })
  const log = fs.openSync(path.join(dataDir, 'serve.log'), 'a')
  // windowsHide：服务是控制台程序，不然 Windows 上会弹出一个命令行窗口。
  server = spawn(davinciBin, ['serve', '--data', dataDir], { stdio: ['ignore', log, log], env: process.env, windowsHide: true })
  let exited = false
  server.on('exit', () => {
    exited = true
    server = null
  })
  const deadline = Date.now() + 20000
  while (Date.now() < deadline) {
    const url = await liveServer()
    if (url) return url
    // 没起来：多半是同一个数据目录已经有服务了（它赢了），上面会读到它；否则是真失败。
    if (exited && !readServerFile()) break
    await new Promise((r) => setTimeout(r, 150))
  }
  throw new Error(`davinci 服务没能启动，日志在 ${path.join(dataDir, 'serve.log')}`)
}

// --- 窗口与链接 ------------------------------------------------------------------

/** davinci://editor/p_xxx?b=yyy（或 davinci://p_xxx?b=yyy）→ /editor/p_xxx?b=yyy */
function routeOf(link) {
  try {
    const u = new URL(link)
    let p = `${u.host}${u.pathname}`.replace(/^\/+|\/+$/g, '')
    if (/^p_/.test(p)) p = `editor/${p}`
    return `/${p}${u.search}`
  } catch {
    return '/'
  }
}

function openRoute(route) {
  if (!baseURL) {
    pendingRoute = route
    return
  }
  if (!mainWindow || mainWindow.isDestroyed()) createWindow(route)
  else {
    mainWindow.loadURL(baseURL + route)
    if (mainWindow.isMinimized()) mainWindow.restore()
    mainWindow.focus()
  }
}

// 红黄绿按钮：离顶边和左边一样远。只在首页显示——首页左上角是空的；其他页面左上角
// 是它们自己的东西（logo 点了回首页），按钮藏起来。
const TRAFFIC_LIGHTS = { x: 16, y: 16 }

function windowOptions() {
  return {
    width: 1440,
    height: 920,
    minWidth: 1024,
    minHeight: 680,
    title: 'davinci',
    backgroundColor: '#f1ece6',
    // Windows 的菜单栏在窗口里：平时收起，按 Alt 出来。
    autoHideMenuBar: isWin,
    // macOS 不要单独的标题栏：页面铺到窗口顶上。
    ...(isMac ? { titleBarStyle: 'hidden', trafficLightPosition: TRAFFIC_LIGHTS } : {}),
  }
}

function updateTrafficLights(win) {
  if (!isMac || win.isDestroyed()) return
  let home = true
  try {
    home = new URL(win.webContents.getURL()).pathname === '/'
  } catch {}
  win.setWindowButtonVisibility(home)
  // 新版 macOS 会在缩放、进出全屏后把按钮放回默认位置，每次都再摆一次。
  if (home) win.setWindowButtonPosition(TRAFFIC_LIGHTS)
}

/** 每个窗口（包括从页面里另开的）都一样：外链交给浏览器，按页面显示或藏起红黄绿按钮。 */
function setupWindow(win) {
  // 本服务的页面（比如「管理素材库 ↗」）在 App 里另开窗口，别的网址交给浏览器。
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith(baseURL)) return { action: 'allow', overrideBrowserWindowOptions: windowOptions() }
    shell.openExternal(url)
    return { action: 'deny' }
  })
  win.webContents.on('did-create-window', (child) => setupWindow(child))
  win.webContents.on('will-navigate', (e, url) => {
    if (!url.startsWith(baseURL)) {
      e.preventDefault()
      shell.openExternal(url)
    }
  })
  // 页面内跳转（pushState）也算：首页 ↔ 编辑器 ↔ 素材库。
  for (const ev of ['did-navigate', 'did-navigate-in-page', 'did-finish-load']) win.webContents.on(ev, () => updateTrafficLights(win))
  for (const ev of ['resize', 'leave-full-screen']) win.on(ev, () => updateTrafficLights(win))
}

function createWindow(route = '/') {
  const win = new BrowserWindow({ ...windowOptions(), show: false })
  setupWindow(win)
  win.once('ready-to-show', () => {
    updateTrafficLights(win)
    win.show()
  })
  win.loadURL(baseURL + route)
  if (!mainWindow || mainWindow.isDestroyed()) mainWindow = win
  return win
}

// --- 把 davinci 命令和 skill 装到这台电脑上 -----------------------------------------

function run(cmd, args) {
  return new Promise((resolve, reject) =>
    execFile(cmd, args, { windowsHide: true }, (err, stdout, stderr) => (err ? reject(new Error(stderr || err.message)) : resolve(stdout.trim()))),
  )
}

function linkTarget(p) {
  try {
    // Windows 的目录联接读出来带 \\?\ 前缀。
    return fs.readlinkSync(p).replace(/^\\\\\?\\/, '').replace(/[\\/]$/, '')
  } catch {
    return null
  }
}

/** 指向一个目录的链接：Windows 用目录联接（不要管理员权限），其他系统用软链接。 */
function linkDir(target, link, relative) {
  if (isWin) fs.symlinkSync(target, link, 'junction')
  else fs.symlinkSync(relative ?? target, link)
}

function exists(p) {
  try {
    fs.lstatSync(p)
    return true
  } catch {
    return false
  }
}

/** 已有的东西不是指向 App 的：问一句要不要换掉。 */
async function okToReplace(where, current) {
  const { response } = await dialog.showMessageBox({
    type: 'question',
    buttons: ['换成 App 里的', '取消'],
    defaultId: 0,
    cancelId: 1,
    message: `${where} 已经存在`,
    detail: current ? `现在指向：${current}` : '它不是一个链接。',
  })
  return response === 0
}

/** Windows：把 App 里命令所在的目录加进用户的 PATH（不用管理员权限）。 */
async function installCLIWindows() {
  const dir = path.dirname(davinciBin)
  const q = (v) => `'${v.replace(/'/g, "''")}'`
  const ps =
    `$d = ${q(dir)}; $p = [Environment]::GetEnvironmentVariable('Path', 'User'); if (-not $p) { $p = '' }; ` +
    `if (($p -split ';') -notcontains $d) { [Environment]::SetEnvironmentVariable('Path', (($p.TrimEnd(';') + ';' + $d).TrimStart(';')), 'User') }`
  try {
    await run('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', ps])
  } catch (e) {
    dialog.showErrorBox('没装上 davinci 命令', String(e.message || e))
    return
  }
  await dialog.showMessageBox({
    message: '装好了 davinci 命令',
    detail: `已把 ${dir} 加进你的 PATH。新开一个终端（Agent 也要重新打开）后输入 davinci projects 试试。`,
  })
}

/** 像 VS Code 的「在 PATH 中安装 code 命令」：/usr/local/bin/davinci → App 里的命令。 */
async function installCLI() {
  if (isWin) return installCLIWindows()
  const dest = '/usr/local/bin/davinci'
  const current = linkTarget(dest)
  if (current === davinciBin) {
    await dialog.showMessageBox({ message: 'davinci 命令已经装好了', detail: `${dest} → ${davinciBin}` })
    return
  }
  if (exists(dest) && !(await okToReplace(dest, current))) return
  try {
    fs.mkdirSync(path.dirname(dest), { recursive: true })
    fs.rmSync(dest, { force: true })
    fs.symlinkSync(davinciBin, dest)
  } catch {
    // /usr/local/bin 要管理员权限时，让系统弹出输密码的窗口。
    const q = (s) => `'${s.replace(/'/g, `'\\''`)}'`
    const sh = `mkdir -p ${q(path.dirname(dest))} && ln -sfn ${q(davinciBin)} ${q(dest)}`
    try {
      await run('/usr/bin/osascript', ['-e', `do shell script ${JSON.stringify(sh)} with administrator privileges`])
    } catch (e) {
      dialog.showErrorBox('没装上 davinci 命令', String(e.message || e))
      return
    }
  }
  // 终端里的 PATH 可能先找到别的 davinci（比如开发用的那个）。
  let found = ''
  try {
    found = await run('/bin/zsh', ['-lc', 'command -v davinci'])
  } catch {}
  const detail =
    found && found !== dest
      ? `${dest} → App 里的命令。\n\n注意：你的终端会先找到 ${found}，Agent 用的会是那一个。`
      : `${dest} → App 里的命令。在终端里输入 davinci projects 试试。`
  await dialog.showMessageBox({ message: '装好了 davinci 命令', detail })
}

/** 给 Claude Code（以及有的话 Codex、Hermes）装 davinci skill，做法和仓库里的 skills/install.sh 一样。 */
async function installSkill() {
  const home = os.homedir()
  const claude = path.join(home, '.claude', 'skills', 'davinci')
  const current = linkTarget(claude)
  if (current !== skillDir) {
    if (exists(claude) && current === null) {
      // 用户自己放的 skill 文件夹：不删，和 skills/install.sh 一样跳过。
      await dialog.showMessageBox({ message: `${claude} 是一个文件夹，不是链接，没有覆盖它` })
      return
    }
    if (exists(claude) && !(await okToReplace(claude, current))) return
    fs.mkdirSync(path.dirname(claude), { recursive: true })
    fs.rmSync(claude, { force: true })
    linkDir(skillDir, claude)
  }
  const done = ['Claude Code']
  for (const [agent, dir] of [
    ['Codex', '.codex'],
    ['Hermes', '.hermes'],
  ]) {
    if (!exists(path.join(home, dir))) continue
    const link = path.join(home, dir, 'skills', 'davinci')
    if (exists(link) && linkTarget(link) === null) continue // 用户自己放的，不动
    fs.mkdirSync(path.dirname(link), { recursive: true })
    fs.rmSync(link, { force: true })
    linkDir(skillDir, link, '../../.claude/skills/davinci')
    done.push(agent)
  }
  await dialog.showMessageBox({
    message: `装好了 davinci skill：${done.join('、')}`,
    detail: '新开一个 Agent 会话，跟它说「做一张小红书封面」就行。Agent 还需要 davinci 命令（菜单里的「安装命令行工具」）。',
  })
}

// --- 菜单 -------------------------------------------------------------------

function buildMenu() {
  const install = [
    { label: '安装命令行工具…', click: () => void installCLI() },
    { label: '安装 Agent skill…', click: () => void installSkill() },
  ]
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(isWin ? [] : [{
        role: 'appMenu',
        submenu: [
          { role: 'about', label: '关于 davinci' },
          { type: 'separator' },
          ...install,
          { type: 'separator' },
          { role: 'hide', label: '隐藏 davinci' },
          { role: 'hideOthers', label: '隐藏其他' },
          { role: 'unhide', label: '全部显示' },
          { type: 'separator' },
          { role: 'quit', label: '退出 davinci' },
        ],
      }]),
      {
        label: '文件',
        submenu: [
          { label: '新窗口', accelerator: 'CmdOrCtrl+N', click: () => createWindow('/') },
          { label: '素材库', click: () => openRoute('/library') },
          { label: '模板库', click: () => openRoute('/templates') },
          { type: 'separator' },
          { label: '打开数据文件夹', click: () => shell.openPath(dataDir) },
          { type: 'separator' },
          isWin ? { role: 'quit', label: '退出' } : { role: 'close', label: '关闭窗口' },
        ],
      },
      // 文本框里的拷贝粘贴靠它；画布上的 ⌘Z、⌘C 由编辑器自己接住（它会 preventDefault）。
      { role: 'editMenu', label: '编辑' },
      {
        label: '显示',
        // 不放缩放：⌘0、⌘+、⌘- 是编辑器的画布缩放。
        submenu: [
          { role: 'reload', label: '重新载入' },
          { role: 'toggleDevTools', label: '开发者工具' },
          { type: 'separator' },
          { role: 'togglefullscreen', label: '全屏' },
        ],
      },
      isWin ? { label: '帮助', submenu: [...install, { type: 'separator' }, { role: 'about', label: '关于 davinci' }] } : { role: 'windowMenu', label: '窗口' },
    ]),
  )
}

// --- 生命周期 -----------------------------------------------------------------

if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.setAsDefaultProtocolClient('davinci')
  app.on('open-url', (e, link) => {
    e.preventDefault()
    openRoute(routeOf(link))
  })
  app.on('second-instance', (_e, argv) => {
    const link = argv.find((a) => a.startsWith('davinci://'))
    if (link) openRoute(routeOf(link))
    else if (mainWindow && !mainWindow.isDestroyed()) mainWindow.focus()
  })

  // Windows 用 davinci:// 链接启动 App 时，链接在命令行参数里。
  const startLink = process.argv.find((a) => a.startsWith('davinci://'))
  if (startLink) pendingRoute = routeOf(startLink)

  app.whenReady().then(async () => {
    buildMenu()
    try {
      baseURL = await ensureServer()
    } catch (e) {
      dialog.showErrorBox('davinci 启动失败', String(e.message || e))
      app.quit()
      return
    }
    createWindow(pendingRoute ?? '/')
    pendingRoute = null
  })

  app.on('activate', () => {
    if (baseURL && BrowserWindow.getAllWindows().length === 0) createWindow('/')
  })

  // 关掉最后一个窗口时退出（服务也一起停）。Agent 之后再用 davinci 命令会自己拉起服务。
  app.on('window-all-closed', () => app.quit())

  app.on('will-quit', () => {
    if (server) server.kill('SIGTERM')
  })
}

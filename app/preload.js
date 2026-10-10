// 页面能用到的 App 能力：首页的引导里装命令行工具和 Agent skill。只开放这几样。
const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('davinciApp', {
  platform: process.platform,
  setupStatus: () => ipcRenderer.invoke('setup:status'),
  installCLI: () => ipcRenderer.invoke('setup:cli'),
  installSkill: () => ipcRenderer.invoke('setup:skill'),
})

# GUI.for.Clash 服务器 WebUI 干净重构与深度清理指南

本指南详细记录了如何将基于 Wails 框架的桌面版 `GUI.for.Clash` 客户端，重构并深度清理为可在无图形 Linux (Debian) / FNAS 服务器上运行的内网免密 WebUI 代理面板的完整步骤。适用于新对话中的全新构建或二次验证与修复。

---

## 🧹 第一步：物理清理桌面端冗余模块与页面

彻底删除项目中所有不属于 Mihomo 核心管理、且已被弃用的桌面版页面组件与资源：
1. **删除冗余视图目录**（前端物理删除）：
   - `frontend/src/views/PlaygroundView/`
   - `frontend/src/views/PluginsView/`
   - `frontend/src/views/ScheduledTasksView/`
2. **删除桌面窗口与托盘组件**：
   - 物理删除 `frontend/src/components/_common/TitleBar.vue`（顶部标题栏窗口组件）
   - 物理删除 `bridge/tray.go`（系统托盘交互逻辑）
3. **物理删除服务器管理多余模块**：
   - 物理删除后端文件 `bridge/server.go`（本地静态文件服务器与文件接收服务）
   - 物理删除前端文件 `frontend/src/bridge/server.ts`（前端对应接口封装桩）
4. **删除桌面打包与配置文件**：
   - 根目录下 Wails 配置文件 `wails.json`
   - `build/` 打包图标与配置文件目录

---

## 🏗️ 第二步：Go 后端重构与依赖精简

### 1. 彻底移除非必要依赖
- 移除了桌面版托盘所需的 `github.com/energye/systray` 库及其在 `go.mod` 中的 `replace` 声明。
- 移除了用于在后端拉起系统浏览器的 **`github.com/pkg/browser`** 依赖。在 `bridge/io.go` 中，将 `OpenDir` 和 `OpenURI` 改造为免依赖的空桩：
  ```go
  func (a *App) OpenDir(path string) FlagResult {
      log.Printf("OpenDir (Stubbed): %s", path)
      return FlagResult{true, "Success"}
  }
  func (a *App) OpenURI(uri string) FlagResult {
      log.Printf("OpenURI (Stubbed): %s", uri)
      return FlagResult{true, "Success"}
  }
  ```
- 运行 `go mod tidy` 整理依赖，使后端核心依赖**精简至仅剩 7 个**（Gin, websocket, geoip2, gopsutil, x/sys, x/text, yaml）。

### 2. 剥离桌面端初始化逻辑 (`bridge/bridge.go`)
- 删除了 Windows 下 WebView2 Runtime 自动解压与检测运行库的代码（`processFixedWebView2Runtime`）。
- 删除了 macOS 专属的软链接创建逻辑（`createMacOSSymlink`）。
- 删除了每次启动时向磁盘缓存目录 `.cache/` 自动解压缩图标和图片的冗余逻辑（`extractEmbeddedFiles`），消除了不必要的磁盘消耗与系统调用。
- 精简了通用配置加载逻辑（`loadConfig`），剔除了窗口宽高及最小化状态的读取，仅加载 `user.yaml`。

### 3. 清理已弃用的 API 路由 (`main.go` & `types.go`)
- 在 `main.go` 中，删除了挂载在 `/api/server/...` 目录下的所有本地服务器控制接口路由。
- 在 `bridge/types.go` 中，删除了已不再使用的 `ServerOptions` 结构体。

---

## 💻 第三步：前端页面精简与 TS 构建修复

### 1. 彻底移除顶部标题栏 (TitleBar)
- 在 [AppShell.vue](file:///d:/Users/yuyue/Documents/Code/FNOS_APP/GUI.for.Clash-Web/frontend/src/components/_common/AppShell.vue) 中移除 `<TitleBar />` 组件的渲染和导入，使页面版面从 `NavigationBar` 导航栏直接开始，高度利用率最大化。
- 在 `components/index.ts` 中删除了对 `TitleBar` 的导出。

### 2. 深度精简设置面板 (SettingsView)
在设置各子组件中移除全部特定于桌面的行为属性：
- **`BehaviorSettings.vue`**：物理删除“以管理员运行”、“开机自启”、“启动延迟”、“关闭窗口退出程序”等无意义选项，**仅保留“程序启动时开启核心”**。
- **`AdvancedSettings.vue`**：物理删除“打开应用程序文件夹”按钮、“滚动释放”、“多实例运行”以及“窗口内容防截屏保护”选项，**仅保留“显示真实内核内存占用”与“自动重启内核”**。
- **`PersonalizationSettings.vue`**：物理删除“打开本地语言文件夹”死按钮。

### 3. 路由与导出清理
- 在 `frontend/src/router/routes.ts` 中移除已删除页面（Playground、Plugins、ScheduledTasks）的路由导入与映射定义。
- 将**规则集 (Rulesets)** 路由的可视性 `hidden` 恢复为 `false`，使其正常展现在导航栏左侧。
- 在 `frontend/src/bridge/index.ts` 中，移除对已删除服务模块 `server` 的重新导出声明 (`export * from './server'`)。

### 4. TypeScript 6 处构建报错修复
修复了开发和打包时触发的 TS 严格检查校验错误：
- **`InterfaceSelect/index.vue`** / **`useCoreBranch.ts`** / **`helper.ts`**：在 `find` / `map` / `findIndex` 的回调参数中显式指定类型（如 `string`、`any`），消除 `implicitAny` 报错。
- **`stores/app.ts`** / **`stores/kernelApi.ts`** / **`stores/plugins.ts`**：对可空属性的属性读取加上非空断言 `!` 或前置 undefined 校验。

### 5. 国际化 i18n 翻译文件瘦身
在 `zh.ts` 和 `en.ts` 中物理删除了已废弃模块和桌面属性所对应的所有翻译 Key，包括：
- `titlebar` 整个语言节点。
- `tray` 整个语言节点。
- `plugin` 与 `plugins` 整个语言节点。
- `scheduledtask` 与 `scheduledtasks` 整个语言节点。
- `settings` 内部与窗口大小、防截屏、自启动、GPU策略、退出内核相关的全部冗余翻译项。

---

## 🏗️ 第四步：最终打包构建与体积优化

### 1. 前端打包
在 `frontend/` 目录运行以下命令进行依赖安装与构建：
```bash
pnpm install
npm run build
```
生产环境静态资源输出至 `frontend/dist`（总大小约 2.3MB）。

### 2. 后端编译
在项目根目录下，使用 `-ldflags="-s -w"` 参数移除所有调试符号与 DWARF 数据，以实现二进制文件的体积优化：
```bash
go build -ldflags="-s -w" -o mihomo-web.exe main.go
```

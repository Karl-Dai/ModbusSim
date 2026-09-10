# Changelog

All notable changes to ModbusSim are documented in this file.

本文档记录 ModbusSim 所有显著变更,中英对照。

格式遵循 [Keep a Changelog](https://keepachangelog.com/),版本号遵循 [Semantic Versioning](https://semver.org/)。

---

## [0.17.4] - 2026-09-10

### Highlights / 亮点

- 修复 macOS 加载 PEM 客户端证书时重复导入私钥、提示钥匙串条目已存在的问题。 / Fix duplicate private-key imports when loading PEM client certificates on macOS.
- 新增明确的旧设备证书兼容选项，支持缺少 EKU/SAN 的测试设备；默认保留严格验证。 / Provide explicit legacy-certificate compatibility for test devices without EKU/SAN while retaining strict verification by default.
- TLS 连接支持从站 ID 和寄存器扫描，保留分块、请求间隔、进度、取消及原从站 ID。 / Support slave-ID and register scans over TLS with chunking, pacing, progress, cancellation and preserved unit IDs.
- 主站与从站共用菜单栏和快捷操作；主站可编辑已有连接设置而保留扫描组。 / Share menus and quick actions between both apps, and edit existing master connection settings without losing scan groups.

### Fixed 修复

- PEM 证书链与私钥分别传给 TLS 库，避免 macOS 临时钥匙串重复导入相同私钥。/ Pass PEM chains and private keys separately to the TLS library to avoid duplicate imports into macOS temporary keychains.
- 扫描不再要求普通 TCP 专用上下文，统一通过当前连接的传输读取；TLS 读写采用调用者指定的超时时间。/ Discovery no longer requires a plain-TCP context; scans read through the active transport, and TLS operations apply the caller's timeout.
- 修复工具栏和表格布局，增强窄窗口、菜单定位及键盘交互。/ Correct toolbar and table layouts and improve narrow-window behavior, menu positioning and keyboard interaction.

### Changed 改进

- 主站“连接设置”可在断开后修改目标、证书、串口、请求与重连参数，并保留连接及扫描组。/ Master connection settings can edit endpoints, certificates, serial parameters, request limits and reconnect options while disconnected, preserving the connection and its scan groups.
- 两端共用菜单栏、快捷按钮、项目名称与操作状态；支持方向键跨菜单导航、焦点恢复以及统一工程快捷键。/ Both apps share menus, quick actions, project names and operation status, with cross-menu arrow navigation, focus restoration and common project shortcuts.
- 旧设备证书兼容模式仅对明确启用的连接生效；连接仍使用 TLS 加密，但跳过服务器证书和地址验证。/ Legacy compatibility is an explicit per-connection option: traffic stays TLS-encrypted, but server certificate and hostname verification are skipped.

### Tests 测试

- 317 项 Rust 测试、41 项前端测试通过，覆盖 PEM 重复加载、旧证书严格拒绝与兼容连接、TLS 扫描、取消、从站 ID 保持和菜单交互。/ 317 Rust tests and 41 frontend tests pass, covering repeated PEM loading, strict rejection and compatible connections for legacy certificates, TLS scans, cancellation, preserved unit IDs and menu interaction.

## [0.17.3] - 2026-09-09

### Highlights / 亮点

- 从站公共界面对齐 IEC104：统一分组菜单、可拖动布局、弹窗和中文寄存器分组。 / Slave common UI aligned with IEC104: grouped menus, resizable layout, consistent dialogs and localized register groups.
- 显示真实客户端数量、对端地址与连接时间，支持 TCP、TLS、RTU-over-TCP。 / Show live client counts, peer addresses and connection times for TCP, TLS and RTU-over-TCP.
- 补齐寄存器 CSV 导入导出、批量操作、报文解析和日志交互；变位启停归入模拟设置。 / Add register CSV workflows, batch actions, frame tools and richer logs; control point mutation through simulation settings.
- 主站支持请求间隔、单次读取上限和重连参数，读取自动分包并保留旧项目默认值。 / Master adds request pacing, read limits and reconnect settings, with automatic read batching and backward-compatible project defaults.

### Added 新增

- 从站新增全部启停、批量连接删除、停止状态下编辑监听/串口参数、工程路径打开和保存快捷键。/ Slave adds start/stop all, batch connection deletion, endpoint/serial editing while stopped, project opening by path and save shortcuts.
- 寄存器新增 CSV 模板、预校验、追加与替换导入，以及多选复制、批量赋值、编辑和删除。导入冲突时整批拒绝，替换需确认并停止连接。/ Registers gain CSV templates, validation, append/replace import, multi-selection copy, batch values, editing and deletion. Conflicting imports are rejected as a whole; replacement requires confirmation and a stopped connection.
- 工具支持 TCP/RTU/ASCII 请求与响应解析、MBAP/CRC/LRC 校验及 PLC 地址换算；日志支持暂停、自动滚动、筛选结果复制和文件导出。/ Tools inspect TCP/RTU/ASCII requests and responses, validate MBAP/CRC/LRC and convert PLC addresses; logs gain pause, auto-scroll, filtered copy and file export.
- 主站读取配置范围为每请求 1–125 个寄存器或 1–2000 位，请求间隔为 0–60000 ms。/ Master read limits support 1–125 registers or 1–2000 bits per request and a request interval of 0–60000 ms.

### Fixed 修复

- 修复寄存器删除命令未注册、异步旧日志/数值回写、CSV 地址重叠和无符号类型名称兼容问题。/ Fixes an unregistered register-deletion command, stale asynchronous log/value updates, CSV overlap handling and unsigned-type name compatibility.
- 移除顶部容易误导的变位总开关；启用点位配置后立即运行，并由表格独立刷新数值。模拟设置执行期间可关闭抽屉，后台任务继续使用原目标。/ Removes the misleading toolbar mutation switch; enabling point settings starts mutation directly while the table refreshes values independently. The simulation drawer can close during operations without changing their original targets.
- 客户端断开和监听停止后清理连接记录，避免重启后残留旧客户端。/ Cleans up tracked clients on disconnect and listener stop to avoid stale peers after restart.

### Compatibility 兼容性

- 保留既有工程格式、Modbus 功能码和串口语义；旧工程未配置的请求参数使用原默认值。CSV 保存配置而不包含运行时值。/ Preserves existing project formats, Modbus function codes and serial semantics; absent request settings use their previous defaults. CSV contains configuration rather than runtime values.

### Tests 测试

- 增加真实客户端生命周期、请求分包/节流、CSV 原子校验、报文解析、翻译键及模拟抽屉关闭期间目标保持等回归覆盖。/ Adds regression coverage for real client lifecycle, request batching/pacing, atomic CSV validation, frame parsing, translation keys and preserved targets when closing the simulation drawer.

## [0.17.2] - 2026-08-14

补丁版本:恢复 ModbusMaster 正常启动,并交付完整的从站点位工作流、SOCKS5 代理、静默后台更新和自动发布链路。无破坏性变更,现有 v1 工程文件继续兼容。

Patch release: restores ModbusMaster startup and delivers complete slave point workflows, SOCKS5 proxying, silent background updates, and automated releases. There are no breaking changes; existing v1 project files remain compatible.

### Highlights / 亮点

- 🚑 **ModbusMaster 恢复启动** — 修复 Aptabase 初始化时缺少 Tokio runtime 导致所有安装包打开即退出的问题。/ **ModbusMaster starts normally again** — fixes the missing Tokio runtime during Aptabase initialization that caused packaged Master apps to exit immediately.
- 🎛️ **完整点位工作流** — 从站支持独立周期的点位变异、数据源、宽值地址冲突校验、站号/名称编辑以及工程保存恢复。/ **Complete point workflows** — slave points gain independent mutation schedules, data sources, wide-value overlap validation, editable unit metadata, and project save/restore.
- 🌐 **SOCKS5 代理** — Master 的 TCP、TCP+TLS、RTU-over-TCP 支持 IPv4/IPv6、代理侧域名解析和可选用户名/密码认证。/ **SOCKS5 proxying** — Master TCP, TCP+TLS, and RTU-over-TCP support IPv4/IPv6, proxy-side DNS, and optional username/password authentication.
- 🔄 **静默更新与自动发布** — 更新包先在后台下载并验签,准备完成后再提示立即安装、跳过该版本或下次启动安装;发布矩阵覆盖两款应用的 10 个平台构建任务。/ **Silent updates and automated releases** — updates download and verify before prompting to install now, skip the version, or install on next launch; the release matrix covers 10 platform builds across both apps.

### Added 新增

- **从站点位变异与数据源** — 每个点位可配置固定值、随机、正弦、斜坡与 CSV 数据源;单一 100 ms 后端 tick 按各点位周期独立调度,配置随工程持久化。/ **Slave point mutation and data sources** — each point can use fixed, random, sine, ramp, or CSV data; one 100 ms backend tick honors independent point schedules and persists configuration with the project.
- **SOCKS5 CONNECT** — 新增三类 Master 网络传输的代理连接与工程保存恢复;密码不会出现在 Rust Debug 输出中,界面明确提示 RFC 1929 凭据与工程文件的安全边界。/ **SOCKS5 CONNECT** — adds proxy connections and project persistence to three Master transports; passwords are redacted from Rust debug output, and the UI explains the RFC 1929 and project-file security boundary.

### Changed 改进

- **应用内更新流程** — Slave 与 Master 共用“后台下载并验签 → 再提示”的状态机,替代等待用户确认后才开始下载的流程。/ **In-app updater flow** — Slave and Master now share a download-and-verify-before-prompt state machine instead of waiting for confirmation before downloading.
- **工程恢复完整性** — Master 扫描组、寄存器定义和值,以及 Slave 变异和点位数据源均可完整保存并恢复运行时状态。/ **Project restoration completeness** — Master scan groups, register definitions and values, plus Slave mutations and point data sources now save and restore their runtime state.
- **自动发布链路** — 夜间任务准备补丁版本,发布 PR 合并且 Test 通过后自动打 tag,再构建 macOS Apple Silicon/Intel、Linux x64、Windows x64/ARM64 产物和独立更新清单。/ **Automated release pipeline** — nightly automation prepares patch releases; a merged release PR that passes Test is tagged and built for macOS Apple Silicon/Intel, Linux x64, and Windows x64/ARM64 with separate updater manifests.

### Fixed 修复

- **Master 启动闪退** — 在构建 Tauri 与 Aptabase 插件前创建并进入多线程 Tokio runtime,消除 `there is no reactor running` panic。/ **Master startup crash** — creates and enters a multi-threaded Tokio runtime before Tauri and Aptabase initialization, eliminating the `there is no reactor running` panic.
- **协议与扫描健壮性** — 加强非法 Modbus PDU、未定义地址、重连和扫描取消处理,避免错误响应、残留任务或扫描状态卡住。/ **Protocol and scanner hardening** — strengthens invalid Modbus PDU, undefined-address, reconnect, and scan-cancellation handling to prevent incorrect responses, orphaned tasks, and stuck scans.
- **寄存器与工程一致性** — 后端拒绝重复或重叠的宽值寄存器范围,寄存器编辑改为原子操作,并修复从站工程保存/加载和旧 32 位寄存器块迁移。/ **Register and project consistency** — rejects duplicate or overlapping wide-register ranges, makes register edits atomic, and fixes slave project save/load plus legacy 32-bit register-block migration.

### Security 安全

- 将 Vite/PostCSS 间接依赖 `nanoid` 从 3.3.17 升级到 3.3.18,修复零长度自定义生成器可能无限循环的高危拒绝服务问题(GHSA-2v37-7h3g-55p8)。/ Upgraded the Vite/PostCSS transitive dependency `nanoid` from 3.3.17 to 3.3.18, fixing the high-severity denial-of-service issue where a zero-sized custom generator could loop indefinitely (GHSA-2v37-7h3g-55p8).

### Internal 内部

- 发布前门禁通过 290 个 Rust 测试、16 个共享前端测试与 20 个发布脚本测试,并覆盖 Rust 格式化、Clippy、两端生产构建、npm audit、Release 元数据与产物校验。/ Release gates passed 290 Rust tests, 16 shared frontend tests, and 20 release-script tests, plus Rust formatting, Clippy, both production builds, npm audit, release metadata, and artifact validation.

## [0.17.1] - 2026-06-12

维护版本:发布流水线修复 + OpenSpec 规格归档。**无应用代码改动**,安装包与 v0.17.0 功能一致。

Maintenance release: release-pipeline fix plus OpenSpec spec archival. **No application code changes** — installers are functionally identical to v0.17.0.

### Fixed 修复

- **发布流水线** — `publish-manifest` job 补 `contents: write` 权限,修复其默认 token 看不到 draft release、导致 release 卡在草稿且 body 不被替换的问题(v0.16 / v0.17 曾因此需要手动补救)。/ **Release pipeline** — granted the `publish-manifest` job `contents: write` so its token can see and publish the draft release; fixes releases getting stuck as drafts with an unreplaced body (previously required manual rescue on v0.16 / v0.17).

### Internal 内部

- **OpenSpec 规格归档** — 归档 `value-panel-writeback` / `improved-slave-register-ui` / `slave-ui-overhaul` 三个已完成 change,同步生成 9 个能力规格;废弃已 moot 的 `egui-033-shadcn-migration`(egui 已于 v0.16 删除);`auto-update` 规格补全 `## 目的` / `## 需求` 结构并通过校验。/ **OpenSpec archival** — archived three completed changes into nine capability specs, abandoned the moot `egui-033-shadcn-migration` change, and normalized the `auto-update` spec to pass validation.

---

## [0.17.0] - 2026-06-12

Minor 版本:匿名使用统计 + 门面打磨。新增隐私友好的 Aptabase 遥测(仅匿名 `app_started`,可一键关闭),工具栏加版本号与 GitHub 入口,README 对齐姊妹项目精装版式并补齐 MIT LICENSE。

Minor release: anonymous usage analytics plus presentation polish. Adds privacy-friendly Aptabase telemetry (anonymous `app_started` only, one-click opt-out), a toolbar version badge with a GitHub link, a README revamp aligned with the sister project, and the MIT LICENSE file.

### Highlights / 亮点

- 📊 **匿名使用统计** — slave / master 启动时通过 [Aptabase](https://aptabase.com) 上报匿名 `app_started`,作者得以了解装机量、活跃度与版本/系统分布;无任何 PII,工具栏 ⓘ「关于」气泡一键关闭(默认开启)。/ **Anonymous usage analytics**: both apps report an anonymous `app_started` on launch via Aptabase — install counts, active usage, version/OS distribution, no PII, one-click opt-out in the toolbar ⓘ "About" popover.
- 🏷️ **工具栏版本号 + GitHub 图标** — 一眼看到当前版本,点击图标直达仓库。/ **Toolbar version badge + GitHub link**: see the running version at a glance, click through to the repo.
- 📖 **README 精装改版** — 对齐姊妹项目 IEC60870-5-104-Simulator 版式:居中精装头部、徽章、实拍截图与快速开始教程;补齐 MIT `LICENSE`。/ **README revamp** aligned with the sister project: centered hero header, badges, real screenshots, a quick-start tutorial, and the MIT `LICENSE` file.
- 🐛 **修复下载量徽章错误图与 CI draft 问题**。/ **Fixed** the downloads badge error image and a CI release-draft issue.

### Added 新增

- **匿名使用统计(Aptabase)** — slave / master 启动时上报匿名 `app_started`,带 `edition`(slave/master)区分两端;默认开启、可在工具栏 ⓘ「关于」气泡里关闭(opt-out),开关存 `tauri-plugin-store` 的 `settings.json`。仅采集应用版本、操作系统、语言与由 IP 现算的大致国家(不存 IP),无任何 PII。Rust 端新增 `analytics.rs` + `tauri-plugin-aptabase`,前端 `VersionBadge` 扩展为「关于」气泡含遥测开关。/ **Anonymous usage analytics (Aptabase)**: both apps emit an anonymous `app_started` with an `edition` property; enabled by default with a toolbar opt-out (stored in `tauri-plugin-store`'s `settings.json`). Collects only app version, OS, locale and an approximate IP-derived country (IP never stored) — no PII. New Rust `analytics.rs` + `tauri-plugin-aptabase`; the `VersionBadge` gains an "About" popover with the toggle.
- **工具栏版本号徽章 + GitHub 链接图标** — `VersionBadge` 组件运行时读 `getVersion()`,GitHub 图标经 opener 插件打开仓库页。/ **Toolbar version badge + GitHub icon** — `VersionBadge` reads `getVersion()` at runtime; the GitHub icon opens the repo via the opener plugin.
- **MIT `LICENSE` 文件** — README badge 与许可证小节改为链接该文件。/ **MIT `LICENSE` file**, with the README badge and license section linking to it.

### Changed 改进

- **README 中英版面对齐姊妹项目** — 精装居中头部 + 徽章、实拍截图、快速开始教程。/ **README layout aligned with the sister project** — centered hero header with badges, real screenshots, and a quick-start tutorial.

### Fixed 修复

- **下载量徽章错误图** — shields.io 的 GitHub token 池限流时返回的错误文案被 GitHub camo 缓存成错误图;徽章 URL 加 `cacheSeconds=3600` 刷新缓存并降低再次限流概率。/ **Downloads badge error image** — shields.io's rate-limit error message got cached by GitHub camo; added `cacheSeconds=3600` to bust the cache and reduce future throttling.
- **CI 发布草稿** — `publish-manifest` 改用 release id 取消 draft,绕开 tag-404。/ **CI release draft** — `publish-manifest` now cancels the draft by release id, avoiding a tag-404.

### Internal 内部

- 因新增 `tauri-plugin-aptabase` 插件,重新生成两个 app 的 Tauri ACL 清单(`gen/schemas`)。/ Regenerated both apps' Tauri ACL manifests (`gen/schemas`) for the new `tauri-plugin-aptabase` plugin.
- 新增 Aptabase 接入的设计与实现计划文档(`docs/superpowers/`)。/ Added Aptabase design and implementation-plan docs under `docs/superpowers/`.

---

## [0.16.0] - 2026-06-10

Minor 版本:技术栈收敛 + 应用内自动更新。废弃 egui 双轨,全面对齐姊妹项目 IEC60870-5-104-Simulator 的纯 Tauri 2 + Vue 3 架构;移植其成熟的自动更新机制(检查 / 弹窗 / 下载 / 安装 / 重启,多代理容灾 + minisign 签名)。

Minor release: stack consolidation plus in-app auto update. The egui dual-track is discontinued, fully aligning with the sister project IEC60870-5-104-Simulator's pure Tauri 2 + Vue 3 architecture; its battle-tested auto-update pipeline (check / dialog / download / install / restart, multi-proxy failover + minisign verification) is ported over.

### Highlights / 亮点

- 🔄 **应用内自动更新** — 启动静默检查新版,弹窗展示更新说明与下载进度,一键安装重启;工具栏可手动「检查更新」。/ **In-app auto update**: silent startup check, update dialog with release notes & download progress, one-click install & restart; manual "Check for Updates" in the toolbar.
- 🇨🇳 **国内网络友好** — 更新检查走 5 个 endpoint 容灾(自建代理优先,GitHub 直连兜底,3 个公共镜像殿后)。/ **CN-network friendly**: update checks fail over across 5 endpoints (self-hosted proxy first, GitHub direct, then 3 public mirrors).
- 🔐 **更新包签名校验** — 全部更新产物 minisign 签名,客户端内置公钥验证,防篡改。/ **Signed updates**: every update bundle is minisign-signed and verified against the embedded public key.
- 🧹 **BREAKING:egui 版停止发布** — 删除约 8.9k 行 egui 代码,workspace 6 crate → 3 crate,CI 更快更简单;egui 用户请迁移到 Tauri 版。/ **BREAKING: egui edition discontinued** — ~8.9k lines removed, workspace slimmed from 6 to 3 crates; egui users should migrate to the Tauri installers.

### Added 新增

- **应用内自动更新** — 对齐 IEC60870-5-104-Simulator 的更新机制:启动 2 秒后静默检查(6 小时节流),发现新版弹出更新对话框(版本号 / 更新说明 / 下载进度),支持一键下载安装重启与「稍后」24 小时 snooze;工具栏新增「检查更新」按钮(强制检查,无视节流与 snooze)。更新检查走 5 个 endpoint 容灾(自建代理 → GitHub 直连 → 3 个公共代理),更新包 minisign 签名校验。后端 `update.rs`(`check_for_update` / `install_update` / `snooze_update`)+ `tauri-plugin-updater/process/store`;发布链路新增 `scripts/gen-update-manifest.mjs` 与 release.yml `publish-manifest` job。/ **In-app auto update** aligned with the IEC104 simulator: silent startup check (6 h throttle), update dialog with release notes and download progress, one-click install & restart, 24 h snooze, and a toolbar "Check for Updates" button (force check). Checks fail over across 5 endpoints (self-hosted proxy → GitHub → 3 public proxies); bundles are minisign-verified. Backend `update.rs` + updater/process/store plugins; release pipeline gains `gen-update-manifest.mjs` and a `publish-manifest` job.

### Removed 移除

- **BREAKING：egui 原生版停止维护与发布** — 删除 `modbussim-egui` / `modbusmaster-egui` / `modbussim-ui-shared` 三个 crate 及 `ci-egui.yml`、release 中的 egui 打包 job;后续 Release 不再提供 `-egui-` 后缀的二进制,egui 版用户请迁移到 Tauri 版安装包(`.dmg` / `.exe` / `.msi` / `.deb` / `.AppImage` / `.rpm`),功能为 egui 版超集。/ **BREAKING: the native egui edition is discontinued** — the `modbussim-egui` / `modbusmaster-egui` / `modbussim-ui-shared` crates, `ci-egui.yml`, and the egui release packaging job are removed; releases no longer ship `-egui-` suffixed binaries. egui users should migrate to the Tauri installers, which are a feature superset.

---

## [0.15.0] - 2026-05-02

Minor 版本:前端大型重构 + 后端推送式事件架构。两端统一抽出共享 `LogPanelShell` / `useFcLabel` / `formatAddress`;`useDialog` 去掉 provide/inject 中转层并接入 i18n;Slave / Master Toolbar 各拆 3 个 modal 子组件;Slave RegisterTable 拆出 `useRegisterValues` + `useRegisterFormat` composables。后端新增 `RegisterChangeCallback` 与 `LogAppendCallback`,核心写路径成功后 emit `register-value-changed` / `log-appended`,前端 `setInterval` 2s 轮询全量替换为 `listen()`。`LogCollector` 内部从 `Vec` 切到 `VecDeque`,日志满 buffer 时 `pop_front()` 取代 O(N) 的 `remove(0)`。无破坏性变更(仍是单工程 git tag 发版,Cargo.toml/tauri.conf.json 不动)。

Minor release: large frontend refactor plus event-driven push architecture on the backend. Both apps now share `LogPanelShell` / `useFcLabel` / `formatAddress`; `useDialog` drops its provide/inject middleman and gains i18n titles; the Slave and Master Toolbars each split into three modal subcomponents; the Slave RegisterTable factors out `useRegisterValues` + `useRegisterFormat` composables. The backend introduces `RegisterChangeCallback` / `LogAppendCallback`; successful core writes emit `register-value-changed` / `log-appended`, and the frontend replaces every 2-second `setInterval` polling loop with `listen()`. `LogCollector` switches its internal storage from `Vec` to `VecDeque` so the ring-buffer eviction is O(1) instead of O(N). No breaking changes (release versioning still tag-only; Cargo.toml/tauri.conf.json unchanged).

### Highlights / 亮点

- ⚡ **2s 轮询 → 事件推送** — 子站寄存器值与通信日志改为后端 emit Tauri 事件,前端 `listen()` 接收;一次 `WriteMultipleRegisters(values=[100])` 从 200 个独立事件压缩为 1 个 batched event,UI 响应即时。/ 2-second polling replaced by Tauri event push for both register values and communication logs; an FC16 write of 100 registers now sends 1 batched event instead of 200 individual ones.
- 🧹 **共享层一次到位** — 新增 `shared-frontend/components/LogPanelShell.vue` + `composables/useFcLabel`、`useAddressFormat`,主 / 从 `LogPanel.vue` 各从 ~240 行收敛到 ~45 行。/ New `LogPanelShell.vue` + `useFcLabel` / `useAddressFormat` cut both `LogPanel.vue` files from ~240 lines to ~45.
- 🎛️ **Toolbar / RegisterTable 大瘦身** — Slave Toolbar 844→194 行 + 3 个独立 dialog;Master Toolbar 876→197 行 + 3 个独立 dialog;Slave RegisterTable 1050→874 行 + 2 个 composable。/ Slave Toolbar 844→194 lines + 3 dialog components; Master Toolbar 876→197 lines + 3 dialogs; Slave RegisterTable 1050→874 lines + 2 composables.
- 🗨️ **Dialog 接入 i18n + 单例兜底** — `useDialog` 标题走 `t('dialog.alertTitle/...')`,旧未关 Promise 在新 dialog 打开时被 cancel,11 处 `inject(dialogKey)` 样板全部移除直接 `import { showAlert }`。/ `useDialog` titles now use i18n; any unresolved previous Promise is cancelled when a new dialog opens; 11 `inject(dialogKey)` boilerplate sites replaced with direct imports.
- 🪣 **LogCollector 改 `VecDeque`** — 满 buffer (10000 条) 时 `pop_front()` O(1) 取代原 `Vec::remove(0)` O(N);未安装 callback 时不再 clone entry。/ `LogCollector` now uses `VecDeque`: full-buffer eviction is O(1), and unobserved appends skip the clone.

### Added 新增

- 后端推送事件:`crates/modbussim-core::slave::RegisterChangeCallback` (`Arc<dyn Fn(&[RegisterChange])>`) + `SlaveConnection::set_change_callback`;TCP / RTU / ASCII / RTU-over-TCP / TLS 五条写入路径成功后通过 callback 发出。`crates/modbussim-core::log_collector::LogAppendCallback` 在 `add` / `add_blocking` / `try_add` 三条路径触发。/ Backend push events: a `RegisterChangeCallback` taking `&[RegisterChange]` is invoked from all five slave transport write paths after a successful write; `LogAppendCallback` fires from all three `add*` paths.
- Tauri commands 新增 emit:slave `start_slave_connection` 注入两个 callback,分别 emit `register-value-changed` (batched) 与 `log-appended`;master `connect_master` 注入 log-append callback emit `log-appended`;`crates/modbussim-app::commands::RegisterChangePayload` 与 `crates/modbusmaster-app::state::LogAppendedEvent` 新 DTO。/ Tauri commands now wire app-handle clones into both callbacks and emit `register-value-changed` / `log-appended`; new DTOs `RegisterChangePayload`, `LogAppendedEvent` exposed for the frontend.
- Shared-frontend 新增:`components/LogPanelShell.vue`(连接列表 + i18n + filter + listen + 合流)、`composables/useFcLabel.ts`(FC / 寄存器类型 i18n 标签)、`composables/useAddressFormat.ts`(`formatAddress(addr, mode)`),并新增 i18n keys `dialog.alertTitle/confirmTitle/promptTitle`、`formats.*`、`fc.*`。/ Shared-frontend gains `LogPanelShell`, `useFcLabel`, `useAddressFormat` composables, plus new i18n keys for dialog titles, value formats, FC labels.
- Slave 端新组件:`MutationControl.vue` / `NewConnectionDialog.vue` / `NewSlaveDialog.vue` 从 Toolbar 拆出。Composables `useRegisterFormat.ts`(`formatU16` / `formatTypedValue` / `formatFloatPair` / `encodeTypedValue`)与 `useRegisterValues.ts`(load / refresh / `register-value-changed` listen / `loadDirtyKeys` race guard)从 RegisterTable 抽出。/ Slave gets `MutationControl`, `NewConnectionDialog`, `NewSlaveDialog` carved out of Toolbar, plus `useRegisterFormat` and `useRegisterValues` composables out of RegisterTable.
- Master 端新组件:`NewConnectionDialog.vue` / `NewScanGroupDialog.vue` / `WriteDialog.vue` 从 Toolbar 拆出。/ Master gets `NewConnectionDialog`, `NewScanGroupDialog`, `WriteDialog` carved out of Toolbar.
- `crates/modbussim-core::parse::register_type_to_str` 反向函数,与已有 `parse_register_type` 配对。/ `parse::register_type_to_str` companion to `parse_register_type`.

### Changed 改进

- `LogCollector` 内部存储 `Vec<LogEntry>` → `VecDeque<LogEntry>`;`add` / `add_blocking` / `try_add` 重写为先 snapshot callback Arc(单次锁),无 callback 时跳过 clone。/ `LogCollector` storage switched to `VecDeque`; `add*` methods snapshot the callback once and skip cloning the entry when no callback is installed.
- `useRegisterValues::loadRegisters` 增加 `loadSeq` race guard 与 `loadDirtyKeys` 集合 — load 期间到达的 push event 被记录,load 完成时不被快照覆盖。/ `useRegisterValues::loadRegisters` now snapshots `loadSeq` and tracks `loadDirtyKeys`, so push events arriving during a load are not clobbered by the snapshot.
- `RegisterTable::commitEdit` 抽 `applyWrites(register_type, [[addr, value], ...])` helper,3 路 try/catch + cache write + emitSelection 重复结构合并;新增 `isBitType(rt)` 替代多处 `rt === 'coil' || rt === 'discrete_input'`。/ `RegisterTable::commitEdit` extracts `applyWrites` and `isBitType` helpers, collapsing three duplicated try/catch + cache + emit blocks.
- `LogPanelShell::scheduleReload` 合流逻辑由 `pendingReload` + do/while 简化为单一 `reloadInFlight` guard(每次 fetch 已为全量,二次 fetch 无意义)。/ `LogPanelShell::scheduleReload` collapses the pending+do/while coalescing pattern into a single `reloadInFlight` guard.
- 所有 11 处 `inject<{ showAlert: ... }>(dialogKey)!` 样板替换为 `import { showAlert } from 'shared-frontend'`;`App.vue` 中的 `provide(dialogKey, ...)` 一并删除。/ All 11 `inject(dialogKey)` boilerplate sites replaced with direct imports of `showAlert`/`showConfirm`/`showPrompt`; the `provide(dialogKey, ...)` calls in both `App.vue`s removed.
- `useDialog::open` 对未 resolve 的旧 Promise 调 cancel 路径,避免悬挂;`title` 改走 i18n,`AppDialog` 按钮文案接 `t('common.cancel')` / `t('common.ok')`。/ `useDialog::open` cancels any unresolved previous Promise before opening a new dialog; titles now go through i18n, and `AppDialog`'s buttons localise via `t('common.cancel')` / `t('common.ok')`.
- `useLogPanel` 解耦 Tauri 命令名:接受 `LogPanelDataSource = { fetchLogs, clearLogs, exportCsv }` 注入,不再硬编码 `get_communication_logs` / `clear_communication_logs` / `export_logs_csv`。/ `useLogPanel` now takes a `LogPanelDataSource` injection instead of hardcoding Tauri command names.
- `RegisterTable` `formatRegType` 与 `formatOptions` 字符串硬编码改走 i18n(`fc.*`、`formats.*`)。/ `RegisterTable`'s register-type and value-format dropdown labels routed through i18n.

### Removed 移除

- 删除 2-second `setInterval` 轮询:`frontend/src/components/RegisterTable.vue` 与 `LogPanel.vue`、`master-frontend/src/components/LogPanel.vue` 三处。改为 listen 事件 + 必要时 `refreshKey` 触发 bulk refresh。/ Removed three `setInterval(..., 2000)` polling loops; replaced by event listeners + on-demand `refreshKey`-driven bulk refresh.
- 删除 `frontend/src/composables/useDialog.ts` 与 `master-frontend/src/composables/useDialog.ts` 两个无意义 re-export 转发壳;`shared-frontend::useDialog` 中未使用的 `dialogKey` 导出删除。/ Removed both `useDialog.ts` re-export shells and the unused `dialogKey` export.
- 删除未引用资源:`frontend/src/components/HelloWorld.vue`、`ToolsView.vue`,以及孤立的 `frontend/src/assets/{hero.png, vite.svg, vue.svg}`。/ Deleted unused `HelloWorld.vue`, `ToolsView.vue`, and orphaned hero/Vite/Vue logo assets.

### Fixed 修复

- master `LogPanel.vue` 自动刷新里硬编码的 `'zh-CN'` locale 改走 `useI18n().locale`;之前写到 `error` ref 后从不显示的问题在 `LogPanelShell` 中以右上角 `!` 角标 + tooltip 修复。/ Hardcoded `'zh-CN'` locale in master `LogPanel.vue` replaced with `useI18n().locale`; the previously dropped `error` ref now surfaces as a `!` badge with tooltip in `LogPanelShell`.
- master `NewConnectionDialog.vue` 删除从未触发的 `(e: 'request-scan'): void` 与多余的 `connectionId?: string` emit 类型声明。/ Removed never-emitted `request-scan` and unused `connectionId` parameter from master `NewConnectionDialog.vue`'s emit declarations.
- Slave Toolbar `random_mutate_registers` 残留 `console.debug` 日志移除。/ Removed leftover `console.debug` from slave Toolbar mutation handler.

### Internal 内部

- 重构覆盖 47 个文件、净减约 2000 行;两端 `vue-tsc` 类型检查 + `vite build` 全绿;`shared-frontend` `vitest` 16/16 通过;`cargo test --workspace` 276/276 通过。/ Refactor touches 47 files with a net ~2000-line reduction; both frontends pass `vue-tsc` + `vite build`; `shared-frontend` vitest 16/16, `cargo test --workspace` 276/276.
- `changes_from_tokio_request` / `changes_from_modbus_request` 多写入变体预分配 `Vec::with_capacity(2 * values.len())`,避免重复 reallocation。/ Multi-write variants of the change-extraction helpers now pre-size the result vector.
- `start_slave_connection` 中 callback 捕获改用 `Arc<str>` 而非反复 `String::clone()`,降低每事件分配。/ Callbacks in `start_slave_connection` now capture connection ids as `Arc<str>` instead of cloning a `String` per event.

---

## [0.14.1] - 2026-05-01

补丁版本:把 v0.14.0 hotfix 引入的 `SlaveDevice::apply_random_mutation_thread` 在 Tauri 子站 `random_mutate_registers` 命令中真正用上,删除 75 行重复实现;新增 6 个单元测试钉住四类寄存器变异行为。无破坏性变更。

Patch release: Tauri slave's `random_mutate_registers` command now actually calls the core `SlaveDevice::apply_random_mutation_thread` API introduced as a v0.14.0 hotfix, removing 75 lines of duplicated logic; six new unit tests pin the mutation behaviour for all four register types. No breaking changes.

### Highlights / 亮点

- ♻️ **Tauri slave 复用 core 变异 API** — `commands.rs::random_mutate_registers` 由 75 行就地实现改为单行 `device.apply_random_mutation_thread(&types)`,行为与 egui 子站完全一致。/ Tauri slave's mutation command shrinks from 75 lines to a single core call, matching egui slave behaviour exactly.
- 🧪 **变异行为单元测试覆盖** — 新增 `tests/random_mutation.rs`:6 个用例钉住 Coil / DiscreteInput / HoldingRegister / InputRegister 都会真正变化,empty defs 返回 0 不 panic;workspace 测试达 280 / 0 失败。/ New `tests/random_mutation.rs` with 6 cases proving all four register types actually mutate (the original "FC03/FC04 不变化" report) and empty defs return 0 without panicking; workspace test count reaches 280, 0 failures.
- 🩺 **后端诊断日志** — `random_mutate` 命令现在打印每类型 addr 计数 + 实际变异数,前端 invoke 也打 `console.debug`,排查"变异请求来了但 UI 没刷"类问题不再靠猜。/ Both backend (`log::debug!`) and frontend (`console.debug`) now record per-type address counts and actual mutation counts, removing the guesswork when diagnosing silent mutation requests.
- 📜 **项目级 CLAUDE.md 入库** — `.claude/CLAUDE.md` 加入仓库:Think Before Coding / Simplicity First / Surgical Changes / Goal-Driven Execution 四条 LLM 协作准则,所有协作者共享同一基线。/ Project-level `.claude/CLAUDE.md` is now checked in, sharing the four LLM-collaboration guidelines with every contributor.

### Changed 改进

- `crates/modbussim-app/src/commands.rs::random_mutate_registers` 删除 ~75 行就地变异逻辑(coils / discrete inputs / holding / input 各自手写 RNG + delta clamp),改调 core `apply_random_mutation_thread`,行为可被 `random_mutation.rs` 测试覆盖。/ `commands.rs::random_mutate_registers` drops ~75 lines of in-place mutation logic in favour of the core helper; behaviour is now testable via `random_mutation.rs`.
- `frontend/src/components/Toolbar.vue::scheduleMutation` invoke 增加 `<number>` 类型标注 + `console.debug` 日志(types + 实际变异数)。/ `scheduleMutation` now types the invoke as `<number>` and logs the mutation count to dev console.

### Tests 测试

- 新增 `crates/modbussim-core/tests/random_mutation.rs`(6 cases):`coil_actually_flips` / `discrete_input_actually_flips` / `holding_register_actually_changes_after_iterations` / `input_register_actually_changes_after_iterations` / `mixed_types_all_change` / `empty_defs_returns_zero_no_panic`。/ Six unit tests in the new `random_mutation.rs` file cover every register type plus the empty-defs no-op edge case.
- 全量 `cargo test --workspace`:**280 通过 / 0 失败**(此前 274 + 本版本 6)。/ Workspace test count is now **280 passing, 0 failing** (previous 274 + 6 new).

### Internal 内部

- `.claude/CLAUDE.md` 入库为项目级 LLM 行为指引(`settings.local.json` / `commands/` / `skills/` 仍为个人本地配置,继续不入库)。/ `.claude/CLAUDE.md` is now version-controlled while local skill / command / settings files remain untracked.

---

## [0.14.0] - 2026-04-28

自 `v0.13.0` 起的大版本更新:Slave UI 整体冷蓝重构、TLS 支持、egui 双端全面 i18n、空状态 Hero 三色弦动画、端到端联动测试覆盖扩展到 10 场景。无破坏性变更。

A large feature drop since `v0.13.0`: full slave-UI cool-blue redesign, TLS support, end-to-end i18n across both egui apps, an empty-state Hero animation, and master/slave E2E coverage extended to 10 scenarios. No breaking changes.

### Highlights / 亮点

- 🎨 **Slave UI 整体重构** — 冷蓝 palette、shadcn 迁移、SidePanel 三段式 240px、状态栏脉动 ●、寄存器表格色彩语义化、按钮层级平衡。/ Slave UI redesigned end-to-end: cool-blue palette, shadcn migration, three-section 240px sidebar, pulsing status dot, semantic register table colours.
- 🔒 **Slave TLS 支持** — 新建对话框新增 TLS 选项,`Transport::TcpTls` 全链路打通,配置可持久化。/ Slave gains TLS in the new-connection dialog (`Transport::TcpTls`), with persisted config.
- 🌐 **Master + Slave egui 全面接入 i18n** — 菜单、侧栏、状态栏、运行时错误串均通过 `tr/tr1`,中英文即时切换并随 eframe 持久化。/ Both egui apps now run through `tr/tr1` end-to-end (menu, sidebar, status bar, runtime error strings); language switch is live and persisted.
- 🎭 **Slave 空状态 Hero 动画** — 三色弦 `paint_dancing_strings` + 心跳采样 `show_welcome_hero`,首次启动不再是空白页。/ New empty-state Hero: three-string dancing animation with heartbeat sampling — no more blank welcome screen.
- 🧪 **端到端覆盖扩展到 10 场景** — `e2e_flow.rs` 新增 7 个非交互式 `#[tokio::test]`(异常码全谱、连接生命周期、多设备路由、多 ScanGroup 并行、主站扫描器、随机变异传播、并发读写),共 274 测试 / 0 失败。/ `e2e_flow.rs` grows from 3 to 10 non-interactive scenarios covering exception codes, lifecycle, multi-device routing, parallel scan groups, scanners, mutation propagation and concurrent r/w. Workspace now at 274 tests, 0 failures.
- ♻️ **master-egui `update()` 抽方法重构** — 460 行 → 190 行,拆出 menu / sidebar / status / 3-tab 共 6 个 render 方法。/ `master-egui::update()` shrinks from ~460 to ~190 lines via per-section render methods.

### Added 新增

- `master-egui` 新增中英文菜单项 + Lang 持久化 (`lang_v1`),i18n 键扩展 ~70 条覆盖 conn / read / write / poll / result 各模块。/ `master-egui` gains a Language menu and `lang_v1` persistence; ~70 new i18n keys across conn/read/write/poll/result modules.
- `slave-egui` 新增中英文切换,完整菜单 + 侧栏 + 错误串国际化。/ `slave-egui` adds language switching with fully translated menu, sidebar and error toasts.
- `modbussim-ui-shared::hero_anim::show_welcome_hero` — 三色弦欢迎屏 helper,Slave / Master 共享。/ Shared `show_welcome_hero` helper drives the new empty-state animation.
- `modbussim-core::LogCollector::try_count_within` — 非阻塞时间窗计数,用于状态栏脉动 ● 强度采样。/ Non-blocking time-window counter on `LogCollector`, feeding the pulse dot intensity.
- `slave-app` 新建对话框 TLS 选项 + `Transport::TcpTls` 持久化字段。/ TLS option in slave's new-connection dialog plus `Transport::TcpTls` persistence.
- `slave-app::danger_button_sm` helper + `pending_delete` 二次确认字段(footer 删除连接 3 秒确认)。/ `danger_button_sm` helper and a 3-second confirmation pattern for connection deletion.
- `crates/modbussim-core/tests/e2e_flow.rs` 新增 7 个非交互式 E2E 测试,自带带时间戳的 `step!` 日志宏。/ Seven new non-interactive `#[tokio::test]` cases in `e2e_flow.rs`, with a timestamped `step!` logging macro.

### Changed 改进

- `master-egui::update()` 函数体抽 6 个方法:`render_menu_bar` / `render_sidebar` / `render_status_bar` / `render_read_tab` / `render_write_tab` / `render_poll_tab`。/ `master-egui::update()` decomposed into six render methods.
- `master-egui` 模块化拆分:新建 `events.rs` (`UiEvent`)、`scan_group.rs` (`ScanGroupUi`)、`result_table.rs`(读结果表渲染)。/ `master-egui` modularised into sibling files for events, scan groups and result tables.
- 子站运行时错误改走 i18n — 引入 `UiEvent::ErrorKey { key, arg } / InfoKey`,`drain_events` 阶段调 `tr1(self.lang, key, arg)`,避免在 async spawn 里捕获 `lang`。/ Slave runtime errors now flow through `UiEvent::ErrorKey/InfoKey`; `tr1` is invoked at drain time, eliminating `lang` capture in async spawns.
- 整套主题切换为冷蓝 palette,字号梯度拉开,新增语义色 token,`shadcn-egui` 替换原有 button/frame 样式。/ Theme switched to a cool-blue palette with widened font scale, new semantic colour tokens, and `shadcn-egui` replaces the old button/frame styling.
- `slave-app` SidePanel 重构为 240px 三段式(头/树/footer),树节点按钮简化为单按钮 + 状态色 icon,删除连接按钮挪到 footer。/ Slave sidebar rebuilt as a three-section 240px panel; tree nodes simplified to a single coloured-icon button; connection deletion moved to the footer.
- 状态栏从静态文字改为脉动 ● + 三态文案(运行中 / 已停止 / 未连接),色彩与 LogCollector 速率联动。/ Status bar evolves from static text to a pulsing dot with three-state labels, intensity driven by the LogCollector rate.
- 寄存器表格:`fmt-pill`、按类型语义化背景色、表头 `tiny_caps`、关闭 `striped`,值解析可隐藏(V/L/Esc 快捷键)。/ Register table now uses `fmt-pill`, semantic per-type background, `tiny_caps` headers, no zebra-striping, and a hideable value-parse pane (V / L / Esc).
- Log panel 改为单行 header 可折叠,RX/TX 改用箭头符号。/ Log panel header collapsed to a single line; RX/TX rendered with arrow glyphs.

### Fixed 修复

- `master-app` 补齐 `TcpSpec { tls: None }` 字段,修复 CI 上的 `E0063` 编译错误。/ `master-app` now sets `TcpSpec { tls: None }`, fixing the CI `E0063` compile failure.
- `slave-app` data source runner 补齐 `Coil` / `DiscreteInput` 写入分支(原来只覆盖 HR/IR)。/ Slave data-source runner now handles `Coil` / `DiscreteInput` writes (previously HR/IR only).
- Jitter:零值 holding/input 寄存器不再被推动,整数除法时保底 ±1。/ Jitter no longer perturbs zero-valued holding/input registers; integer division has a ±1 floor.

### Tests 测试

- `e2e_flow.rs` 从 3 场景扩展到 10 场景,719 行,覆盖完整 FC01-04 / FC05/06/15/16 + 异常码 + 生命周期 + 多设备 / 多 ScanGroup + 主站扫描器 + 变异传播 + 并发读写。/ `e2e_flow.rs` grows from 3 to 10 scenarios (719 lines) covering all FCs, exception codes, lifecycle, multi-device, parallel scan groups, scanners, mutation propagation and concurrent r/w.
- `slave-app::amp_from_counts` 归一化边界单测。/ Boundary tests for `slave-app::amp_from_counts` normalisation.
- 工作区测试总数 274 / 0 失败。/ Workspace test total: 274 / 0 failures.

### Internal 内部

- `Frame::none` → `Frame::new`(egui 0.33 deprecated 迁移)。/ Migrated `Frame::none` → `Frame::new` (egui 0.33 deprecation).
- 多次 `cargo fmt --all` 整体规整。/ Repeated `cargo fmt --all` housekeeping.
- 新增 `docs/superpowers/specs` 与 `openspec` 流程产物归档。/ Spec/plan artefacts archived under `docs/superpowers/specs` and `openspec`.

---

## [0.13.0] - 2026-04-20

详见 git tag 与提交历史。/ See git tag and commit history.

[0.14.1]: https://github.com/kelsoprotein-lab/ModbusSim/releases/tag/v0.14.1
[0.14.0]: https://github.com/kelsoprotein-lab/ModbusSim/releases/tag/v0.14.0
[0.13.0]: https://github.com/kelsoprotein-lab/ModbusSim/releases/tag/v0.13.0

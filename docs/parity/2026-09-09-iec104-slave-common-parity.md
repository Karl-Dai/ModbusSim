# Modbus 从站与 IEC104 子站公共能力对照

对照对象：`../IEC60870-5-104-Simulator` 当前工作区（HEAD `dba100b`）。本次修改集中在 Modbus 从站及其公共前端组件；104 仓库仅用于读取参考。原有 Modbus 主站通信设置等未提交改动保留。

## 实现矩阵

| 公共部分 | IEC104 对照入口 | Modbus 实现 |
| --- | --- | --- |
| 公共视觉规范 | `shared-frontend/styles/{tokens,transitions,toolbar}.css` | 将相同样式移入 `shared-frontend/src/styles`；从站移除 Vite 初始模板样式；现有组件改用统一颜色变量 |
| 三栏布局 | `frontend/src/App.vue`、`Splitter.vue` | 树、值面板、日志尺寸可调整并记忆；窗口缩小时限制尺寸；分隔条支持方向键和 Home/End |
| 分组工具栏 | `frontend/src/components/Toolbar.vue`、`ToolbarMenu.vue` | 配置、新建、寄存器、设置、工具、帮助菜单；当前/全部启停；窄窗口操作区可横向滚动，右侧状态区保留 |
| 菜单交互 | `ToolbarMenu.vue` | 复用菜单组件，支持键盘方向键、Escape、焦点恢复和视口内定位 |
| 工程文件 | `useConfigActions.ts` | 保留打开/保存/另存为，增加通过路径打开、Cmd/Ctrl+O、Cmd/Ctrl+S、Shift+保存；加载后重置工作区视图 |
| 连接管理 | `ConnectionTree.vue`、`ServerSettingsModal.vue` | 保留单个连接/从站管理；增加连接批量选择删除、全部启停的进度与失败报告；停止后可编辑监听/串口参数，保留设备与寄存器 |
| 客户端连接 | `ClientConnectionsModal.vue` | 保留本会话上一轮新增的数量、对端 IP/端口、连接时间、连接/断开刷新；TCP/TLS/RTU-over-TCP 均跟踪真实连接 |
| 表格选择 | `DataPointTable.vue`、`MultiSelectActions.vue` | 保留虚拟表格、Shift/Ctrl 多选、列宽记忆；增加全选、复制、Escape 清选、批量赋值/删除和显式操作栏 |
| 元数据编辑 | `DataPointModal.vue` | 接通既有 RegisterModal 的编辑模式，编辑地址、类型、名称、注释等；修复 remove_register 未注册命令的问题 |
| CSV 工作流 | `CsvImportModeModal.vue`、`usePointCsvActions.ts` | 模板、导入预校验、追加（重复整批拒绝）、替换、导出；使用系统保存对话框和后端写文件。替换需要停止连接且明确确认 |
| CSV 数据边界 | IEC104 点表 CSV | Modbus CSV 存储寄存器定义、名称、注释、仿真和数据源配置，不存运行时值。追加保留已有值；替换重置值与对应仿真运行状态 |
| 校验与兼容 | 导入/配置错误提示 | CSV 支持 UTF-8 BOM、引号、逗号、多行字段；检查类型、地址边界和占用范围重叠。后端先校验完整集合再修改；兼容 uint16/uint32 与旧 u_int16/u_int32 序列化名称，旧写出格式不变 |
| 仿真设置 | `SimulationSettingsDrawer.vue` | 保留原有 Modbus 仿真/数据源能力，统一样式和弹窗焦点；新增设置菜单入口及所选寄存器批量入口；移除顶部变位总开关，点位配置直接启用，表格独立刷新变位数值 |
| 通信日志 | `frontend/src/components/LogPanel.vue` | 保留连接/方向/功能码/关键字筛选；增加暂停刷新、自动滚动、复制筛选结果、导出筛选结果、拖动/键盘调列宽；防止旧请求回写新连接或已重置工作区 |
| 工具入口 | `ParseFrameDialog.vue` | 将“工具待实现”替换为 Modbus 完整帧解析、CRC16/LRC、PLC 地址转换。校验 MBAP 长度/协议号、RTU CRC、ASCII LRC；请求与响应分别解析 |
| 弹窗与可访问性 | 公共弹窗、抽屉 | 引入 ModalShell 和统一焦点指令，支持 Escape、Tab 焦点约束、关闭后焦点恢复；保留嵌套确认框；新建连接失败时保留表单 |
| 关于与帮助 | `AboutDialog.vue` | 增加独立关于窗口、版本、使用文档、版本记录与项目链接；保留统计开关并显示读取/保存失败 |
| 国际化 | 中英文词典 | 新增内容同时提供中英文；自动扫描从站 literal 翻译键，防止原样显示键名 |
| 更新 | `UpdateDialog.vue` | 统一视觉，保留已有 Modbus 更新进度、超时、本地化、签名验证逻辑，不改变更新端点或发布配置 |

## 协议与复用边界

- 104 的总召/计量召唤、STARTDT/STOPDT、遥控选择/执行、品质位、COT/CA/IOA、时标、k/w/t1/t2/t3 等属于 IEC104 协议语义，Modbus 使用功能码、Unit ID、寄存器地址和串口参数。
- 解析工具支持当前模拟器的主要功能码 FC01–06、FC15、FC16 和异常响应；不解密 TLS 流量，也不将原始 104 报文解析器用于 Modbus。
- 日志展示保留 Modbus 现有请求/响应摘要。104 专有帧分类和字段解释不直接移植；本次没有增加完整原始线缆字节的抓取链路。
- 已有连接的设置面板编辑监听/串口参数，保持原传输类型和 TLS 配置。TLS 证书仍在新建连接/项目文件中配置；不提供运行中热切换证书或协议类型。
- 本次将可复用的样式、分隔条、菜单、弹窗焦点管理、日志交互放入本仓库公共前端，未建立跨 Git 仓库的运行时依赖。

## 验证记录

- 从站与主站前端 TypeScript/Vite 生产构建通过。
- `cargo clippy -p modbussim-core -p modbussim-app --lib -- -D warnings` 和 `git diff --check` 通过。
- 公共前端 25 项测试通过：CSV 转义/边界/重叠、旧日志请求丢弃、中英文键覆盖，以及已有本地化/更新进度测试。
- 从站 Rust 单元测试 12 项通过，包含报文解析、传输参数校验、CSV 原子校验与旧名称兼容。
- 核心真实协议测试 16 项通过：连接生命周期 2 项、从站 TCP 协议集成 12 项、TLS 端到端 2 项。
- 浏览器真实页面已检查：整体布局、菜单、工具弹窗、Escape 与焦点恢复、键盘调节分栏、中英文切换。浏览器预览没有 Tauri 后端，未把它视作原生 IPC 的端到端验证。
- 标准 Tauri debug `.app` 已构建并启动。本轮桌面窗口访问返回 `cgWindowNotFound`，因此新的原生窗口交互、系统文件选择/保存对话框及完整 CSV UI 往返仍待桌面可访问时核验。
- 本会话上一轮已通过真实原生窗口验证两个 TCP 客户端的数量、对端地址、通信响应与断开刷新。本轮没有将这份旧证据扩展成对全部新 UI 的验证。

## 交付状态

本对照涉及的改动纳入 v0.17.3 发布准备。应用包位于 `target/debug/bundle/macos/ModbusSlave.app`；它是本地验证构建，不覆盖 `/Applications` 下的已安装版本。

## v0.17.3 发布前补充验证

- Rust 工作区检查、所有目标 Clippy（拒绝 warning）、311 项 Rust 单元/集成测试通过。
- 两端生产构建、31 项前端测试和 20 项发布脚本测试通过；依赖审计无 high/critical 问题。
- 使用独立无头 Chromium 与测试用 Tauri bridge 数据，检查真实页面的客户端详情、中文分组、变位刷新、CSV 预览，以及 800×600 下菜单和工具弹窗的定位/裁剪。截图留在本地 output/playwright，未加入发布源码。
- 无头测试验证浏览器布局与交互；真实协议链路由 Rust 集成测试覆盖。系统原生文件对话框不计入这次无头验证。

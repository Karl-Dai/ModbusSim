# Go/Wails migration / Go/Wails 迁移

v0.17.9 changes both desktop hosts and the protocol implementation to Go/Wails. Rust/Tauri sources are retained in earlier Git tags, rather than in the active build.

v0.17.9 将主从站桌面框架和协议实现统一为 Go/Wails。旧 Rust/Tauri 源码可在历史 Git 标签中查阅，当前构建不再包含它们。

## Installation / 安装

Download the correct macOS Universal, Windows x64/ARM64 or Linux x64 bundle from Releases, verify its SHA256SUMS entry and install it manually. Close the previous app before replacing it. The previous Tauri updater cannot install the new bundle format. The Help menu opens Releases for later upgrades.

从 Release 下载对应平台包，核对 SHA256SUMS 后手动安装。替换前先关闭旧应用；旧 Tauri 更新器无法安装新包格式。后续升级可通过帮助菜单打开 Release 页面下载。

macOS requires version 13+, Linux requires GTK3/WebKitGTK 4.0, and Windows requires WebView2. Application identifiers and icons are preserved.

macOS 要求 13+；Linux 需要 GTK3/WebKitGTK 4.0；Windows 需要 WebView2。应用标识与图标沿用原版。

## Project files / 工程文件

Open an existing v1 `.modbusproj` in the matching Go app. Its devices, registers, scan groups and settings are imported. Save the migrated project to a new file: Go writes `Version: 2` with PascalCase fields. Keep the original file for rollback; the older Rust version cannot read v2 exports.

在对应的 Go 应用中打开 v1 `.modbusproj`，导入设备、寄存器、扫描组和配置。请另存为新文件：Go 使用 PascalCase 字段和 `Version: 2`。保留原文件以便回退，旧 Rust 版无法读取 v2 导出。

## Verification / 验证范围

Automated tests cover TCP, TLS/mTLS, RTU-over-TCP, SOCKS5, serial framing, project conversion, polling, simulation and writes. Release builds cover all published targets. Physical serial-device acceptance still requires real hardware.

自动测试覆盖 TCP、TLS/mTLS、RTU-over-TCP、SOCKS5、串口帧、工程转换、轮询、仿真和写入；发布构建覆盖全部发布平台。真实串口设备验收仍需实际硬件。

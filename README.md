<div align="center">

# 🔌 ModbusSim

**A cross-platform Modbus simulator — Slave _and_ Master, in one desktop toolkit.**

[![Release](https://img.shields.io/github/v/release/Karl-Dai/ModbusSim?label=release&color=2ea043)](https://github.com/Karl-Dai/ModbusSim/releases)
[![Downloads](https://img.shields.io/github/downloads/Karl-Dai/ModbusSim/total?color=1f6feb&cacheSeconds=3600)](https://github.com/Karl-Dai/ModbusSim/releases)
[![Stars](https://img.shields.io/github/stars/Karl-Dai/ModbusSim?color=e3b341)](https://github.com/Karl-Dai/ModbusSim/stargazers)
[![License: MIT](https://img.shields.io/badge/License-MIT-lightgrey.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20·%20macOS%20·%20Linux-informational)]()

Built with **Go** · **Wails 2** · **Vue 3**

**English** · [中文](README_CN.md)

![ModbusMaster polling a simulated slave over TCP](docs/screenshots/tut-3-master-data.png)
<br>
<sub>Slave register simulation → scan-group polling → random mutation → wire-level logs</sub>

</div>

---

## Why this project

Testing a Modbus integration usually means wiring up a real PLC or borrowing a master station. This project puts **both ends on your desktop**:

- 🛰️ **Slave & Master in one repo** — simulate a field device, or drive one, with no external hardware.
- 🔌 **Five transports, one core** — TCP, TCP+TLS, RTU, ASCII and RTU-over-TCP, covering function codes FC01–FC06 / FC15 / FC16.
- 📈 **Built-in data simulation** — every register can run its own data source (Fixed / Random / Sine / Sawtooth / Triangle / Counter / CSV playback) or an independent periodic mutation schedule; 20,000+ registers with virtual scrolling.
- 🌐 **Network reach** — route Master TCP, TCP+TLS and RTU-over-TCP connections through a SOCKS5 proxy with optional authentication and proxy-side DNS.
- 🖥️ **Native desktop app** — small Go + Wails binaries for Windows, macOS and Linux, with standalone desktop bundles and manual release downloads.
- 🌏 **Bilingual UI** — full English / 简体中文, switchable at runtime.

## Table of Contents

- [Screenshots](#screenshots)
- [Features](#features)
- [Download](#download)
- [Supported Function Codes](#supported-function-codes)
- [Transport Modes](#transport-modes)
- [Build from Source](#build-from-source)
- [Quick Start (Tutorial)](#quick-start-tutorial)
- [Architecture](#architecture)
- [Contributing](#contributing)
- [Changelog](#changelog)
- [macOS First Launch](#macos-first-launch)
- [License](#license)

## Screenshots

**Slave · 20,000 registers with live random mutation**

ModbusSlave runs a real Modbus TCP server on `0.0.0.0:502` with two slave devices. The register table virtual-scrolls through 20,000+ holding registers; **Random Mutation** changes values in place (orange flashes) and the value panel on the right decodes the selected register as Signed / Unsigned / Hex / Binary at once.

![ModbusSlave with running server, mutating registers and value panel](docs/screenshots/tut-1-slave.png)

**Master · New Connection dialog**

One dialog covers every transport — TCP (with optional TLS), RTU serial, ASCII serial or RTU-over-TCP — with target address, port, slave ID and timeout. Network transports also accept a **SOCKS5 proxy** with optional username/password authentication and proxy-side DNS resolution, so the target is reachable even behind a jump host or an industrial gateway.

![New Connection dialog](docs/screenshots/tut-2-master-newconn.png)

**Master · scan groups fill the table**

Add scan groups (e.g. holding registers 0–59 and coils 0–31) with a custom poll interval per group and a per-group slave ID override. The connection tree shows each group's function code and range; the data table refreshes on every poll with the values served by the Slave, decoded as Unsigned / Signed / Hex / Binary / Float32.

![Master data table polling the slave](docs/screenshots/tut-3-master-data.png)

**Master · communication log with decoded TX/RX frames**

The bottom log panel records every request/response pair — direction, function code and a readable detail (`R 0 x60`, `60 regs`) side by side, filterable by direction / FC / text and exportable to CSV.

![ModbusMaster communication log with decoded frames](docs/screenshots/tut-4-master-log.png)

## Features

### 🛰️ ModbusSlave — Slave Simulator

- **Multi-transport** — TCP, TCP+TLS, RTU (serial), ASCII (serial), RTU-over-TCP
- **Modbus TCP over TLS** — TLS 1.2+ encryption, PEM and PKCS#12 certificate formats, optional mTLS (mutual authentication with client certificates)
- **Multiple slave devices** — create connections on any port, add multiple slave devices per connection
- **Four register types** — Coils (FC01), Discrete Inputs (FC02), Holding Registers (FC03), Input Registers (FC04)
- **Full protocol support** — Read (FC01–04), Write Single (FC05/06), Write Multiple (FC15/16), with Modbus exception codes
- **Register table** — address search/filter, inline value editing, Ctrl/Shift multi-select, virtual scrolling (20,000+ registers), multi-format display (Auto / U16 / I16 / Hex / Bin / Float32 with 4 byte orders)
- **Default initialization** — new slaves pre-fill addresses 0–20,000 across all four register types; batch-add supports up to 50,000 entries per operation
- **Value panel** — Signed/Unsigned/Hex/Binary (16-bit), Long/Float (32-bit), Double (64-bit), all byte orders (AB CD / CD AB / BA DC / DC BA)
- **Per-point data sources** — every register can run its own simulation source: Fixed, Random, Sine, Sawtooth, Triangle, Counter or CSV sequence playback; each point carries an independent update interval and wave parameters (amplitude / frequency / offset / phase / wave period), and the configuration round-trips through project save/load
- **Per-point periodic mutation** — right-click a register for single-point config, or multi-select rows and open the **Simulation Settings** drawer to apply one schedule (Flip / Increment / Decrement / Random) to every selected point at once; discrete points flip, analog points ramp between min/max bounds with a configurable step; the drawer lists all active simulations with mode, period, bounds and a **live current value**, each with per-row stop; the minimum period is 100 ms, all schedules run concurrently and independently on a single 100 ms backend tick, and settings persist with the project
- **Communication log** — real-time TX/RX logging with search, direction/function-code filtering, and CSV export
- **Project files** — save/load complete configurations as `.modbusproj` files for quick scenario switching
- **Serial port support** — auto-detect system serial ports, configurable baud rate, data bits, stop bits, parity

### 📡 ModbusMaster — Master Tool

- **Multi-transport** — TCP, TCP+TLS, RTU (serial), ASCII (serial), RTU-over-TCP
- **Modbus TCP over TLS** — TLS 1.2+ encryption, PEM and PKCS#12 certificate formats, accept-invalid-certs mode for self-signed certificate testing
- **SOCKS5 proxy** — route TCP, TCP+TLS and RTU-over-TCP connections through a SOCKS5 proxy (IPv4/IPv6) with optional username/password authentication and proxy-side DNS resolution; credentials are redacted from Go logs and the UI explains the RFC 1929 / project-file security boundary
- **Scan groups** — periodic polling with custom intervals per register group, per-group slave ID override
- **Device discovery** — slave ID scan (1–247), register address scan, auto-add discovered devices to scan groups
- **Multi-format data view** — Unsigned, Signed, Hex, Binary, Float32 (AB CD / CD AB), virtual scrolling
- **Write operations** — write single/multiple coils and registers (FC05/06/15/16)
- **Communication log** — TX/RX logging with search/filter (direction, function code, text) and CSV export
- **Auto-reconnect** — configurable reconnection with exponential backoff (1s → 2s → 4s → … → 30s max)
- **Project files** — save/load connection and scan group configurations
- **Connection-on-scan** — auto-prompt to scan devices after a successful connection
- **Version upgrades** — open Releases from Help and download the Go/Wails version manually.

### 🧩 Shared Architecture

- **Unified error system** — structured `ModbusError` with categorized error types (connection/protocol/application), serialized to JSON for frontend parsing
- **Shared Vue components** — common composables, types and i18n shared between both Wails apps via the `shared-frontend` npm workspace

## Download

Download ModbusSlave or ModbusMaster from the [latest Release](https://github.com/Karl-Dai/ModbusSim/releases/latest).

| Platform | Bundle |
|---|---|
| macOS 13+ (Apple Silicon / Intel) | Universal `.zip`; extract and move the `.app` to Applications |
| Windows x64 / ARM64 | Standalone `.exe` for the matching architecture; requires WebView2 |
| Linux x64 | `.tar.gz`; binary requires GTK3 and WebKitGTK 4.0 |

The Go/Wails release replaces Rust/Tauri and requires manual installation. Verify downloads with `SHA256SUMS` or `release-manifest.json`; the previous updater cannot install these bundles.

Legacy v1 projects can be imported. Go saves v2 projects that older Rust releases cannot read, so keep your original backup. See the [migration guide](docs/go-migration.md).

## Supported Function Codes

| Code | Function | Slave (Server) | Master (Client) |
|------|----------|:-:|:-:|
| FC01 | Read Coils | Read | Read/Poll |
| FC02 | Read Discrete Inputs | Read | Read/Poll |
| FC03 | Read Holding Registers | Read | Read/Poll |
| FC04 | Read Input Registers | Read | Read/Poll |
| FC05 | Write Single Coil | Write | Write |
| FC06 | Write Single Register | Write | Write |
| FC15 | Write Multiple Coils | Write | Write |
| FC16 | Write Multiple Registers | Write | Write |

## Transport Modes

| Mode | Transport | Framing | Use Case |
|------|-----------|---------|----------|
| TCP | TCP/IP socket | MBAP header | Standard Modbus TCP |
| TCP+TLS | TLS over TCP | MBAP header | Secure Modbus TCP (TLS 1.2+) |
| RTU | Serial port | Slave ID + CRC-16 | RS-485/RS-232 devices |
| ASCII | Serial port | `:` + hex + LRC + CRLF | Legacy serial devices |
| RTU-over-TCP | TCP/IP socket | Slave ID + CRC-16 | Industrial gateways |

## Build from Source

Install Go 1.27+, Node.js 22+ and the native Wails dependencies for your platform. Ubuntu 22.04 requires `libgtk-3-dev libwebkit2gtk-4.0-dev`; macOS requires Xcode Command Line Tools.

```sh
npm ci --ignore-scripts
npm run dev:slave:go
npm run dev:master:go
# Production bundles
npm run build:slave:go
npm run build:master:go
# Regenerate changed bindings
npm run bindings:slave:go
npm run bindings:master:go
# Validation
npm test --workspace shared-frontend
npm run test:go
```

The scripts use Wails v2.14.0 and supply the macOS CGO flags. See [Go development](go/README.md).

## Quick Start (Tutorial)

A full round-trip in six steps — drive the simulated Slave from the Master, no hardware required. (Screenshots show the Chinese UI; flip to English any time with the **中 / EN** toggle.)

> **Install first:** grab the installer for your platform from the [Releases page](#download), or run from source (`npm run dev:slave:go` / `npm run dev:master:go`). Open **both** `ModbusSlave` and `ModbusMaster`.

### Step 1 · Slave — create a server with registers

Open **ModbusSlave** and click **新建连接 (New Connection)**: pick TCP, a port (e.g. `502`) and random initialization — the server starts with a device pre-filled across all four register types (coils, discrete inputs, holding and input registers). Turn on **随机变化 (Random Mutation)** to make values move; click any row to decode it in the value panel.

![ModbusSlave with running server, mutating registers and value panel](docs/screenshots/tut-1-slave.png)

**Tip · default init & batch add**: new slaves pre-fill addresses 0–20,000 across all four register types, and batch-add handles up to 50,000 entries per operation.

### Step 2 · Master — create a connection

Open **ModbusMaster** and click **新建连接 (New Connection)**. The defaults already target the local Slave: address `127.0.0.1`, port `502`, slave ID `1`. Tick **启用 TLS (Enable TLS)** for a secure link. Click **创建 (Create)**, then **连接 (Connect)**.

![New Connection dialog](docs/screenshots/tut-2-master-newconn.png)

**Tip · SOCKS5 proxy**: on TCP, TCP+TLS and RTU-over-TCP, tick **Use SOCKS5 proxy** to route the connection through a proxy with optional username/password authentication and proxy-side DNS resolution — handy when the target sits behind a jump host or industrial gateway.

### Step 3 · Master — scan groups fill the table

Add scan groups (e.g. holding registers 0–59 and coils 0–31) and start polling. The connection tree shows each group with its function code and range; the table refreshes on every poll with the values served by the Slave.

![Master data table polling the slave](docs/screenshots/tut-3-master-data.png)

### Step 4 · Slave — drive values with data sources & mutation

Back on the Slave, make the registers move and watch them surface on the Master's next poll:

- **Right-click → 数据源 (Data Source)** on any register — pick Fixed, Random, Sine, Sawtooth, Triangle, Counter or CSV sequence playback, each with its own update interval and wave parameters.
- **Right-click → 模拟设置 (Open Simulation Settings)** on any row — or Ctrl/Shift multi-select several rows first — to apply one schedule (翻转 / 递增 / 递减 / 随机) to all selected points at once. The drawer also lists every active simulation with its live current value and a per-row stop; single-point tuning stays one right-click away via **周期变位 (Periodic Mutation)**.
- Both settings persist in `.modbusproj` project files, so a saved scenario replays identically after reload.

### Step 5 · Master — write back

Use **写入 (Write)** on the Master to send FC05/06/15/16 — single/multiple coils and registers. Confirm the change lands on the Slave's table (and in its communication log) before the next poll.

### Step 6 · Read the wire — decoded frames

Expand **通信日志 (Communication Log)** at the bottom: every TX/RX pair is decoded — direction, function code and a readable detail (`R 0 x60`, `60 regs`), filterable by direction / function code / text and exportable to CSV. The master's **auto-reconnect** attempts are all logged.

![Communication log with decoded frames](docs/screenshots/tut-4-master-log.png)

That's the full round-trip — server, registers, polling, simulation, write-back and wire-level inspection, all on your desktop.

## Architecture

```text
go/internal/             Protocols, transports, projects, simulation and state
go/cmd/slavegui/          Slave Wails desktop host
go/cmd/mastergui/         Master Wails desktop host
frontend/                Slave Vue frontend
master-frontend/         Master Vue frontend
shared-frontend/         Themes, translations and shared components
scripts/                 Go/Wails versioning and artifact validation
```

Go owns protocol behavior and application state; Vue calls generated Wails bindings. The former Rust implementation remains available in Git tags v0.17.8 and earlier.

## Contributing

Issues and pull requests are welcome. For a code change, please make sure `npm run test:go` passes (plus the frontend type checks) before opening a PR.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) or the [Releases page](https://github.com/Karl-Dai/ModbusSim/releases).

Starting from v0.16.0, both apps check GitHub Releases on startup and prompt to install new versions. Users on pre-0.16 builds need to upgrade manually once.

## macOS First Launch

The bundles are **not Apple-notarized** (no paid Developer Program). On first launch macOS shows *"ModbusSlave / ModbusMaster cannot be opened — Apple could not verify…"* with only *Done* and *Move to Trash* buttons. This is the standard macOS 15 (Sequoia) block for ad-hoc-signed apps — the app is **not damaged**.

<details>
<summary><b>How to allow it (pick one)</b></summary>

**1. GUI path**

- Double-click the `.app`, see the block dialog, click *Done*.
- Open *System Settings → Privacy & Security*, scroll to the bottom.
- You'll see *"ModbusSlave was blocked…"* — click *Open Anyway* and enter your password.
- The next dialog has an *Open* button; click it. Subsequent launches go straight through.

**2. One-line Terminal**

```bash
xattr -dr com.apple.quarantine "/Applications/ModbusSlave.app"
xattr -dr com.apple.quarantine "/Applications/ModbusMaster.app"
```

Strips the quarantine flag so macOS stops blocking.

</details>



## License

[MIT](LICENSE)

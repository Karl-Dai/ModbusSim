#!/usr/bin/env node
// Generate a richly formatted GitHub Release body for a given tag.
// Usage: node scripts/build-release-notes.mjs <tag>     # writes RELEASE_BODY.md
//
// Produces:
//   - Header (version + date)
//   - Per-OS download table (slave / master)
//   - Verbatim CHANGELOG section for the version
//   - Footer with links to full changelog + previous releases
//
// Designed to be called from CI after build jobs upload assets, so the
// rendered table always lines up with what's actually in the release.

import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { extractChangelogSection } from './gen-update-manifest.mjs'

const REPO = 'Karl-Dai/ModbusSim'
const REPO_URL = `https://github.com/${REPO}`

const MACOS_FIRST_LAUNCH_NOTE = [
  '## macOS 首次启动 / First launch on macOS',
  '',
  '首次双击 `.app` 会被 Gatekeeper 拦截("Apple 无法验证…")。放行:打开',
  '*系统设置 → 隐私与安全性*,滚到底点 *仍要打开*;或终端执行',
  '`xattr -dr com.apple.quarantine "/Applications/ModbusSlave.app"`。',
  '',
  'First launch is blocked by Gatekeeper ("Apple could not verify…"). To allow:',
  '*System Settings → Privacy & Security*, scroll to bottom, click *Open Anyway*; or',
  'run `xattr -dr com.apple.quarantine "/Applications/ModbusSlave.app"` in Terminal.',
].join('\n')

// Go/Wails 版没有内置 updater(应用内只跳转到 Releases 页),
// 引导国内用户直接走镜像下载。
const CN_MIRROR_BANNER = [
  '> 🇨🇳 **中国大陆用户**:GitHub 下载缓慢时可走镜像加速:',
  '>',
  `> <https://ghfast.top/${REPO_URL}/releases/latest>`,
  '>',
  '> 🌍 **Users in mainland China**: use the mirror above when GitHub downloads are slow.',
].join('\n')

const APPS = [
  { role: 'Slave (从站)', prefix: 'ModbusSlave' },
  { role: 'Master (主站)', prefix: 'ModbusMaster' },
]

// Asset names must match the Wails build matrix in .github/workflows/release.yml.
const PLATFORMS = [
  { label: 'macOS (Universal, zip)', file: (p, v) => `${p}_${v}_macOS-universal.zip` },
  { label: 'Windows x64 (exe)',      file: (p, v) => `${p}_${v}_windows-x64.exe` },
  { label: 'Linux x64 (tar.gz)',     file: (p, v) => `${p}_${v}_linux-x64.tar.gz` },
]

export function buildBody(tag, changelog) {
  const version = tag.replace(/^v/, '')
  const section = extractChangelogSection(changelog, version)

  const lines = []
  lines.push(`# ModbusSim ${tag}`)
  lines.push('')
  lines.push(CN_MIRROR_BANNER)
  lines.push('')
  lines.push('## 下载 / Downloads')
  lines.push('')
  lines.push('下方资产里按平台选择 / Pick the asset for your platform below:')
  lines.push('')
  lines.push('macOS 包解压后得到 `.app`,拖入“应用程序”即可;Windows 为单文件 exe(首次运行若缺 WebView2 会引导安装);Linux 为单文件二进制。 / The macOS zip contains the `.app` bundle; the Windows asset is a single exe (it offers to install WebView2 if missing); the Linux asset is a single binary.')
  lines.push('')
  lines.push(`| 平台 / Platform | ${APPS[0].role} | ${APPS[1].role} |`)
  lines.push('|---|---|---|')
  for (const p of PLATFORMS) {
    const cells = APPS.map((a) => `\`${p.file(a.prefix, version)}\``).join(' | ')
    lines.push(`| ${p.label} | ${cells} |`)
  }
  lines.push('')
  if (section) {
    lines.push(section)
    lines.push('')
  } else {
    lines.push(`> ⚠️  CHANGELOG.md 缺少 \`${version}\` 的 section,请补上。`)
    lines.push('')
  }
  lines.push(MACOS_FIRST_LAUNCH_NOTE)
  lines.push('')
  lines.push('---')
  lines.push('')
  lines.push(`完整变更历史 / Full changelog: [CHANGELOG.md](${REPO_URL}/blob/main/CHANGELOG.md)`)
  lines.push('')
  lines.push(`之前版本 / Previous releases: <${REPO_URL}/releases>`)
  return lines.join('\n')
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const tag = process.argv[2]
  if (!tag) { console.error('usage: build-release-notes.mjs <tag>'); process.exit(1) }
  const changelog = readFileSync(resolve(process.cwd(), 'CHANGELOG.md'), 'utf8')
  const body = buildBody(tag, changelog)
  const out = resolve(process.cwd(), 'RELEASE_BODY.md')
  writeFileSync(out, body + '\n')
  console.log(`wrote ${out} (${body.length} bytes)`)
}

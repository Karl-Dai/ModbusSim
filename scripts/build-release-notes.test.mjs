import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { buildBody } from './build-release-notes.mjs'

const changelog = `# Changelog

## [0.17.2] - 2026-08-13

### Fixed 修复

- point workflow

## [0.17.1] - 2026-06-12

- old
`

describe('buildBody', () => {
  it('renders the matching changelog section and both apps', () => {
    const body = buildBody('v0.17.2', changelog)
    assert.match(body, /^# ModbusSim v0\.17\.2\b/)
    assert.ok(body.includes('### Fixed 修复'))
    assert.ok(!body.includes('0.17.1'))
    assert.ok(body.includes('ModbusSlave_0.17.2_macOS-universal.zip'))
    assert.ok(body.includes('ModbusMaster_0.17.2_linux-x64.tar.gz'))
  })

  it('lists the Wails asset names for every platform and app', () => {
    const body = buildBody('v0.17.2', changelog)
    for (const app of ['ModbusSlave', 'ModbusMaster']) {
      assert.ok(body.includes(`${app}_0.17.2_macOS-universal.zip`))
      assert.ok(body.includes(`${app}_0.17.2_windows-x64.exe`))
      assert.ok(body.includes(`${app}_0.17.2_linux-x64.tar.gz`))
    }
  })

  it('keeps mirror and macOS first-launch guidance', () => {
    const body = buildBody('v0.17.2', changelog)
    assert.ok(body.indexOf('ghfast.top/') < body.indexOf('## 下载 / Downloads'))
    assert.ok(body.includes('macOS 首次启动 / First launch on macOS'))
    assert.ok(body.includes('xattr -dr com.apple.quarantine'))
  })

  it('warns when the version section is missing', () => {
    assert.ok(buildBody('v9.9.9', changelog).includes('CHANGELOG.md 缺少 `9.9.9`'))
  })
})

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  buildChangelogSection,
  nextPatchVersion,
  parseCommit,
  prepareRelease,
  prependChangelog,
  updateGoVersionVar,
  updateProductVersion,
} from './prepare-release.mjs'

describe('prepare-release', () => {
  it('bumps a stable tag by one patch version', () => {
    assert.equal(nextPatchVersion('v0.17.1'), '0.17.2')
    assert.throws(() => nextPatchVersion('v0.17.1-rc.1'), /stable SemVer/)
  })

  it('classifies conventional commits and preserves scopes', () => {
    assert.deepEqual(parseCommit('fix(slave): complete point workflows (#6)'), {
      category: 'Fixed',
      summary: 'slave: complete point workflows (#6)',
    })
    assert.deepEqual(parseCommit('feat!: change workspace format'), {
      category: 'Added',
      summary: 'change workspace format (breaking)',
    })
    assert.deepEqual(parseCommit('plain subject'), {
      category: 'Changed',
      summary: 'plain subject',
    })
  })

  it('generates categorized changelog sections', () => {
    const changelog = buildChangelogSection('0.17.2', '2026-08-13', [
      'fix(slave): complete point workflows (#6)',
      'feat(master): add polling preset',
      'docs: explain releases',
    ])
    assert.match(changelog, /^## \[0\.17\.2\] - 2026-08-13/)
    assert.ok(changelog.includes('### Added 新增\n\n- master: add polling preset'))
    assert.ok(changelog.includes('### Fixed 修复\n\n- slave: complete point workflows (#6)'))
  })

  it('updates all supported version file formats', () => {
    assert.ok(updateProductVersion('{\n  "info": {\n    "productVersion": "0.17.1"\n  }\n}\n', '0.17.2')
      .includes('"productVersion": "0.17.2"'))
    assert.ok(updateGoVersionVar('var Version = "0.17.1-go-dev"\n', '0.17.2')
      .includes('var Version = "0.17.2"'))
  })

  it('inserts a new changelog section without deleting history', () => {
    const oldChangelog = '# Changelog\n\nIntro.\n\n## [0.17.1] - 2026-06-12\n\n- old\n'
    const section = buildChangelogSection('0.17.2', '2026-08-13', ['fix: new fix'])
    const changelog = prependChangelog(oldChangelog, section)
    assert.ok(changelog.indexOf('[0.17.2]') < changelog.indexOf('[0.17.1]'))
    assert.ok(changelog.includes('- old'))
  })

  it('prepares and verifies all ModbusSim version files together', () => {
    const root = mkdtempSync(join(tmpdir(), 'modbussim-release-'))
    try {

      for (const dir of ['go/cmd/slavegui', 'go/cmd/mastergui']) {
        mkdirSync(join(root, dir), { recursive: true })
        writeFileSync(join(root, dir, 'wails.json'), '{\n  "info": {\n    "productVersion": "0.17.1"\n  }\n}\n')
        writeFileSync(join(root, dir, 'app.go'), 'package main\n\nvar Version = "0.17.1-go-dev"\n')
      }
      writeFileSync(
        join(root, 'CHANGELOG.md'),
        '# Changelog\n\nIntro.\n\n## [0.17.1] - 2026-06-12\n\n- old\n',
      )

      prepareRelease(root, {
        fromTag: 'v0.17.1',
        version: '0.17.2',
        date: '2026-08-13',
        subjects: ['fix(slave): complete point workflows (#6)'],
      })


      for (const dir of ['go/cmd/slavegui', 'go/cmd/mastergui']) {
        assert.ok(readFileSync(join(root, dir, 'wails.json'), 'utf8').includes('"productVersion": "0.17.2"'))
        assert.ok(readFileSync(join(root, dir, 'app.go'), 'utf8').includes('var Version = "0.17.2"'))
      }
      assert.ok(readFileSync(join(root, 'CHANGELOG.md'), 'utf8').includes('## [0.17.2] - 2026-08-13'))
    } finally {
      rmSync(root, { recursive: true, force: true })
    }
  })
})

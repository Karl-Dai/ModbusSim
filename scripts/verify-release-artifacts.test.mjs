import { it } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, writeFileSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { expectedBundles, verifyBundles } from './verify-release-artifacts.mjs'

it('requires both apps on all four Go/Wails platforms', () => {
  assert.equal(expectedBundles('v0.17.9').length, 8)
  assert.ok(expectedBundles('v0.17.9').includes('ModbusMaster_0.17.9_windows-arm64.exe'))
  assert.throws(() => expectedBundles('next'))
})
it('refuses missing bundles and writes checksums only for a complete release', () => {
  const directory = mkdtempSync(join(tmpdir(), 'modbussim-artifacts-'))
  try {
    assert.throws(() => verifyBundles('v0.17.9', directory))
    for (const name of expectedBundles('v0.17.9')) writeFileSync(join(directory, name), 'test bundle')
    assert.equal(verifyBundles('v0.17.9', directory).length, 8)
    assert.equal(readFileSync(join(directory, 'SHA256SUMS'), 'utf8').trim().split('\n').length, 8)
    const manifest = JSON.parse(readFileSync(join(directory, 'release-manifest.json'), 'utf8'))
    assert.equal(manifest.version, '0.17.9')
    assert.equal(manifest.runtime, 'go-wails')
    writeFileSync(join(directory, expectedBundles('v0.17.9')[0]), '')
    assert.throws(() => verifyBundles('v0.17.9', directory), /empty release bundle/)
  } finally { rmSync(directory, { recursive: true, force: true }) }
})

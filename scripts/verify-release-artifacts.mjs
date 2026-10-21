#!/usr/bin/env node
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync, statSync } from 'node:fs'
import { resolve } from 'node:path'

export function expectedBundles(tag) {
  if (!/^v\d+\.\d+\.\d+$/.test(tag)) throw new Error('invalid stable release tag')
  return ['ModbusMaster', 'ModbusSlave'].flatMap(product => [
    `${product}_${tag.slice(1)}_macOS-universal.zip`,
    `${product}_${tag.slice(1)}_linux-x64.tar.gz`,
    `${product}_${tag.slice(1)}_windows-x64.exe`,
    `${product}_${tag.slice(1)}_windows-arm64.exe`,
  ]).sort()
}

export function verifyBundles(tag, directory) {
  const assets = expectedBundles(tag).map(name => {
    const path = resolve(directory, name)
    if (statSync(path).size === 0) throw new Error(`empty release bundle: ${name}`)
    return { name, sha256: createHash('sha256').update(readFileSync(path)).digest('hex'),
      url: `https://github.com/Karl-Dai/ModbusSim/releases/download/${tag}/${name}` }
  })
  writeFileSync(resolve(directory, 'SHA256SUMS'), assets.map(asset => `${asset.sha256}  ${asset.name}\n`).join(''))
  writeFileSync(resolve(directory, 'release-manifest.json'), JSON.stringify({ version: tag.slice(1), runtime: 'go-wails', installation: 'manual', assets }, null, 2) + '\n')
  return assets
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const assets = verifyBundles(process.argv[2], process.argv[3])
  console.log(`verified ${assets.length} Go/Wails bundles and generated SHA256SUMS`)
}

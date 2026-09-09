import { expect, it } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { useI18n } from '../src/i18n'
function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap(entry => entry.isDirectory() ? files(join(dir, entry.name)) : /\.(vue|ts)$/.test(entry.name) ? [join(dir, entry.name)] : [])
}
it('has Chinese and English messages for every literal slave UI translation key', () => {
  const { t, setLocale } = useI18n()
  const keys = new Set(files(resolve('../frontend/src')).flatMap(path => [...readFileSync(path, 'utf8').matchAll(/\bt\(\s*['"]([^'"]+)['"]/g)].map(match => match[1])))
  for (const locale of ['zh-CN', 'en-US'] as const) {
    setLocale(locale)
    for (const key of keys) expect(t(key), `${locale}: ${key}`).not.toBe(key)
  }
  setLocale('zh-CN')
})

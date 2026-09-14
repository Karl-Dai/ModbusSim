import { beforeEach, describe, expect, it, vi } from 'vitest'

// Mock localStorage since jsdom's implementation is incomplete
const mockStorage: Record<string, string> = {}

const localStorageMock = {
  getItem: (key: string) => mockStorage[key] ?? null,
  setItem: (key: string, value: string) => {
    mockStorage[key] = value
  },
  removeItem: (key: string) => {
    delete mockStorage[key]
  },
  clear: () => {
    Object.keys(mockStorage).forEach(key => delete mockStorage[key])
  },
  key: (index: number) => Object.keys(mockStorage)[index] ?? null,
  length: Object.keys(mockStorage).length,
}

type ChangeListener = (event: { matches: boolean }) => void

function installMatchMedia(initial: boolean) {
  let matches = initial
  const listeners = new Set<ChangeListener>()
  const mql = {
    get matches() {
      return matches
    },
    media: '(prefers-color-scheme: dark)',
    onchange: null,
    addEventListener: (_type: string, cb: ChangeListener) => {
      listeners.add(cb)
    },
    removeEventListener: (_type: string, cb: ChangeListener) => {
      listeners.delete(cb)
    },
    addListener: (cb: ChangeListener) => {
      listeners.add(cb)
    },
    removeListener: (cb: ChangeListener) => {
      listeners.delete(cb)
    },
    dispatchEvent: () => true,
    set(next: boolean) {
      matches = next
      listeners.forEach(cb => cb({ matches: next }))
    },
  }
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn(() => mql),
  })
  return mql
}

async function freshTheme() {
  vi.resetModules()
  const theme = await import('../src/composables/useTheme')
  const { nextTick } = await import('vue')
  return { ...theme, nextTick }
}

beforeEach(() => {
  localStorageMock.clear()
  global.localStorage = localStorageMock as any
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.style.colorScheme = ''
})

describe('useTheme', () => {
  it('persists the chosen mode and restores it on the next load', async () => {
    installMatchMedia(true)
    let theme = await freshTheme()
    theme.initTheme()
    theme.useTheme().setMode('light')
    expect(mockStorage['modbussim.theme']).toBe('light')
    await theme.nextTick()
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')

    theme = await freshTheme()
    theme.initTheme()
    expect(theme.useTheme().mode.value).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('follows the system color scheme in auto mode', async () => {
    const mql = installMatchMedia(true)
    const theme = await freshTheme()
    theme.initTheme()
    expect(theme.useTheme().resolvedTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')

    mql.set(false)
    await theme.nextTick()
    expect(theme.useTheme().resolvedTheme.value).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')
  })

  it('ignores system changes in manual mode', async () => {
    const mql = installMatchMedia(true)
    const theme = await freshTheme()
    theme.initTheme()
    theme.useTheme().setMode('dark')
    mql.set(false)
    await theme.nextTick()
    expect(theme.useTheme().resolvedTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('falls back to dark when matchMedia is unavailable', async () => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: undefined,
    })
    const theme = await freshTheme()
    theme.initTheme()
    expect(theme.useTheme().resolvedTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
  })
})

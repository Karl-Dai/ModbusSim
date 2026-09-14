import { computed, ref, watch } from 'vue'

export type ThemeMode = 'auto' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

export const THEME_STORAGE_KEY = 'modbussim.theme'

function isThemeMode(raw: string | null): raw is ThemeMode {
  return raw === 'auto' || raw === 'light' || raw === 'dark'
}

export function loadStoredTheme(): ThemeMode {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY)
    return isThemeMode(raw) ? raw : 'auto'
  } catch {
    return 'auto'
  }
}

export function storeTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, mode)
  } catch {
    // localStorage 被禁用时静默忽略
  }
}

function systemPrefersDark(): boolean {
  try {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true
    return window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    return true
  }
}

const mode = ref<ThemeMode>(loadStoredTheme())
const systemDark = ref(systemPrefersDark())
const resolvedTheme = computed<ResolvedTheme>(() =>
  mode.value === 'auto' ? (systemDark.value ? 'dark' : 'light') : mode.value,
)

function applyTheme(): void {
  const root = document.documentElement
  root.dataset.theme = resolvedTheme.value
  root.style.colorScheme = resolvedTheme.value
}

let initialized = false

export function initTheme(): void {
  if (initialized) return
  initialized = true
  try {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      window
        .matchMedia('(prefers-color-scheme: dark)')
        .addEventListener('change', event => {
          systemDark.value = event.matches
        })
    }
  } catch {
    // matchMedia 不可用时保持静态深色
  }
  watch(resolvedTheme, applyTheme, { immediate: true })
}

export function useTheme() {
  function setMode(next: ThemeMode): void {
    mode.value = next
    storeTheme(next)
  }
  return { mode, resolvedTheme, setMode }
}

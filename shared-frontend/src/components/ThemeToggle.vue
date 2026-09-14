<script setup lang="ts">
import { useI18n } from '../i18n'
import { useTheme } from '../composables/useTheme'

const { t } = useI18n()
const { resolvedTheme, setMode } = useTheme()

const paths = {
  sun: 'M8 5a3 3 0 1 0 0 6 3 3 0 0 0 0-6M8 2v1.5M8 12.5V14M2 8h1.5M12.5 8H14M4.82 4.82 3.76 3.76M11.18 4.82l1.06-1.06M4.82 11.18l-1.06 1.06M11.18 11.18l1.06 1.06',
  moon: 'M14 8.53A6 6 0 1 1 7.47 2 4.67 4.67 0 0 0 14 8.53z',
}

function toggle() {
  setMode(resolvedTheme.value === 'dark' ? 'light' : 'dark')
}
</script>

<template>
  <button type="button" class="theme-toggle" :title="t('parity.themeToggle')"
    :aria-label="t('parity.themeToggle')" @click="toggle">
    <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor"
      stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path :d="resolvedTheme === 'dark' ? paths.sun : paths.moon" />
    </svg>
  </button>
</template>

<style scoped>
.theme-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 24px;
  margin-right: 8px;
  border-radius: 4px;
  background: var(--c-surface0);
  border: 1px solid var(--c-surface1);
  color: var(--c-subtext0);
  cursor: pointer;
  font-family: inherit;
}

.theme-toggle:hover {
  background: var(--c-surface1);
  color: var(--c-text);
}
</style>

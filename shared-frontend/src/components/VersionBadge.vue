<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getVersion } from '@tauri-apps/api/app'
import { invoke } from '@tauri-apps/api/core'
import { RELEASE_HIGHLIGHTS } from '../releaseNotes'
import { useI18n } from 'shared-frontend'

const { t, locale } = useI18n()

const REPO_URL = 'https://github.com/Karl-Dai/ModbusSim'
const version = ref('')
const showPanel = ref(false)
const analyticsEnabled = ref(true)

onMounted(async () => {
  try { version.value = await getVersion() } catch { version.value = '' }
  try { analyticsEnabled.value = await invoke<boolean>('get_analytics_enabled') } catch { /* not in Tauri */ }
})

// In a Tauri webview window.open can't reach the OS browser, so go through the
// opener plugin via raw invoke (shared-frontend only depends on @tauri-apps/api).
// Outside Tauri (plain vite dev in browser) invoke throws — fall back to window.open.
async function openRepo() {
  try {
    await invoke('plugin:opener|open_url', { url: REPO_URL })
  } catch {
    window.open(REPO_URL, '_blank')
  }
}

async function toggleAnalytics() {
  analyticsEnabled.value = !analyticsEnabled.value
  try {
    await invoke('set_analytics_enabled', { enabled: analyticsEnabled.value })
  } catch { /* not in Tauri */ }
}
</script>

<template>
  <div class="version-badge">
    <span v-if="version" class="version-text" :title="`v${version}`">v{{ version }}</span>
    <button
      type="button"
      class="github-link"
      :title="REPO_URL"
      :aria-label="REPO_URL"
      @click="openRepo"
    >
      <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">
        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38
                 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13
                 -.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66
                 .07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15
                 -.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0
                 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82
                 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01
                 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z"/>
      </svg>
    </button>
    <button
      type="button"
      class="about-toggle"
      :title="t('about.title')"
      :aria-label="t('about.title')"
      @click="showPanel = !showPanel"
    >
      <svg viewBox="0 0 16 16" width="13" height="13" fill="currentColor" aria-hidden="true">
        <path d="M8 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM8 4a1 1 0 1 1 0 2 1 1 0 0 1 0-2zM7 7h2v5H7z"/>
      </svg>
    </button>

    <template v-if="showPanel">
      <div class="about-backdrop" @click="showPanel = false"></div>
      <div class="about-panel" role="dialog" :aria-label="t('about.title')">
        <div class="about-title">ModbusSim<span v-if="version"> v{{ version }}</span></div>
        <ul class="release-notes"><li v-for="note in RELEASE_HIGHLIGHTS[locale]" :key="note">{{ note }}</li></ul>
        <label class="about-row">
          <input type="checkbox" :checked="analyticsEnabled" @change="toggleAnalytics" />
          <span>{{ t('about.analytics') }}</span>
        </label>
        <div class="about-note">{{ t('about.analyticsNote') }}</div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.release-notes { padding-left: 16px; line-height: 1.5; max-height: 40vh; overflow-y: auto; margin: 8px 0; }
.release-notes li { margin-bottom: 6px; }
.version-badge {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 0 4px;
  font-size: 11px;
  color: var(--c-overlay0);
  font-variant-numeric: tabular-nums;
}
.version-text {
  padding: 2px 4px;
  line-height: 1;
  white-space: nowrap;
}
.github-link,
.about-toggle {
  display: inline-flex;
  align-items: center;
  padding: 3px 4px;
  margin: 0;
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
  border-radius: 4px;
  line-height: 1;
}
.github-link:hover,
.about-toggle:hover { color: var(--c-text); background: var(--c-surface0); }
.github-link svg,
.about-toggle svg { display: block; }

.about-backdrop {
  position: fixed;
  inset: 0;
  z-index: 40;
}
.about-panel {
  position: absolute;
  top: 100%;
  right: 0;
  margin-top: 4px;
  z-index: 41;
  width: 240px;
  padding: 10px 12px;
  background: var(--c-base);
  border: 1px solid var(--c-surface0);
  border-radius: 8px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.35);
  color: var(--c-text);
  font-size: 12px;
  text-align: left;
}
.about-title {
  font-weight: 600;
  margin-bottom: 8px;
}
.about-row {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.about-row input { cursor: pointer; }
.about-note {
  margin-top: 6px;
  color: var(--c-overlay0);
  line-height: 1.4;
}
</style>

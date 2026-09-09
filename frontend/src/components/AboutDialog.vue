<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getVersion } from '@tauri-apps/api/app'
import { invoke } from '@tauri-apps/api/core'
import { ModalShell, useI18n, RELEASE_HIGHLIGHTS } from 'shared-frontend'
const emit = defineEmits<{ close: [] }>()
const { t, locale } = useI18n()
const version = ref('')
const analytics = ref<boolean | null>(null)
const error = ref('')
const busy = ref(false)
onMounted(async () => {
  try { version.value = await getVersion(); analytics.value = await invoke<boolean>('get_analytics_enabled') }
  catch (e) { error.value = String(e) }
})
async function openLink(path: string) {
  try { await invoke('plugin:opener|open_url', { url: `https://github.com/Karl-Dai/ModbusSim${path}` }) }
  catch (e) { error.value = String(e) }
}
async function toggleAnalytics() {
  busy.value = true
  try { await invoke('set_analytics_enabled', { enabled: !analytics.value }); analytics.value = !analytics.value }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>
<template>
  <ModalShell :title="t('toolbar.appTitleSlave')" :subtitle="version ? `v${version}` : '—'" :busy="busy" @close="emit('close')">
    <p class="hint">{{ t('parity.aboutDescription') }}</p>
    <ul class="release-notes"><li v-for="note in RELEASE_HIGHLIGHTS[locale]" :key="note">{{ note }}</li></ul>
    <div class="links"><button @click="openLink(locale === 'zh-CN' ? '/blob/main/README_CN.md' : '/blob/main/README.md')">{{ t('parity.documentation') }}</button><button @click="openLink('/releases')">{{ t('parity.releases') }}</button><button @click="openLink('')">GitHub</button></div>
    <label class="analytics"><input type="checkbox" :disabled="busy || analytics === null" :checked="analytics === true" @change="toggleAnalytics" /> {{ t('about.analytics') }}</label>
    <p class="hint">{{ t('about.analyticsNote') }}</p><p v-if="error" class="error" role="alert">{{ error }}</p>
  </ModalShell>
</template>
<style scoped>
.release-notes { padding-left: 18px; line-height: 1.6; }
.release-notes li { margin: 8px 0; }
.links { display: flex; gap: 12px; margin: 18px 0; }
.links button { border: 0; background: transparent; color: var(--c-blue); cursor: pointer; }
.analytics { margin-top: 20px; }
</style>

<script setup lang="ts">
import { ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { ModalShell, useI18n, showConfirm } from 'shared-frontend'
import { decodeRegisterCsv } from '../utils/registerCsv'
import type { RegisterDef } from '../composables/useRegisterFormat'
const props = defineProps<{ connectionId: string; slaveId: number }>()
const emit = defineEmits<{ close: []; imported: [] }>()
const { t } = useI18n()
const rows = ref<RegisterDef[]>([]), mode = ref('append'), error = ref(''), busy = ref(false)
async function choose(event: Event) {
  rows.value = []; error.value = ''
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  busy.value = true
  try { rows.value = decodeRegisterCsv(await file.text()) }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
async function apply() {
  if (!rows.value.length) return
  busy.value = true
  try {
    if (mode.value === 'replace' && !await showConfirm(t('parity.confirmReplace'))) return
    await invoke('import_registers', { request: { connection_id: props.connectionId, slave_id: props.slaveId, registers: rows.value, mode: mode.value } })
    emit('imported'); emit('close')
  } catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>
<template>
  <ModalShell :title="t('parity.importCsv')" :busy="busy" @close="emit('close')">
    <label>{{ t('parity.chooseCsv') }}<input type="file" accept=".csv,text/csv" :disabled="busy" @change="choose" /></label>
    <label>{{ t('parity.importMode') }}<select v-model="mode" :disabled="busy"><option value="append">{{ t('parity.append') }}</option><option value="replace">{{ t('parity.replace') }}</option></select></label>
    <p class="hint">{{ t('parity.csvHint') }}</p><p role="status">{{ t('parity.readyRows', { n: rows.length }) }}</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template #actions><button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button><button class="primary" :disabled="busy || !rows.length" @click="apply">{{ t('common.confirm') }}</button></template>
  </ModalShell>
</template>

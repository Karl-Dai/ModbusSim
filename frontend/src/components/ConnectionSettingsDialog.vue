<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { ModalShell, useI18n } from 'shared-frontend'
const props = defineProps<{ connectionId: string }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
type Transport = { type: string; host?: string; port: string | number; baud_rate?: number; data_bits?: number; stop_bits?: number; parity?: string }
const transport = ref<Transport | null>(null), busy = ref(true), error = ref('')
onMounted(async () => {
  try { transport.value = await invoke<Transport>('get_slave_transport', { id: props.connectionId }) }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
})
async function saveSettings() {
  if (!transport.value) return
  busy.value = true; error.value = ''
  try { await invoke('update_slave_transport', { id: props.connectionId, transport: transport.value }); emit('saved'); emit('close') }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>
<template>
  <ModalShell :title="t('parity.connectionSettings')" :busy="busy" @close="emit('close')">
    <p class="hint">{{ t('parity.stoppedSettings') }}</p>
    <template v-if="transport">
      <p class="hint">{{ transport.type.toUpperCase() }}</p>
      <template v-if="transport.host !== undefined">
        <label>{{ t('parity.bindAddress') }}<input v-model="transport.host" /></label>
        <label>{{ t('dialog.portNumber') }}<input type="number" min="1" max="65535" v-model.number="transport.port" /></label>
      </template>
      <template v-else>
        <label>{{ t('dialog.serialPort') }}<input v-model="transport.port" /></label>
        <label>{{ t('dialog.baudRate') }}<input type="number" min="1" v-model.number="transport.baud_rate" /></label>
        <label>{{ t('dialog.dataBits') }}<input type="number" min="5" max="8" v-model.number="transport.data_bits" /></label>
        <label>{{ t('dialog.stopBits') }}<input type="number" min="1" max="2" v-model.number="transport.stop_bits" /></label>
        <label>{{ t('dialog.parity') }}<select v-model="transport.parity"><option value="none">None</option><option value="odd">Odd</option><option value="even">Even</option></select></label>
      </template>
    </template>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template #actions><button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button><button class="primary" :disabled="busy || !transport" @click="saveSettings">{{ t('common.save') }}</button></template>
  </ModalShell>
</template>

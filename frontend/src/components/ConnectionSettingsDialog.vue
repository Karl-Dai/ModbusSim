<script setup lang="ts">
import type { project } from "../../wailsjs/go/models"
import { ref, onMounted } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { ModalShell, useI18n } from 'shared-frontend'
const props = defineProps<{ connectionId: string }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const transport = ref<project.Transport | null>(null), busy = ref(true), error = ref('')
onMounted(async () => {
  try { transport.value = await App.GetTransport(props.connectionId) }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
})
async function saveSettings() {
  if (!transport.value) return
  busy.value = true; error.value = ''
  try { await App.UpdateTransport(props.connectionId, transport.value); emit('saved'); emit('close') }
  catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>
<template>
  <ModalShell :title="t('parity.connectionSettings')" :busy="busy" @close="emit('close')">
    <p class="hint">{{ t('parity.stoppedSettings') }}</p>
    <template v-if="transport">
      <p class="hint">{{ transport.Type.toUpperCase() }}</p>
      <template v-if="transport.Host !== undefined">
        <label>{{ t('parity.bindAddress') }}<input v-model="transport.Host" /></label>
        <label>{{ t('dialog.portNumber') }}<input type="number" min="1" max="65535" v-model.number="transport.Port" /></label>
      </template>
      <template v-else-if="transport.Serial">
        <label>{{ t('dialog.serialPort') }}<input v-model="transport.Serial.Port" /></label>
        <label>{{ t('dialog.baudRate') }}<input type="number" min="1" v-model.number="transport.Serial.BaudRate" /></label>
        <label>{{ t('dialog.dataBits') }}<input type="number" min="5" max="8" v-model.number="transport.Serial.DataBits" /></label>
        <label>{{ t('dialog.stopBits') }}<input type="number" min="1" max="2" v-model.number="transport.Serial.StopBits" /></label>
        <label>{{ t('dialog.parity') }}<select v-model="transport.Serial.Parity"><option value="none">None</option><option value="odd">Odd</option><option value="even">Even</option></select></label>
      </template>
    </template>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template #actions><button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button><button class="primary" :disabled="busy || !transport" @click="saveSettings">{{ t('common.save') }}</button></template>
  </ModalShell>
</template>

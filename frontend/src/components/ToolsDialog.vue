<script setup lang="ts">
import { ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { ModalShell, useI18n } from 'shared-frontend'
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const tool = ref('frame'), data = ref('0001 0000 0006 01 03 0000 0001')
const transport = ref('tcp'), direction = ref('request'), plcAddress = ref(40001)
const result = ref(''), error = ref(''), busy = ref(false)
watch([tool, data, transport, direction, plcAddress], () => { result.value = ''; error.value = '' })
async function calculate() {
  busy.value = true; result.value = ''; error.value = ''
  try {
    if (tool.value === 'address') {
      const row = await invoke<{ protocol_address: number; register_type: string }>('convert_plc_to_modbus', { request: { address: plcAddress.value } })
      result.value = `${t('parity.protocolAddress')}: ${row.protocol_address}\n${t('parity.registerType')}: ${row.register_type}`
    } else if (tool.value === 'checksum') {
      const [crc, lrc] = await Promise.all([invoke<string>('calculate_crc16', { data: data.value }), invoke<string>('calculate_lrc', { data: data.value })])
      result.value = `CRC16: ${crc}\n${t('parity.wireOrder')}: ${crc.slice(2)} ${crc.slice(0, 2)}\nLRC: ${lrc}`
    } else {
      result.value = await invoke<string>('inspect_modbus_frame', { data: data.value, transport: transport.value, direction: direction.value })
    }
  } catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>
<template>
  <ModalShell :title="t('parity.toolsTitle')" :busy="busy" wide @close="emit('close')">
    <label for="tool-kind">{{ t('common.tools') }}</label>
    <select id="tool-kind" v-model="tool"><option value="frame">{{ t('parity.frameParser') }}</option><option value="checksum">CRC16 / LRC</option><option value="address">{{ t('parity.addressConverter') }}</option></select>
    <template v-if="tool === 'address'"><label for="plc-address">{{ t('parity.plcAddress') }}</label><input id="plc-address" type="number" v-model.number="plcAddress" min="1" /></template>
    <template v-else>
      <div v-if="tool === 'frame'" class="options"><label>{{ t('dialog.transport') }}<select v-model="transport"><option value="tcp">TCP / TLS</option><option value="rtu">RTU / RTU-over-TCP</option><option value="ascii">ASCII</option></select></label><label>{{ t('parity.frameDirection') }}<select v-model="direction"><option value="request">{{ t('parity.request') }}</option><option value="response">{{ t('parity.response') }}</option></select></label></div>
      <label for="frame-input">{{ t('parity.frameInput') }}</label><textarea id="frame-input" v-model="data" rows="5" spellcheck="false" />
    </template>
    <p v-if="error" role="alert" class="error">{{ error }}</p><pre v-if="result" class="result" tabindex="0">{{ result }}</pre>
    <template #actions><button :disabled="busy" @click="emit('close')">{{ t('common.close') }}</button><button class="primary" :disabled="busy" @click="calculate">{{ busy ? t('common.loading') : t('parity.calculate') }}</button></template>
  </ModalShell>
</template>
<style scoped>
.options { display: flex; gap: 12px; }.options label { flex: 1; }
.result { white-space: pre-wrap; overflow-wrap: anywhere; background: var(--c-mantle); padding: 14px; line-height: 1.7; user-select: text; }
textarea, .result { font-family: var(--font-mono); }
</style>

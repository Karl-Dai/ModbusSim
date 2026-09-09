<script setup lang="ts">
import { ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert } from 'shared-frontend'

const { t } = useI18n()

interface Props { show: boolean; connectionId: string | null }
const props = defineProps<Props>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created'): void }>()

const form = ref({
  name: '',
  function: 'read_holding_registers',
  start_address: 0,
  quantity: 10,
  interval_ms: 1000,
})

watch(() => props.show, (visible) => {
  if (!visible) return
  form.value = { name: '', function: 'read_holding_registers', start_address: 0, quantity: 10, interval_ms: 1000 }
})

async function submit() {
  if (!props.connectionId) return
  if (!Number.isInteger(form.value.start_address) || form.value.start_address < 0
    || !Number.isInteger(form.value.quantity) || form.value.quantity < 1 || form.value.quantity > 65535
    || form.value.start_address + form.value.quantity > 65536
    || !Number.isInteger(form.value.interval_ms) || form.value.interval_ms < 100 || form.value.interval_ms > 60000) {
    await showAlert(t('errors.invalidScanRange'))
    return
  }
  try {
    await invoke('add_scan_group', {
      connectionId: props.connectionId,
      request: {
        name: form.value.name || `SG-${Date.now() % 10000}`,
        function: form.value.function,
        start_address: form.value.start_address,
        quantity: form.value.quantity,
        interval_ms: form.value.interval_ms,
      }
    })
    emit('close')
    emit('created')
  } catch (e) { await showAlert(String(e)) }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-box">
        <div class="modal-title">{{ t('dialog.newScanGroup') }}</div>
        <div class="modal-body">
          <label class="form-label">
            {{ t('dialog.simpleName') }}
            <input v-model="form.name" class="form-input" type="text" :placeholder="t('dialog.scanGroupName')" />
          </label>
          <label class="form-label">
            {{ t('table.function') }}
            <select v-model="form.function" class="form-input">
              <option value="read_coils">FC01 - Read Coils</option>
              <option value="read_discrete_inputs">FC02 - Read Discrete Inputs</option>
              <option value="read_holding_registers">FC03 - Read Holding Registers</option>
              <option value="read_input_registers">FC04 - Read Input Registers</option>
            </select>
          </label>
          <label class="form-label">
            {{ t('table.startAddress') }}
            <input v-model.number="form.start_address" class="form-input" type="number" min="0" max="65535" />
          </label>
          <label class="form-label">
            {{ t('table.quantity') }}
            <input v-model.number="form.quantity" class="form-input" type="number" min="1" :max="Math.min(65535, 65536 - form.start_address)" aria-describedby="scan-quantity-hint" />
          </label>
          <p id="scan-quantity-hint" class="form-hint">{{ t('dialog.scanQuantityHint') }}</p>
          <label class="form-label">
            {{ t('dialog.scanInterval') }}
            <input v-model.number="form.interval_ms" class="form-input" type="number" min="100" max="60000" aria-describedby="scan-interval-hint" />
          </label>
          <p id="scan-interval-hint" class="form-hint">{{ t('dialog.scanIntervalHint') }}</p>
        </div>
        <div class="modal-footer">
          <button class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="submit">{{ t('common.create') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { box-sizing: border-box; background: #1e1e2e; border: 1px solid #45475a; border-radius: 8px; padding: 20px; width: 400px; max-width: calc(100vw - 32px); max-height: calc(100vh - 32px); overflow-y: auto; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 15px; font-weight: 600; color: #cdd6f4; margin-bottom: 16px; }
.modal-body { display: flex; flex-direction: column; gap: 12px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.form-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: #6c7086; }
.form-hint { margin: -6px 0 0; color: #a6adc8; font-size: 12px; line-height: 1.5; }
.form-input { padding: 6px 10px; background: #313244; border: 1px solid #45475a; border-radius: 4px; color: #cdd6f4; font-size: 13px; }
.form-input:focus { outline: none; border-color: #89b4fa; }
.btn { padding: 7px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-primary { background: #89b4fa; color: #1e1e2e; }
.btn-primary:hover { background: #74c7ec; }
.btn-secondary { background: #45475a; color: #cdd6f4; }
.btn-secondary:hover { background: #585b70; }
</style>

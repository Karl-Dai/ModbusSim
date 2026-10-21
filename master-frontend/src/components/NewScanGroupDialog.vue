<script setup lang="ts">
import { ref, watch } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { useI18n, showAlert } from 'shared-frontend'

const { t } = useI18n()

interface Props { show: boolean; connectionId: string | null }
const props = defineProps<Props>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created'): void }>()

const form = ref({
  Name: '',
  Function: 'read_holding_registers',
  StartAddress: 0,
  Quantity: 10,
  IntervalMS: 1000,
})

watch(() => props.show, (visible) => {
  if (!visible) return
  form.value = { Name: '', Function: 'read_holding_registers', StartAddress: 0, Quantity: 10, IntervalMS: 1000 }
})

async function submit() {
  if (!props.connectionId) return
  if (!Number.isInteger(form.value.StartAddress) || form.value.StartAddress < 0
    || !Number.isInteger(form.value.Quantity) || form.value.Quantity < 1 || form.value.Quantity > 65535
    || form.value.StartAddress + form.value.Quantity > 65536
    || !Number.isInteger(form.value.IntervalMS) || form.value.IntervalMS < 100 || form.value.IntervalMS > 60000) {
    await showAlert(t('errors.invalidScanRange'))
    return
  }
  try {
    await App.AddScanGroup(props.connectionId, { ID: '', Enabled: true, IsPolling: false, ...{
        Name: form.value.Name || `SG-${Date.now() % 10000}`,
        Function: form.value.Function,
        StartAddress: form.value.StartAddress,
        Quantity: form.value.Quantity,
        IntervalMS: form.value.IntervalMS,
      } })
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
            <input v-model="form.Name" class="form-input" type="text" :placeholder="t('dialog.scanGroupName')" />
          </label>
          <label class="form-label">
            {{ t('table.function') }}
            <select v-model="form.Function" class="form-input">
              <option value="read_coils">FC01 - Read Coils</option>
              <option value="read_discrete_inputs">FC02 - Read Discrete Inputs</option>
              <option value="read_holding_registers">FC03 - Read Holding Registers</option>
              <option value="read_input_registers">FC04 - Read Input Registers</option>
            </select>
          </label>
          <label class="form-label">
            {{ t('table.startAddress') }}
            <input v-model.number="form.StartAddress" class="form-input" type="number" min="0" max="65535" />
          </label>
          <label class="form-label">
            {{ t('table.Quantity') }}
            <input v-model.number="form.Quantity" class="form-input" type="number" min="1" :max="Math.min(65535, 65536 - form.StartAddress)" aria-describedby="scan-Quantity-hint" />
          </label>
          <p id="scan-Quantity-hint" class="form-hint">{{ t('dialog.scanQuantityHint') }}</p>
          <label class="form-label">
            {{ t('dialog.scanInterval') }}
            <input v-model.number="form.IntervalMS" class="form-input" type="number" min="100" max="60000" aria-describedby="scan-interval-hint" />
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
.modal-box { box-sizing: border-box; background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; padding: 20px; width: 400px; max-width: calc(100vw - 32px); max-height: calc(100vh - 32px); overflow-y: auto; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 15px; font-weight: 600; color: var(--c-text); margin-bottom: 16px; }
.modal-body { display: flex; flex-direction: column; gap: 12px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.form-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--c-subtext0); }
.form-hint { margin: -6px 0 0; color: var(--c-subtext0); font-size: 12px; line-height: 1.5; }
.form-input { padding: 6px 10px; background: var(--c-control-bg); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 13px; }
.form-input:focus { outline: none; border-color: var(--c-blue); }
.btn { padding: 7px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-primary { background: var(--c-blue); color: var(--c-on-accent); }
.btn-primary:hover { background: var(--c-accent-hover); }
.btn-secondary { background: var(--c-neutral-bg); color: var(--c-text); }
.btn-secondary:hover { background: var(--c-hover-bg); }
</style>

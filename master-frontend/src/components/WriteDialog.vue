<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert, float32ToU16Pair, type ByteOrder } from 'shared-frontend'

const { t } = useI18n()

interface Props { show: boolean; connectionId: string | null; slaveId?: number | null }
const props = defineProps<Props>()
const writeTarget = computed(() => props.slaveId == null
  ? t('valuePanel.writeDefaultTarget')
  : t('valuePanel.writeTarget', { id: props.slaveId }))
const emit = defineEmits<{ (e: 'close'): void }>()

const form = ref({
  function: 'write_single_register',
  address: 0,
  value: '',
  dataType: 'raw' as 'raw' | 'float32',
  byteOrder: 'ABCD' as ByteOrder,
})

watch(() => props.show, (visible) => {
  if (!visible) return
  form.value = { function: 'write_single_register', address: 0, value: '', dataType: 'raw', byteOrder: 'ABCD' }
})

watch(() => form.value.function, () => { form.value.dataType = 'raw' })

const isMultiRegFC = computed(() => form.value.function === 'write_multiple_registers')
const isFloat32Mode = computed(() => isMultiRegFC.value && form.value.dataType === 'float32')

const float32Preview = computed(() => {
  if (!isFloat32Mode.value) return null
  const parts = form.value.value.split(',').map(s => s.trim()).filter(s => s !== '')
  if (parts.length === 0) return null
  return parts.map((input, index) => {
    const n = parseFloat(input)
    if (isNaN(n)) return { index, input, float: null, regs: null, error: t('errors.invalidNumber') }
    const pair = float32ToU16Pair(n, form.value.byteOrder)
    return { index, input, float: n, regs: pair, error: '' }
  })
})

const float32Valid = computed(() => {
  if (!float32Preview.value) return false
  return float32Preview.value.length > 0 && float32Preview.value.every(r => !r.error)
})

const float32RegCount = computed(() => {
  if (!float32Preview.value) return 0
  return float32Preview.value.filter(r => !r.error).length * 2
})

const float32Warning = computed(() => {
  if (!float32Preview.value || !float32Valid.value) return ''
  const count = float32Preview.value.length
  if (count * 2 > 123) return t('errors.overflowFC16', { count: count * 2 })
  if (form.value.address + count * 2 - 1 > 65535) return t('errors.addressOverflow')
  return ''
})

async function submit() {
  if (!props.connectionId) return
  try {
    const fc = form.value.function
    if (fc === 'write_single_register') {
      await invoke('write_single_register', {
        connectionId: props.connectionId,
        request: { slave_id: props.slaveId ?? null, address: form.value.address, value: parseInt(form.value.value) }
      })
    } else if (fc === 'write_single_coil') {
      await invoke('write_single_coil', {
        connectionId: props.connectionId,
        request: {
          slave_id: props.slaveId ?? null,
          address: form.value.address,
          value: form.value.value === '1' || form.value.value.toLowerCase() === 'true',
        }
      })
    } else if (fc === 'write_multiple_registers') {
      let values: number[]
      if (form.value.dataType === 'float32') {
        const floats = form.value.value.split(',').map(v => parseFloat(v.trim()))
        if (floats.some(isNaN)) { await showAlert(t('errors.invalidFloat')); return }
        values = []
        for (const f of floats) {
          const [r0, r1] = float32ToU16Pair(f, form.value.byteOrder)
          values.push(r0, r1)
        }
      } else {
        values = form.value.value.split(',').map(v => parseInt(v.trim()))
      }
      await invoke('write_multiple_registers', {
        connectionId: props.connectionId,
        request: { slave_id: props.slaveId ?? null, address: form.value.address, values }
      })
    } else if (fc === 'write_multiple_coils') {
      const values = form.value.value.split(',').map(v => v.trim() === '1' || v.trim().toLowerCase() === 'true')
      await invoke('write_multiple_coils', {
        connectionId: props.connectionId,
        request: { slave_id: props.slaveId ?? null, address: form.value.address, values }
      })
    }
    emit('close')
  } catch (e) { await showAlert(String(e)) }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-box">
        <div class="modal-title">{{ t('dialog.writeRegisters') }}</div>
        <div class="modal-body">
          <div class="form-hint write-target">{{ writeTarget }}</div>
          <label class="form-label">
            {{ t('table.function') }}
            <select v-model="form.function" class="form-input">
              <option value="write_single_coil">FC05 - Write Single Coil</option>
              <option value="write_single_register">FC06 - Write Single Register</option>
              <option value="write_multiple_coils">FC15 - Write Multiple Coils</option>
              <option value="write_multiple_registers">FC16 - Write Multiple Registers</option>
            </select>
          </label>
          <label v-if="isMultiRegFC" class="form-label">
            {{ t('dialog.dataType') }}
            <select v-model="form.dataType" class="form-input">
              <option value="raw">Raw u16</option>
              <option value="float32">Float32 (REAL)</option>
            </select>
          </label>
          <label v-if="isFloat32Mode" class="form-label">
            {{ t('dialog.byteOrder') }}
            <select v-model="form.byteOrder" class="form-input">
              <option value="ABCD">AB CD (Big Endian)</option>
              <option value="CDAB">CD AB (Little Endian Word Swap)</option>
              <option value="BADC">BA DC (Byte Swap)</option>
              <option value="DCBA">DC BA (Little Endian)</option>
            </select>
          </label>
          <label class="form-label">
            {{ t('table.address') }}
            <input v-model.number="form.address" class="form-input" type="number" min="0" max="65535" />
          </label>
          <label class="form-label">
            {{ t('dialog.simpleValue') }}
            <span class="form-hint" v-if="isFloat32Mode">{{ t('dialog.valueHintFloat32') }}</span>
            <span class="form-hint" v-else-if="form.function.includes('multiple')">{{ t('dialog.valueHintMultiple') }}</span>
            <textarea v-if="isFloat32Mode" v-model="form.value" class="form-input form-textarea" placeholder="3.14, 2.71, 1.41" rows="3" />
            <input v-else v-model="form.value" class="form-input" type="text" placeholder="0" />
          </label>
          <div v-if="float32Preview && float32Preview.length > 0" class="float-preview">
            <div class="preview-summary">
              {{ t('dialog.float32Summary', { count: float32Preview.length, regCount: float32RegCount, addr: form.address }) }}
              <span v-if="float32Warning" class="preview-warn">{{ float32Warning }}</span>
            </div>
            <table class="preview-table">
              <thead><tr><th>{{ t('table.address') }}</th><th>Float</th><th>Reg[0]</th><th>Reg[1]</th></tr></thead>
              <tbody>
                <tr v-for="item in float32Preview" :key="item.index" :class="{ 'preview-error': item.error }">
                  <td>{{ form.address + item.index * 2 }}</td>
                  <td>{{ item.error || item.float }}</td>
                  <td>{{ item.regs ? '0x' + item.regs[0].toString(16).toUpperCase().padStart(4, '0') : '-' }}</td>
                  <td>{{ item.regs ? '0x' + item.regs[1].toString(16).toUpperCase().padStart(4, '0') : '-' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="submit"
            :disabled="isFloat32Mode && (!float32Valid || !!float32Warning)">{{ t('toolbar.write') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { background: #1e1e2e; border: 1px solid #45475a; border-radius: 8px; padding: 20px; min-width: 340px; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 15px; font-weight: 600; color: #cdd6f4; margin-bottom: 16px; }
.modal-body { display: flex; flex-direction: column; gap: 12px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.form-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: #6c7086; }
.form-input { padding: 6px 10px; background: #313244; border: 1px solid #45475a; border-radius: 4px; color: #cdd6f4; font-size: 13px; }
.form-input:focus { outline: none; border-color: #89b4fa; }
.form-hint { color: #6c7086; font-size: 11px; }
.form-textarea { resize: vertical; min-height: 60px; font-family: 'SF Mono', 'Fira Code', monospace; }
.btn { padding: 7px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-primary { background: #89b4fa; color: #1e1e2e; }
.btn-primary:hover { background: #74c7ec; }
.btn-primary:disabled { opacity: 0.4; cursor: default; }
.btn-secondary { background: #45475a; color: #cdd6f4; }
.btn-secondary:hover { background: #585b70; }
.float-preview { margin-top: 4px; border: 1px solid #313244; border-radius: 4px; overflow: hidden; }
.preview-summary { padding: 4px 8px; font-size: 11px; color: #a6e3a1; background: #181825; }
.preview-warn { color: #fab387; margin-left: 8px; }
.preview-table { width: 100%; border-collapse: collapse; font-size: 11px; }
.preview-table th { background: #181825; color: #6c7086; font-weight: 500; padding: 3px 8px; text-align: left; }
.preview-table td { padding: 2px 8px; color: #cdd6f4; font-family: 'SF Mono', 'Fira Code', monospace; border-top: 1px solid #1e1e2e; }
.preview-error td { color: #f38ba8; }
</style>

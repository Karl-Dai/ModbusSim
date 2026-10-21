<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { useI18n, showAlert, float32ToU16Pair, type ByteOrder } from 'shared-frontend'

const { t } = useI18n()

interface Props { show: boolean; connectionId: string | null; slaveId?: number | null }
const props = defineProps<Props>()
const writeTarget = computed(() => props.slaveId == null ? t('valuePanel.writeDefaultTarget') : t('valuePanel.writeTarget', { id: props.slaveId }))
const emit = defineEmits<{ (e: 'close'): void }>()

const form = ref({
  Function: 'write_single_register',
  Address: 0,
  value: '',
  dataType: 'raw' as 'raw' | 'float32',
  byteOrder: 'ABCD' as ByteOrder,
})

watch(() => props.show, (visible) => {
  if (!visible) return
  form.value = { Function: 'write_single_register', Address: 0, value: '', dataType: 'raw', byteOrder: 'ABCD' }
})

watch(() => form.value.Function, (fc) => {
  form.value.dataType = 'raw'
  if (fc === 'write_multiple_coils' && !form.value.value.trim()) form.value.value = '0'
})

const busy = ref(false)
const isMultiCoilFC = computed(() => form.value.Function === 'write_multiple_coils')
// Keep empty comma-separated entries so an omitted value cannot shift later addresses.
const coilTokens = computed(() => {
  const input = form.value.value.trim()
  return input ? input.split(/[,，]/).flatMap(part => part.trim() ? part.trim().split(/\s+/) : ['']) : []
})
const coilValues = computed(() => coilTokens.value.map(value => /^(1|true|on)$/i.test(value)))
const invalidQuantity = ref(false)
watch(() => form.value.value, () => { invalidQuantity.value = false })
const coilQuantity = computed({
  get: () => coilTokens.value.length,
  set: (Quantity: number | string) => {
    const count = Number(Quantity)
    invalidQuantity.value = !Number.isInteger(count) || count < 1 || count > 1968
    if (invalidQuantity.value) return
    form.value.value = Array.from({ length: count }, (_, i) => coilTokens.value[i] ?? '0').join(', ')
  },
})
const coilError = computed(() => {
  if (!isMultiCoilFC.value) return ''
  if (invalidQuantity.value || coilTokens.value.length < 1 || coilTokens.value.length > 1968) return t('errors.invalidCoilQuantity')
  const invalidIndex = coilTokens.value.findIndex(value => !/^(0|1|true|false|on|off)$/i.test(value))
  if (invalidIndex >= 0) return t('errors.invalidCoilValue', { index: invalidIndex + 1 })
  if (!Number.isInteger(form.value.Address) || form.value.Address < 0 || form.value.Address > 65535) return t('errors.invalidCoilAddress')
  if (form.value.Address + coilTokens.value.length - 1 > 65535) return t('errors.addressOverflow')
  return ''
})

function toggleCoil(index: number) {
  const tokens = [...coilTokens.value]
  tokens[index] = coilValues.value[index] ? '0' : '1'
  form.value.value = tokens.join(', ')
}

const isMultiRegFC = computed(() => form.value.Function === 'write_multiple_registers')
const isFloat32Mode = computed(() => isMultiRegFC.value && form.value.dataType === 'float32')

const float32Preview = computed(() => {
  if (!isFloat32Mode.value) return null
  const parts = form.value.value.split(',').map(s => s.trim()).filter(s => s !== '')
  if (parts.length === 0) return null
  return parts.map((input, index) => {
    const n = parseFloat(input)
    if (isNaN(n)) return { index, input, float: null, regs: null, Error: t('errors.invalidNumber') }
    const pair = float32ToU16Pair(n, form.value.byteOrder)
    return { index, input, float: n, regs: pair, Error: '' }
  })
})

const float32Valid = computed(() => {
  if (!float32Preview.value) return false
  return float32Preview.value.length > 0 && float32Preview.value.every(r => !r.Error)
})

const float32RegCount = computed(() => {
  if (!float32Preview.value) return 0
  return float32Preview.value.filter(r => !r.Error).length * 2
})

const float32Warning = computed(() => {
  if (!float32Preview.value || !float32Valid.value) return ''
  const count = float32Preview.value.length
  if (count * 2 > 123) return t('errors.overflowFC16', { count: count * 2 })
  if (form.value.Address + count * 2 - 1 > 65535) return t('errors.addressOverflow')
  return ''
})

async function submit() {
  if (!props.connectionId || busy.value || (isMultiCoilFC.value && coilError.value)) return
  busy.value = true
  try {
    const fc = form.value.Function
    if (fc === 'write_single_register') {
      await App.WriteSingleRegisterSlave(props.connectionId, props.slaveId ?? null, form.value.Address, parseInt(form.value.value))
    } else if (fc === 'write_single_coil') {
      await App.WriteSingleCoilSlave(props.connectionId, props.slaveId ?? null, form.value.Address, form.value.value === '1' || form.value.value.toLowerCase() === 'true')
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
      await App.WriteMultipleRegistersSlave(props.connectionId, props.slaveId ?? null, form.value.Address, values)
    } else if (fc === 'write_multiple_coils') {
      const values = [...coilValues.value]
      await App.WriteMultipleCoilsSlave(props.connectionId, props.slaveId ?? null, form.value.Address, values)
    }
    emit('close')
  } catch (e) { await showAlert(String(e)) }
  finally { busy.value = false }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-box" :class="{ 'coil-modal': isMultiCoilFC }">
        <div class="modal-title">{{ t('dialog.writeRegisters') }}</div>
        <div class="modal-body">
          <div class="form-hint write-target">{{ writeTarget }}</div>
          <label class="form-label">
            {{ t('table.function') }}
            <select v-model="form.Function" class="form-input">
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
            {{ isMultiCoilFC ? t('dialog.coilStartAddress') : t('table.address') }}
            <input v-model.number="form.Address" class="form-input" type="number" min="0" max="65535" />
          </label>
          <label v-if="isMultiCoilFC" class="form-label">
            {{ t('dialog.coilQuantity') }}
            <input v-model="coilQuantity" class="form-input" type="number" min="1" max="1968" step="1" :disabled="busy"
              :aria-invalid="invalidQuantity" :aria-describedby="invalidQuantity ? 'coil-value-error' : undefined" />
          </label>
          <label class="form-label">
            {{ t('dialog.simpleValue') }}
            <span id="coil-value-hint" class="form-hint" v-if="isMultiCoilFC">{{ t('dialog.coilValuesHint') }}</span>
            <span class="form-hint" v-else-if="isFloat32Mode">{{ t('dialog.valueHintFloat32') }}</span>
            <span class="form-hint" v-else-if="form.Function.includes('multiple')">{{ t('dialog.valueHintMultiple') }}</span>
            <textarea v-if="isMultiCoilFC" v-model="form.value" class="form-input form-textarea" placeholder="1, 0, 1, 0" rows="3"
              :disabled="busy" :aria-invalid="!!coilError" :aria-describedby="coilError ? 'coil-value-hint coil-value-error' : 'coil-value-hint'" />
            <textarea v-else-if="isFloat32Mode" v-model="form.value" class="form-input form-textarea" placeholder="3.14, 2.71, 1.41" rows="3" />
            <input v-else v-model="form.value" class="form-input" type="text" placeholder="0" />
          </label>
          <template v-if="isMultiCoilFC">
            <p v-if="coilError" id="coil-value-error" class="coil-error" role="alert">{{ coilError }}</p>
            <template v-else>
              <p class="coil-summary" role="status">{{ t('dialog.coilSummary', { count: coilQuantity, start: form.Address, end: form.Address + coilQuantity - 1 }) }}</p>
              <div class="coil-grid">
                <button v-for="(value, index) in coilValues" :key="index" class="coil-toggle" :class="{ on: value }"
                  :aria-label="t('dialog.coilAtAddress', { Address: form.Address + index })" :aria-pressed="value" :disabled="busy" @click="toggleCoil(index)">
                  <span>{{ form.Address + index }}</span><strong>{{ value ? 'ON' : 'OFF' }}</strong>
                </button>
              </div>
            </template>
          </template>
          <div v-if="float32Preview && float32Preview.length > 0" class="float-preview">
            <div class="preview-summary">
              {{ t('dialog.float32Summary', { count: float32Preview.length, regCount: float32RegCount, addr: form.Address }) }}
              <span v-if="float32Warning" class="preview-warn">{{ float32Warning }}</span>
            </div>
            <table class="preview-table">
              <thead><tr><th>{{ t('table.address') }}</th><th>Float</th><th>Reg[0]</th><th>Reg[1]</th></tr></thead>
              <tbody>
                <tr v-for="item in float32Preview" :key="item.index" :class="{ 'preview-error': item.Error }">
                  <td>{{ form.Address + item.index * 2 }}</td>
                  <td>{{ item.Error || item.float }}</td>
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
            :aria-busy="busy" :disabled="busy || (isMultiCoilFC && !!coilError) || (isFloat32Mode && (!float32Valid || !!float32Warning))">{{ t('toolbar.write') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; padding: 20px; min-width: 340px; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.coil-modal { width: min(520px, calc(100vw - 32px)); min-width: 0; box-sizing: border-box; max-height: calc(100vh - 32px); overflow-y: auto; }
.coil-modal .form-label, .coil-modal .form-hint { color: var(--c-subtext1); }
.coil-error { margin: 0; color: var(--c-red); font-size: 12px; }
.coil-summary { margin: 0; color: var(--c-green); font-size: 12px; }
.coil-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(84px, 1fr)); gap: 8px; max-height: 200px; overflow-y: auto; padding: 3px; }
.coil-toggle { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 8px; border: 1px solid var(--c-surface2); border-radius: 4px; background: var(--c-control-bg); color: var(--c-text); font-family: monospace; cursor: pointer; }
.coil-toggle.on { background: var(--c-success-bg); color: var(--c-success-text); border-color: var(--c-green); }
.coil-toggle:focus-visible { outline: 2px solid var(--c-blue); outline-offset: 2px; }
.coil-toggle:disabled { cursor: default; opacity: 0.6; }
.modal-title { font-size: 15px; font-weight: 600; color: var(--c-text); margin-bottom: 16px; }
.modal-body { display: flex; flex-direction: column; gap: 12px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.form-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--c-subtext0); }
.form-input { padding: 6px 10px; background: var(--c-control-bg); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 13px; }
.form-input:focus { outline: none; border-color: var(--c-blue); }
.form-hint { color: var(--c-subtext0); font-size: 11px; }
.form-textarea { resize: vertical; min-height: 60px; font-family: var(--font-mono); }
.btn { padding: 7px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-primary { background: var(--c-blue); color: var(--c-on-accent); }
.btn-primary:hover { background: var(--c-accent-hover); }
.btn-primary:disabled { opacity: 0.4; cursor: default; }
.btn-secondary { background: var(--c-neutral-bg); color: var(--c-text); }
.btn-secondary:hover { background: var(--c-hover-bg); }
.float-preview { margin-top: 4px; border: 1px solid var(--c-surface0); border-radius: 4px; overflow: hidden; }
.preview-summary { padding: 4px 8px; font-size: 11px; color: var(--c-green); background: var(--c-mantle); }
.preview-warn { color: var(--c-peach); margin-left: 8px; }
.preview-table { width: 100%; border-collapse: collapse; font-size: 11px; }
.preview-table th { background: var(--c-mantle); color: var(--c-subtext0); font-weight: 500; padding: 3px 8px; text-align: left; }
.preview-table td { padding: 2px 8px; color: var(--c-text); font-family: var(--font-mono); border-top: 1px solid var(--c-base); }
.preview-error td { color: var(--c-red); }
</style>

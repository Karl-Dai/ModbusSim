<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { ref, computed } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert } from 'shared-frontend'

const { t } = useI18n()

interface Register {
  address: number
  register_type: string
  data_type: string
  endian: string
  name: string
  comment: string
}

interface Props {
  show: boolean
  existingRegisters: Register[]
  connectionId: string
  slaveId: number
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  saved: []
}>()

const startAddress = ref<number>(0)
const endAddress = ref<number>(100)
const formType = ref('holding_register')
const formDataType = ref('uint16')
const formEndian = ref('big')
const namePrefix = ref('')
const isSaving = ref(false)

const wordWidth = computed(() => {
  const isWordArea = formType.value === 'holding_register' || formType.value === 'input_register'
  return isWordArea && ['uint32', 'int32', 'float32'].includes(formDataType.value) ? 2 : 1
})

const candidateAddresses = computed(() => {
  const s = startAddress.value ?? 0
  const e = endAddress.value ?? 0
  const result: number[] = []
  for (let address = s; address <= e; address += wordWidth.value) {
    if (address + wordWidth.value - 1 > e || address + wordWidth.value - 1 > 65535) break
    result.push(address)
  }
  return result
})

const count = computed(() => candidateAddresses.value.length)

function overlapsExisting(address: number): boolean {
  const candidateEnd = address + wordWidth.value - 1
  return props.existingRegisters.some((register) => {
    if (register.register_type !== formType.value) return false
    const isWide = (register.register_type === 'holding_register' || register.register_type === 'input_register')
      && ['uint32', 'int32', 'float32'].includes(register.data_type)
    const existingEnd = register.address + (isWide ? 1 : 0)
    return address <= existingEnd && register.address <= candidateEnd
  })
}

const existingCount = computed(() => {
  return candidateAddresses.value.filter(overlapsExisting).length
})

const newCount = computed(() => count.value - existingCount.value)

const isValid = computed(() => {
  return count.value > 0 && count.value <= 50000
})

async function handleConfirm() {
  if (!isValid.value) return
  isSaving.value = true

  const registers = []
  for (const addr of candidateAddresses.value) {
    if (overlapsExisting(addr)) continue
    registers.push({
      address: addr,
      register_type: formType.value,
      data_type: formDataType.value,
      endian: formEndian.value,
      name: namePrefix.value ? `${namePrefix.value}_${addr}` : '',
      comment: '',
    })
  }

  try {
    await invoke('import_registers', {
      request: {
        connection_id: props.connectionId,
        slave_id: props.slaveId,
        registers,
      },
    })
    emit('saved')
    emit('close')
  } catch (err) {
    await showAlert(t('errors.batchAddFailed', { err: String(err) }))
  } finally {
    isSaving.value = false
  }
}

function handleBackdropClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-backdrop')) {
    emit('close')
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" v-modal="() => emit('close')" class="modal-backdrop" @click="handleBackdropClick">
      <div class="modal">
        <div class="modal-header">
          <span class="modal-title">{{ t('batchAdd.title') }}</span>
          <button class="btn-close" @click="$emit('close')">×</button>
        </div>

        <div class="modal-body">
          <div class="form-row">
            <div class="form-group half">
              <label class="form-label">{{ t('table.startAddress') }}</label>
              <input v-model.number="startAddress" type="number" class="form-input" min="0" max="65535" />
            </div>
            <div class="form-group half">
              <label class="form-label">{{ t('table.endAddress') }}</label>
              <input v-model.number="endAddress" type="number" class="form-input" min="0" max="65535" />
            </div>
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('table.type') }}</label>
            <select v-model="formType" class="form-select">
              <option value="coil">{{ t('table.coil') }} (Coil)</option>
              <option value="discrete_input">{{ t('table.discreteInput') }} (Discrete Input)</option>
              <option value="input_register">{{ t('table.inputRegister') }} (Input Register)</option>
              <option value="holding_register">{{ t('table.holdingRegister') }} (Holding Register)</option>
            </select>
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('dialog.dataType') }}</label>
            <select v-model="formDataType" class="form-select">
              <option value="bool">Bool</option>
              <option value="uint16">UInt16</option>
              <option value="int16">Int16</option>
              <option value="uint32">UInt32</option>
              <option value="int32">Int32</option>
              <option value="float32">Float32</option>
            </select>
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('dialog.byteOrder') }}</label>
            <select v-model="formEndian" class="form-select">
              <option value="big">{{ t('dialog.byteOrderBig') }}</option>
              <option value="little">{{ t('dialog.byteOrderLittle') }}</option>
              <option value="mid_big">{{ t('dialog.byteOrderMidBig') }}</option>
              <option value="mid_little">{{ t('dialog.byteOrderMidLittle') }}</option>
            </select>
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('batchAdd.namePrefix') }}</label>
            <input v-model="namePrefix" type="text" class="form-input" :placeholder="t('batchAdd.namePrefixPlaceholder')" />
          </div>

          <div class="count-info">
            <span v-if="count > 50000" class="count-warn">{{ t('errors.rangeTooLarge') }}</span>
            <template v-else>
              <span>{{ t('batchAdd.totalCount', { count }) }}</span>
              <span v-if="existingCount > 0" class="count-skip">{{ t('batchAdd.skipCount', { count: existingCount }) }}</span>
              <span>{{ t('batchAdd.willAdd', { count: newCount }) }}</span>
            </template>
          </div>
        </div>

        <div class="modal-footer">
          <button class="btn btn-secondary" @click="$emit('close')" :disabled="isSaving">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="handleConfirm" :disabled="!isValid || isSaving">
            {{ isSaving ? t('common.adding') : t('common.confirm') }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal {
  background: var(--c-base);
  border: 1px solid var(--c-surface1);
  border-radius: 8px;
  width: 420px;
  max-width: 90vw;
  max-height: 90vh;
  overflow-y: auto;
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--c-surface0);
}

.modal-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--c-text);
}

.btn-close {
  background: none;
  border: none;
  color: var(--c-overlay0);
  font-size: 20px;
  cursor: pointer;
  padding: 0 4px;
  line-height: 1;
}

.btn-close:hover {
  color: var(--c-text);
}

.modal-body {
  padding: 20px;
}

.form-row {
  display: flex;
  gap: 12px;
}

.form-group {
  margin-bottom: 16px;
}

.form-group.half {
  flex: 1;
}

.form-label {
  display: block;
  font-size: 13px;
  color: var(--c-overlay0);
  margin-bottom: 6px;
}

.form-input,
.form-select {
  width: 100%;
  padding: 8px 12px;
  background: var(--c-crust);
  border: 1px solid var(--c-surface1);
  border-radius: 6px;
  color: var(--c-text);
  font-size: 14px;
  box-sizing: border-box;
}

.form-input:focus,
.form-select:focus {
  outline: none;
  border-color: var(--c-blue);
}

.count-info {
  font-size: 13px;
  color: var(--c-subtext0);
  padding: 8px 0;
}

.count-info strong {
  color: var(--c-green);
}

.count-skip {
  color: var(--c-peach);
}

.count-warn {
  color: var(--c-red);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 20px;
  border-top: 1px solid var(--c-surface0);
}

.btn {
  padding: 8px 20px;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 14px;
}

.btn-primary {
  background: var(--c-blue);
  color: var(--c-base);
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-secondary {
  background: var(--c-surface1);
  color: var(--c-text);
}

.btn-secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>

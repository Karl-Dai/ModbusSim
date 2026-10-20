<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { ref, watch, computed } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { useI18n, showAlert } from 'shared-frontend'

const { t } = useI18n()

interface Register {
  Address: number
  RegisterType: string
  DataType: string
  Endian: string
  Name: string
  Comment: string
}

interface Props {
  show: boolean
  mode: 'add' | 'edit'
  register?: Register
  existingRegisters: Register[]
  connectionId: string
  slaveId: number
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  saved: []
}>()

// Form state
const formName = ref('')
const formAddress = ref<number | undefined>(undefined)
const formType = ref('holding_register')
const formDataType = ref('uint16')
const formEndian = ref('big')
const formComment = ref('')
const showAdvanced = ref(false)

// Reset form when modal opens or mode/register changes
watch(
  () => [props.show, props.mode, props.register],
  () => {
    if (props.show) {
      if (props.mode === 'edit' && props.register) {
        formName.value = props.register.Name || ''
        formAddress.value = props.register.Address
        formType.value = props.register.RegisterType
        formDataType.value = props.register.DataType
        formEndian.value = props.register.Endian
        formComment.value = props.register.Comment || ''
      } else {
        formName.value = ''
        formAddress.value = undefined
        formType.value = 'holding_register'
        formDataType.value = 'uint16'
        formEndian.value = 'big'
        formComment.value = ''
      }
    }
  },
  { immediate: true }
)

const modalTitle = computed(() => (props.mode === 'add' ? t('registerEdit.addTitle') : t('registerEdit.editTitle')))

function occupiedEnd(address: number, registerType: string, dataType: string): number {
  const isWordArea = registerType === 'holding_register' || registerType === 'input_register'
  const width = isWordArea && ['uint32', 'int32', 'float32'].includes(dataType) ? 2 : 1
  return address + width - 1
}

// Check the complete occupied range, not only the point's starting address.
function hasConflict(): boolean {
  if (formAddress.value === undefined) return false
  const candidateEnd = occupiedEnd(formAddress.value, formType.value, formDataType.value)
  if (candidateEnd > 65535) return true
  return props.existingRegisters.some(
    (r) => {
      const isOriginal = props.mode === 'edit'
        && r.Address === props.register?.Address
        && r.RegisterType === props.register?.RegisterType
      if (isOriginal || r.RegisterType !== formType.value) return false
      const existingEnd = occupiedEnd(r.Address, r.RegisterType, r.DataType)
      return formAddress.value! <= existingEnd && r.Address <= candidateEnd
    }
  )
}

async function handleConfirm() {
  if (formAddress.value === undefined) return

  if (hasConflict()) {
    await showAlert(t('registerEdit.conflictBody'))
    return
  }

  await save()
}

async function save() {
  if (formAddress.value === undefined) return
  try {
    const request = {
      ConnectionID: props.connectionId,
      SlaveID: props.slaveId,
      Address: formAddress.value,
      RegisterType: formType.value,
      DataType: formDataType.value,
      Endian: formEndian.value,
      Name: formName.value,
      Comment: formComment.value,
    }
    if (props.mode === 'edit' && props.register) {
      await App.UpdateRegister({
          ...request,
          OriginalAddress: props.register.Address,
          OriginalRegisterType: props.register.RegisterType,
        })
    } else {
      await App.AddRegister({
          ...request,
        })
    }
    emit('saved')
    emit('close')
  } catch (e) {
    await showAlert(t('errors.operationFailed', { err: String(e) }))
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
        <!-- Header -->
        <div class="modal-header">
          <span class="modal-title">{{ modalTitle }}</span>
          <button class="btn-close" @click="$emit('close')">×</button>
        </div>

        <!-- Body -->
        <div class="modal-body">
          <!-- Basic fields -->
          <div class="form-group">
            <label class="form-label">{{ t('dialog.simpleName') }}</label>
            <input v-model="formName" type="text" class="form-input" :placeholder="t('registerEdit.namePlaceholder')" />
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('table.address') }}</label>
            <input v-model.number="formAddress" type="number" class="form-input" min="0" max="65535" />
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

          <!-- Advanced options toggle -->
          <div class="form-group">
            <button class="btn-link" @click="showAdvanced = !showAdvanced">
              {{ showAdvanced ? `▼ ${t('registerEdit.hideAdvanced')}` : `▶ ${t('registerEdit.advanced')}` }}
            </button>
          </div>

          <!-- Advanced fields -->
          <template v-if="showAdvanced">
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
              <label class="form-label">{{ t('registerEdit.commentLabel') }}</label>
              <input v-model="formComment" type="text" class="form-input" :placeholder="t('registerEdit.namePlaceholder')" />
            </div>
          </template>
        </div>

        <!-- Footer -->
        <div class="modal-footer">
          <button class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="handleConfirm">{{ t('common.confirm') }}</button>
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
  position: relative;
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

.form-group {
  margin-bottom: 16px;
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

.btn-link {
  background: none;
  border: none;
  color: var(--c-blue);
  font-size: 13px;
  cursor: pointer;
  padding: 0;
}

.btn-link:hover {
  text-decoration: underline;
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

.btn-secondary {
  background: var(--c-surface1);
  color: var(--c-text);
}

.btn-danger {
  background: var(--c-red);
  color: var(--c-base);
}

/* Conflict dialog */
.conflict-dialog {
  position: absolute;
  inset: 0;
  background: rgba(30, 30, 46, 0.97);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
}

.conflict-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--c-red);
  margin-bottom: 12px;
}

.conflict-body {
  font-size: 14px;
  color: var(--c-text);
  margin-bottom: 24px;
}

.conflict-footer {
  display: flex;
  gap: 12px;
}
</style>

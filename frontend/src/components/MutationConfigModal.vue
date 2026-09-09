<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { ref, watch, computed } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert } from 'shared-frontend'
import type { MutationMode, MutationConfig, RegisterDef } from '../composables/useRegisterFormat'

const { t } = useI18n()

interface Props {
  show: boolean
  register?: RegisterDef
  connectionId: string
  slaveId: number
}
const props = defineProps<Props>()
const emit = defineEmits<{ close: []; saved: [] }>()

const mode = ref<MutationMode>('flip')
const periodMs = ref(1000)
const step = ref(1)
const min = ref(0)
const max = ref(100)

// Coil / Discrete Input are single-bit: only flip applies.
const isBool = computed(
  () => props.register?.register_type === 'coil' || props.register?.register_type === 'discrete_input'
)
const isConfigured = computed(() => !!props.register?.mutation?.enabled)
const showStep = computed(() => !isBool.value && (mode.value === 'increment' || mode.value === 'decrement'))

watch(
  () => [props.show, props.register],
  () => {
    if (!props.show) return
    const m = props.register?.mutation
    if (m) {
      mode.value = m.mode
      periodMs.value = m.period_ms
      step.value = m.step
      min.value = m.min
      max.value = m.max
    } else {
      mode.value = 'flip'
      periodMs.value = 1000
      step.value = 1
      min.value = 0
      max.value = 100
    }
  },
  { immediate: true }
)

const title = computed(() => {
  const r = props.register
  return r ? `${t('mutation.title')} — ${r.register_type} @ ${r.address}` : t('mutation.title')
})

async function save() {
  if (!props.register) return
  if (!Number.isFinite(periodMs.value) || periodMs.value < 100) {
    await showAlert(t('mutation.invalidPeriod'))
    return
  }
  if (!isBool.value && (!Number.isFinite(min.value) || !Number.isFinite(max.value) || min.value > max.value)) {
    await showAlert(t('mutation.invalidRange'))
    return
  }
  if (showStep.value && (!Number.isFinite(step.value) || step.value <= 0)) {
    await showAlert(t('mutation.invalidStep'))
    return
  }
  const config: MutationConfig = {
    enabled: true,
    mode: isBool.value ? 'flip' : mode.value,
    period_ms: Math.round(periodMs.value),
    step: step.value,
    min: min.value,
    max: max.value,
  }
  try {
    await invoke('set_point_mutation', {
      request: {
        connection_id: props.connectionId,
        slave_id: props.slaveId,
        register_type: props.register.register_type,
        address: props.register.address,
        config,
      },
    })
    await invoke('set_mutation_running', { running: true })
    emit('saved')
    emit('close')
  } catch (e) {
    await showAlert(t('errors.operationFailed', { err: String(e) }))
  }
}

async function clear() {
  if (!props.register) return
  try {
    await invoke('clear_point_mutation', {
      request: {
        connection_id: props.connectionId,
        slave_id: props.slaveId,
        register_type: props.register.register_type,
        address: props.register.address,
      },
    })
    emit('saved')
    emit('close')
  } catch (e) {
    await showAlert(t('errors.operationFailed', { err: String(e) }))
  }
}

function handleBackdropClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-backdrop')) emit('close')
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" v-modal="() => emit('close')" class="modal-backdrop" @click="handleBackdropClick">
      <div class="modal">
        <div class="modal-header">
          <span class="modal-title">{{ title }}</span>
          <button class="btn-close" @click="$emit('close')">×</button>
        </div>

        <div class="modal-body">
          <div v-if="!isBool" class="form-group">
            <label class="form-label">{{ t('mutation.mode') }}</label>
            <select v-model="mode" class="form-select">
              <option value="flip">{{ t('mutation.modeFlip') }}</option>
              <option value="increment">{{ t('mutation.modeIncrement') }}</option>
              <option value="decrement">{{ t('mutation.modeDecrement') }}</option>
              <option value="random">{{ t('mutation.modeRandom') }}</option>
            </select>
          </div>
          <div v-else class="form-group">
            <div class="bool-hint">{{ t('mutation.boolHint') }}</div>
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('mutation.period') }}</label>
            <input v-model.number="periodMs" type="number" class="form-input" min="100" step="100" />
          </div>

          <div v-if="!isBool" class="form-row">
            <div class="form-group">
              <label class="form-label">{{ t('mutation.min') }}</label>
              <input v-model.number="min" type="number" class="form-input" />
            </div>
            <div class="form-group">
              <label class="form-label">{{ t('mutation.max') }}</label>
              <input v-model.number="max" type="number" class="form-input" />
            </div>
          </div>

          <div v-if="showStep" class="form-group">
            <label class="form-label">{{ t('mutation.step') }}</label>
            <input v-model.number="step" type="number" class="form-input" min="0" />
          </div>
        </div>

        <div class="modal-footer">
          <button v-if="isConfigured" class="btn btn-danger" @click="clear">{{ t('mutation.clear') }}</button>
          <button class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="save">{{ t('mutation.enable') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal { background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; width: 380px; max-width: 90vw; max-height: 90vh; overflow-y: auto; }
.modal-header { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; border-bottom: 1px solid var(--c-surface0); }
.modal-title { font-size: 15px; font-weight: 600; color: var(--c-text); }
.btn-close { background: none; border: none; color: var(--c-overlay0); font-size: 20px; cursor: pointer; padding: 0 4px; line-height: 1; }
.btn-close:hover { color: var(--c-text); }
.modal-body { padding: 20px; }
.form-row { display: flex; gap: 12px; }
.form-row .form-group { flex: 1; }
.form-group { margin-bottom: 16px; }
.form-label { display: block; font-size: 13px; color: var(--c-overlay0); margin-bottom: 6px; }
.form-input, .form-select { width: 100%; padding: 8px 12px; background: var(--c-crust); border: 1px solid var(--c-surface1); border-radius: 6px; color: var(--c-text); font-size: 14px; box-sizing: border-box; }
.form-input:focus, .form-select:focus { outline: none; border-color: var(--c-blue); }
.bool-hint { font-size: 13px; color: var(--c-subtext0); }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; padding: 16px 20px; border-top: 1px solid var(--c-surface0); }
.btn { padding: 8px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; }
.btn-primary { background: var(--c-blue); color: var(--c-base); }
.btn-secondary { background: var(--c-surface1); color: var(--c-text); }
.btn-danger { background: var(--c-red); color: var(--c-base); margin-right: auto; }
</style>

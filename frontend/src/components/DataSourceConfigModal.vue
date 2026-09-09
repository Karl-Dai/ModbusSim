<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { computed, ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { showAlert, useI18n } from 'shared-frontend'
import type { DataSource, RegisterDef } from '../composables/useRegisterFormat'

const { t } = useI18n()
const props = defineProps<{
  show: boolean
  register?: RegisterDef
  connectionId: string
  slaveId: number
}>()
const emit = defineEmits<{ close: []; saved: [] }>()

type SourceType = DataSource['type']
const sourceType = ref<SourceType>('fixed')
const updateIntervalMs = ref(1000)
const value = ref(0)
const min = ref(0)
const max = ref(100)
const amplitude = ref(100)
const frequency = ref(1)
const offset = ref(1000)
const phase = ref(0)
const periodMs = ref(1000)
const start = ref(0)
const step = ref(1)
const wrap = ref(true)
const csvValues = ref('0, 10, 20, 30')
const loopPlayback = ref(true)

const title = computed(() => {
  const register = props.register
  return register
    ? `${t('dataSource.title')} — ${register.register_type} @ ${register.address}`
    : t('dataSource.title')
})
const isConfigured = computed(() => !!props.register?.data_source)

watch(
  () => [props.show, props.register],
  () => {
    if (!props.show) return
    const config = props.register?.data_source
    updateIntervalMs.value = config?.update_interval_ms ?? 1000
    const source = config?.source
    sourceType.value = source?.type ?? 'fixed'
    if (!source) return
    switch (source.type) {
      case 'fixed': value.value = source.value; break
      case 'random': min.value = source.min; max.value = source.max; break
      case 'sine':
        amplitude.value = source.amplitude
        frequency.value = source.frequency
        offset.value = source.offset
        phase.value = source.phase
        break
      case 'sawtooth':
      case 'triangle':
        min.value = source.min
        max.value = source.max
        periodMs.value = source.period_ms
        break
      case 'counter':
        start.value = source.start
        step.value = source.step
        wrap.value = source.wrap
        break
      case 'csv_playback':
        csvValues.value = source.values.join(', ')
        loopPlayback.value = source.loop_playback
        break
    }
  },
  { immediate: true },
)

function validWord(input: number): boolean {
  return Number.isInteger(input) && input >= 0 && input <= 0xffff
}

function buildSource(): DataSource | null {
  switch (sourceType.value) {
    case 'fixed':
      return validWord(value.value) ? { type: 'fixed', value: value.value } : null
    case 'random':
      return validWord(min.value) && validWord(max.value) && min.value <= max.value
        ? { type: 'random', min: min.value, max: max.value }
        : null
    case 'sine':
      return [amplitude.value, frequency.value, offset.value, phase.value].every(Number.isFinite)
        && frequency.value >= 0
        ? { type: 'sine', amplitude: amplitude.value, frequency: frequency.value, offset: offset.value, phase: phase.value }
        : null
    case 'sawtooth':
    case 'triangle':
      return validWord(min.value) && validWord(max.value) && min.value <= max.value
        && Number.isInteger(periodMs.value) && periodMs.value >= (sourceType.value === 'triangle' ? 2 : 1)
        ? { type: sourceType.value, min: min.value, max: max.value, period_ms: periodMs.value }
        : null
    case 'counter':
      return validWord(start.value) && Number.isInteger(step.value) && step.value >= -32768 && step.value <= 32767
        ? { type: 'counter', start: start.value, step: step.value, wrap: wrap.value }
        : null
    case 'csv_playback': {
      const values = csvValues.value.split(/[\s,;]+/).filter(Boolean).map(Number)
      return values.length > 0 && values.every(validWord)
        ? { type: 'csv_playback', values, loop_playback: loopPlayback.value }
        : null
    }
  }
}

async function save() {
  if (!props.register) return
  const source = buildSource()
  if (!source || !Number.isInteger(updateIntervalMs.value) || updateIntervalMs.value < 1) {
    await showAlert(t('dataSource.invalid'))
    return
  }
  try {
    await invoke('set_data_source', {
      request: {
        connection_id: props.connectionId,
        slave_id: props.slaveId,
        register_type: props.register.register_type,
        address: props.register.address,
        source,
        update_interval_ms: updateIntervalMs.value,
      },
    })
    emit('saved')
    emit('close')
  } catch (error) {
    await showAlert(t('errors.operationFailed', { err: String(error) }))
  }
}

async function clear() {
  if (!props.register) return
  try {
    await invoke('remove_data_source', {
      connectionId: props.connectionId,
      slaveId: props.slaveId,
      registerType: props.register.register_type,
      address: props.register.address,
    })
    emit('saved')
    emit('close')
  } catch (error) {
    await showAlert(t('errors.operationFailed', { err: String(error) }))
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" v-modal="() => emit('close')" class="modal-backdrop" @click.self="$emit('close')">
      <div class="modal">
        <div class="modal-header">
          <span class="modal-title">{{ title }}</span>
          <button class="btn-close" @click="$emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label class="form-label">{{ t('dataSource.type') }}</label>
            <select v-model="sourceType" class="form-input">
              <option value="fixed">{{ t('dataSource.fixed') }}</option>
              <option value="random">{{ t('dataSource.random') }}</option>
              <option value="sine">{{ t('dataSource.sine') }}</option>
              <option value="sawtooth">{{ t('dataSource.sawtooth') }}</option>
              <option value="triangle">{{ t('dataSource.triangle') }}</option>
              <option value="counter">{{ t('dataSource.counter') }}</option>
              <option value="csv_playback">{{ t('dataSource.csv') }}</option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label">{{ t('dataSource.updateInterval') }}</label>
            <input v-model.number="updateIntervalMs" class="form-input" type="number" min="1" />
          </div>

          <div v-if="sourceType === 'fixed'" class="form-group">
            <label class="form-label">{{ t('dialog.simpleValue') }}</label>
            <input v-model.number="value" class="form-input" type="number" min="0" max="65535" />
          </div>
          <div v-if="['random', 'sawtooth', 'triangle'].includes(sourceType)" class="form-row">
            <div class="form-group"><label class="form-label">{{ t('mutation.min') }}</label><input v-model.number="min" class="form-input" type="number" /></div>
            <div class="form-group"><label class="form-label">{{ t('mutation.max') }}</label><input v-model.number="max" class="form-input" type="number" /></div>
          </div>
          <div v-if="sourceType === 'sawtooth' || sourceType === 'triangle'" class="form-group">
            <label class="form-label">{{ t('dataSource.wavePeriod') }}</label>
            <input v-model.number="periodMs" class="form-input" type="number" min="1" />
          </div>
          <template v-if="sourceType === 'sine'">
            <div class="form-row">
              <div class="form-group"><label class="form-label">{{ t('dataSource.amplitude') }}</label><input v-model.number="amplitude" class="form-input" type="number" /></div>
              <div class="form-group"><label class="form-label">{{ t('dataSource.offset') }}</label><input v-model.number="offset" class="form-input" type="number" /></div>
            </div>
            <div class="form-row">
              <div class="form-group"><label class="form-label">{{ t('dataSource.frequency') }}</label><input v-model.number="frequency" class="form-input" type="number" min="0" step="0.1" /></div>
              <div class="form-group"><label class="form-label">{{ t('dataSource.phase') }}</label><input v-model.number="phase" class="form-input" type="number" step="0.1" /></div>
            </div>
          </template>
          <template v-if="sourceType === 'counter'">
            <div class="form-row">
              <div class="form-group"><label class="form-label">{{ t('dataSource.start') }}</label><input v-model.number="start" class="form-input" type="number" /></div>
              <div class="form-group"><label class="form-label">{{ t('mutation.step') }}</label><input v-model.number="step" class="form-input" type="number" /></div>
            </div>
            <label class="check-row"><input v-model="wrap" type="checkbox" /> {{ t('dataSource.wrap') }}</label>
          </template>
          <template v-if="sourceType === 'csv_playback'">
            <div class="form-group"><label class="form-label">{{ t('dataSource.values') }}</label><textarea v-model="csvValues" class="form-input values-input" /></div>
            <label class="check-row"><input v-model="loopPlayback" type="checkbox" /> {{ t('dataSource.loop') }}</label>
          </template>
        </div>
        <div class="modal-footer">
          <button v-if="isConfigured" class="btn btn-danger" @click="clear">{{ t('dataSource.clear') }}</button>
          <button class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="save">{{ t('dataSource.enable') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; inset: 0; z-index: 1000; display: flex; align-items: center; justify-content: center; background: rgba(0, 0, 0, .6); }
.modal { width: 440px; max-width: 92vw; max-height: 90vh; overflow-y: auto; border: 1px solid var(--c-surface1); border-radius: 8px; background: var(--c-base); }
.modal-header, .modal-footer { display: flex; align-items: center; gap: 8px; padding: 16px 20px; border-color: var(--c-surface0); }
.modal-header { justify-content: space-between; border-bottom: 1px solid var(--c-surface0); }
.modal-footer { justify-content: flex-end; border-top: 1px solid var(--c-surface0); }
.modal-title { color: var(--c-text); font-size: 15px; font-weight: 600; }
.btn-close { border: 0; background: transparent; color: var(--c-overlay0); font-size: 20px; cursor: pointer; }
.modal-body { padding: 20px; }
.form-row { display: flex; gap: 12px; }
.form-row .form-group { flex: 1; }
.form-group { margin-bottom: 14px; }
.form-label { display: block; margin-bottom: 6px; color: var(--c-subtext0); font-size: 13px; }
.form-input { box-sizing: border-box; width: 100%; padding: 8px 10px; border: 1px solid var(--c-surface1); border-radius: 6px; outline: none; background: var(--c-crust); color: var(--c-text); }
.form-input:focus { border-color: var(--c-blue); }
.values-input { min-height: 72px; resize: vertical; }
.check-row { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; color: var(--c-text); font-size: 13px; }
.btn { padding: 8px 20px; border: 0; border-radius: 6px; cursor: pointer; }
.btn-primary { background: var(--c-blue); color: var(--c-base); }
.btn-secondary { background: var(--c-surface1); color: var(--c-text); }
.btn-danger { margin-right: auto; background: var(--c-red); color: var(--c-base); }
</style>

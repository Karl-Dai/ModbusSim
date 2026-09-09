<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert, useFcLabel } from 'shared-frontend'
import type { MutationMode, PointMutationInfo, RegisterDef } from '../composables/useRegisterFormat'

const { t } = useI18n()
const { registerTypeLabel } = useFcLabel()

const props = defineProps<{
  show: boolean
  connectionId: string
  slaveId: number
  selectedRegs: RegisterDef[]
  activeRows: PointMutationInfo[]
  currentValueFor: (reg: { register_type: string; address: number }) => string
}>()

const emit = defineEmits<{
  close: []
  changed: []
}>()

const period = ref(1000)
const mode = ref<MutationMode>('flip')
const step = ref(1)
const min = ref(0)
const max = ref(100)
const actionPending = ref(false)

function pointKey(reg: { register_type: string; address: number }) {
  return `${reg.register_type}@${reg.address}`
}

function pointSupportsStep(reg: RegisterDef) {
  return reg.register_type !== 'coil' && reg.register_type !== 'discrete_input'
}

const selectedSignature = computed(() =>
  props.selectedRegs.map((reg) => pointKey(reg)).join('|'),
)
const activeByKey = computed(() =>
  new Map(props.activeRows.map((row) => [pointKey(row), row])),
)
const selectionSupportsStep = computed(() =>
  props.selectedRegs.some((reg) => pointSupportsStep(reg)),
)
const anySelectedActive = computed(() =>
  props.selectedRegs.some((reg) => activeByKey.value.has(pointKey(reg))),
)
const selectedConfigSignatures = computed(() =>
  props.selectedRegs.map((reg) => {
    const active = activeByKey.value.get(pointKey(reg))
    return active
      ? `${active.mode}:${active.period_ms}:${active.step}:${active.min}:${active.max}`
      : 'inactive'
  }),
)
const mixedSelectedConfig = computed(
  () => new Set(selectedConfigSignatures.value).size > 1,
)

function applyDefaults(reg: RegisterDef) {
  const value = 0
  mode.value = 'flip'
  period.value = 1000
  if (reg.data_type === 'float32') {
    step.value = 0.5
    min.value = -1
    max.value = 1
  } else {
    step.value = 1
    min.value = Math.round((value - 100) * 1e3) / 1e3
    max.value = Math.round((value + 100) * 1e3) / 1e3
  }
}

function loadSelectionConfig() {
  const first = props.selectedRegs[0]
  if (!first) return
  const active = activeByKey.value.get(pointKey(first))
  if (active && !mixedSelectedConfig.value) {
    period.value = active.period_ms
    mode.value = active.mode
    step.value = active.step
    min.value = active.min
    max.value = active.max
    return
  }
  applyDefaults(first)
  if (!selectionSupportsStep.value) mode.value = 'flip'
}

watch(
  [() => props.show, selectedSignature],
  ([visible]) => {
    if (visible) loadSelectionConfig()
  },
  { flush: 'sync', immediate: true },
)

function close() {
  emit('close')
}

function handleBackdrop(event: MouseEvent) {
  if ((event.target as HTMLElement).classList.contains('sim-drawer-backdrop')) close()
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && props.show) close()
}

watch(
  () => props.show,
  (visible) => {
    if (visible) window.addEventListener('keydown', handleKeydown)
    else window.removeEventListener('keydown', handleKeydown)
  },
  { immediate: true },
)
onBeforeUnmount(() => window.removeEventListener('keydown', handleKeydown))

async function applyToSelection() {
  if (actionPending.value || props.selectedRegs.length === 0) return
  if (!Number.isFinite(period.value) || period.value < 100 || period.value > 60000) {
    await showAlert(t('mutation.invalidPeriod'))
    return
  }
  if (
    selectionSupportsStep.value
    && (mode.value === 'increment' || mode.value === 'decrement')
    && (!Number.isFinite(step.value) || step.value === 0)
  ) {
    await showAlert(t('mutation.invalidStep'))
    return
  }
  if (
    selectionSupportsStep.value
    && mode.value !== 'flip'
    && (!Number.isFinite(min.value) || !Number.isFinite(max.value) || min.value > max.value)
  ) {
    await showAlert(t('mutation.invalidRange'))
    return
  }

  const targets = props.selectedRegs.map((reg) => ({
    register_type: reg.register_type,
    address: reg.address,
    config: {
      enabled: true,
      mode: pointSupportsStep(reg) ? mode.value : 'flip',
      period_ms: Math.round(period.value),
      step: step.value,
      min: min.value,
      max: max.value,
    },
  }))
  const connectionId = props.connectionId
  const slaveId = props.slaveId
  actionPending.value = true
  try {
    for (const target of targets) {
      await invoke('set_point_mutation', {
        request: {
          connection_id: connectionId,
          slave_id: slaveId,
          register_type: target.register_type,
          address: target.address,
          config: target.config,
        },
      })
    }
    // Applying a point configuration starts its mutation immediately.
    await invoke('set_mutation_running', { running: true })
    emit('changed')
  } catch (error) {
    await showAlert(t('errors.operationFailed', { err: String(error) }))
  } finally {
    actionPending.value = false
  }
}

async function stopPoints(points: Array<{ register_type: string; address: number }>) {
  if (actionPending.value || points.length === 0) return
  const connectionId = props.connectionId
  const slaveId = props.slaveId
  const targets = points.map(({ register_type, address }) => ({ register_type, address }))
  actionPending.value = true
  try {
    for (const point of targets) {
      await invoke('clear_point_mutation', {
        request: {
          connection_id: connectionId,
          slave_id: slaveId,
          register_type: point.register_type,
          address: point.address,
        },
      })
    }
    emit('changed')
  } catch (error) {
    await showAlert(t('errors.operationFailed', { err: String(error) }))
  } finally {
    actionPending.value = false
  }
}

function stopSelection() {
  return stopPoints(props.selectedRegs)
}

function modeLabel(value: MutationMode) {
  if (value === 'increment') return t('mutation.modeIncrement')
  if (value === 'decrement') return t('mutation.modeDecrement')
  if (value === 'random') return t('mutation.modeRandom')
  return t('mutation.modeFlip')
}

function pointTitle(reg: { register_type: string; address: number }) {
  return `${registerTypeLabel(reg.register_type)} @ ${reg.address}`
}
</script>

<template>
  <Teleport to="body">
    <Transition name="sim-drawer">
      <div
        v-if="show"
        v-modal="close"
        class="sim-drawer-backdrop"
        @mousedown="handleBackdrop"
      >
        <aside
          class="sim-drawer"
          role="dialog"
          :aria-label="t('simulationSettings.title')"
          @mousedown.stop
        >
          <header class="sim-drawer-head">
            <div>
              <span class="sim-eyebrow">SIMULATION</span>
              <h3>{{ t('simulationSettings.title') }}</h3>
            </div>
            <button
              class="sim-close"
              :aria-label="t('common.close')"
              @click="close"
            >×</button>
          </header>

          <div class="sim-drawer-body">
            <section class="sim-section">
              <h4>{{ t('simulationSettings.selectionHint', { count: selectedRegs.length }) }}</h4>
              <p v-if="selectedRegs.length === 0" class="sim-empty">
                {{ t('simulationSettings.noSelection') }}
              </p>
              <template v-else>
                <div class="sim-selection">
                  <span
                    v-for="reg in selectedRegs.slice(0, 8)"
                    :key="pointKey(reg)"
                    class="sim-point-chip"
                  >{{ pointTitle(reg) }}</span>
                  <span v-if="selectedRegs.length > 8" class="sim-point-chip">
                    {{ t('simulationSettings.chipsMore', { count: selectedRegs.length - 8 }) }}
                  </span>
                </div>
                <p v-if="mixedSelectedConfig" class="sim-warning">
                  {{ t('simulationSettings.mixedValues') }}
                </p>

                <div class="sim-form">
                  <label>
                    <span>{{ t('mutation.period') }}</span>
                    <div class="sim-input-unit">
                      <input v-model.number="period" type="number" min="100" max="60000" step="100" />
                      <span>ms</span>
                    </div>
                  </label>

                  <div v-if="selectionSupportsStep" class="sim-mode-field">
                    <span>{{ t('mutation.mode') }}</span>
                    <div class="sim-mode-buttons">
                      <button :class="{ active: mode === 'flip' }" @click="mode = 'flip'">
                        {{ t('mutation.modeFlip') }}
                      </button>
                      <button :class="{ active: mode === 'increment' }" @click="mode = 'increment'">
                        {{ t('mutation.modeIncrement') }}
                      </button>
                      <button :class="{ active: mode === 'decrement' }" @click="mode = 'decrement'">
                        {{ t('mutation.modeDecrement') }}
                      </button>
                      <button :class="{ active: mode === 'random' }" @click="mode = 'random'">
                        {{ t('mutation.modeRandom') }}
                      </button>
                    </div>
                  </div>
                  <p v-else class="sim-hint">{{ t('simulationSettings.flipOnlyHint') }}</p>

                  <template v-if="selectionSupportsStep && mode !== 'flip'">
                    <label v-if="mode !== 'random'">
                      <span>{{ t('mutation.step') }}</span>
                      <input v-model.number="step" type="number" />
                    </label>
                    <label>
                      <span>{{ t('mutation.min') }}</span>
                      <input v-model.number="min" type="number" />
                    </label>
                    <label>
                      <span>{{ t('mutation.max') }}</span>
                      <input v-model.number="max" type="number" />
                    </label>
                  </template>
                </div>

                <div class="sim-actions">
                  <button
                    class="sim-btn sim-btn-primary"
                    :disabled="actionPending"
                    @click="applyToSelection"
                  >{{ t('simulationSettings.apply') }}</button>
                  <button
                    v-if="anySelectedActive"
                    class="sim-btn sim-btn-danger"
                    :disabled="actionPending"
                    @click="stopSelection"
                  >{{ t('simulationSettings.stopSelected') }}</button>
                </div>
              </template>
            </section>

            <section class="sim-section">
              <div class="sim-section-title">
                <h4>{{ t('simulationSettings.activeTitle') }}</h4>
                <span>{{ activeRows.length }}</span>
              </div>
              <p v-if="activeRows.length === 0" class="sim-empty">
                {{ t('simulationSettings.noActive') }}
              </p>
              <div v-else class="sim-active-list">
                <article
                  v-for="row in activeRows"
                  :key="pointKey(row)"
                  class="sim-active-card"
                >
                  <div class="sim-active-head">
                    <div>
                      <strong>{{ pointTitle(row) }}</strong>
                    </div>
                    <button
                      class="sim-row-stop"
                      :disabled="actionPending"
                      @click="stopPoints([row])"
                    >{{ t('simulationSettings.stop') }}</button>
                  </div>
                  <dl>
                    <div>
                      <dt>{{ t('mutation.mode') }}</dt>
                      <dd>{{ modeLabel(row.mode) }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('mutation.period') }}</dt>
                      <dd>{{ row.period_ms }} ms</dd>
                    </div>
                    <div v-if="row.mode === 'increment' || row.mode === 'decrement'">
                      <dt>{{ t('mutation.step') }}</dt>
                      <dd>{{ row.step }}</dd>
                    </div>
                    <div v-if="row.mode !== 'flip'">
                      <dt>{{ t('mutation.min') }} / {{ t('mutation.max') }}</dt>
                      <dd>{{ row.min }} / {{ row.max }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('simulationSettings.currentValue') }}</dt>
                      <dd class="sim-current-value">{{ currentValueFor(row) }}</dd>
                    </div>
                  </dl>
                </article>
              </div>
            </section>
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.sim-drawer-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1550;
  display: flex;
  justify-content: flex-end;
  background: rgba(17, 17, 27, 0.6);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}

.sim-drawer {
  width: 460px;
  max-width: 94vw;
  height: 100vh;
  display: flex;
  flex-direction: column;
  color: var(--c-text);
  background: var(--c-mantle);
  border-left: 1px solid var(--c-surface0);
  box-shadow: -16px 0 32px -8px rgba(0, 0, 0, 0.45);
}

.sim-drawer-head {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--c-surface0);
}

.sim-eyebrow {
  display: block;
  margin-bottom: 4px;
  color: var(--c-overlay0);
  font: 600 9.5px/1 ui-monospace, "SF Mono", Menlo, monospace;
  letter-spacing: 0.16em;
}

.sim-drawer-head h3,
.sim-section h4 {
  margin: 0;
  color: var(--c-text);
}

.sim-drawer-head h3 {
  font-size: 14px;
}

.sim-close {
  width: 28px;
  height: 28px;
  color: var(--c-overlay0);
  background: transparent;
  border: 0;
  border-radius: 4px;
  font-size: 21px;
  cursor: pointer;
}

.sim-close:hover:not(:disabled) {
  color: var(--c-text);
  background: var(--c-surface0);
}

.sim-drawer-body {
  flex: 1;
  min-height: 0;
  padding: 14px;
  overflow-y: auto;
}

.sim-section {
  margin-bottom: 14px;
  padding: 14px;
  background: var(--c-base);
  border: 1px solid var(--c-surface0);
  border-radius: 7px;
}

.sim-section h4 {
  font-size: 12px;
}

.sim-section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.sim-section-title > span {
  min-width: 22px;
  padding: 2px 6px;
  color: var(--c-subtext0);
  background: var(--c-surface0);
  border-radius: 10px;
  font: 600 10px/1.3 ui-monospace, "SF Mono", Menlo, monospace;
  text-align: center;
}

.sim-empty,
.sim-warning,
.sim-hint {
  margin: 12px 0 0;
  padding: 9px 10px;
  color: var(--c-subtext0);
  background: var(--c-mantle);
  border-left: 2px solid var(--c-surface1);
  border-radius: 3px;
  font-size: 11px;
  line-height: 1.45;
}

.sim-warning {
  color: var(--c-yellow);
  border-left-color: var(--c-yellow);
}

.sim-hint {
  margin-top: 0;
  grid-column: 1 / -1;
}

.sim-selection {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-top: 10px;
}

.sim-point-chip {
  max-width: 100%;
  padding: 3px 6px;
  overflow: hidden;
  color: var(--c-subtext0);
  background: var(--c-surface0);
  border-radius: 4px;
  font: 500 10px/1.3 ui-monospace, "SF Mono", Menlo, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sim-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;
}

.sim-form label,
.sim-mode-field {
  display: flex;
  flex-direction: column;
  gap: 5px;
  color: var(--c-subtext0);
  font-size: 11px;
}

.sim-mode-field {
  grid-column: 1 / -1;
}

.sim-form input {
  width: 100%;
  height: 30px;
  box-sizing: border-box;
  padding: 0 8px;
  color: var(--c-text);
  background: var(--c-crust);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  font: 500 12px/1 ui-monospace, "SF Mono", Menlo, monospace;
  outline: none;
}

.sim-form input:focus {
  border-color: var(--c-blue);
}

.sim-input-unit {
  position: relative;
}

.sim-input-unit input {
  padding-right: 34px;
}

.sim-input-unit span {
  position: absolute;
  top: 8px;
  right: 8px;
  color: var(--c-overlay0);
  font: 500 10px/1 ui-monospace, "SF Mono", Menlo, monospace;
}

.sim-mode-buttons {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 5px;
}

.sim-mode-buttons button {
  height: 30px;
  color: var(--c-subtext0);
  background: var(--c-crust);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  cursor: pointer;
}

.sim-mode-buttons button.active {
  color: var(--c-base);
  background: var(--c-blue);
  border-color: var(--c-blue);
  font-weight: 600;
}

.sim-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.sim-btn {
  padding: 7px 12px;
  border: 1px solid transparent;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}

.sim-btn:disabled,
.sim-row-stop:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.sim-btn-primary {
  color: var(--c-base);
  background: var(--c-blue);
  border-color: var(--c-blue);
}

.sim-btn-danger {
  color: var(--c-red);
  background: transparent;
  border-color: var(--c-red);
}

.sim-active-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
}

.sim-active-card {
  padding: 10px;
  background: var(--c-mantle);
  border: 1px solid var(--c-surface0);
  border-radius: 5px;
}

.sim-active-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}

.sim-active-head strong {
  display: block;
  font: 600 11px/1.3 ui-monospace, "SF Mono", Menlo, monospace;
}

.sim-row-stop {
  padding: 3px 7px;
  color: var(--c-red);
  background: transparent;
  border: 1px solid rgba(243, 139, 168, 0.55);
  border-radius: 4px;
  font-size: 10px;
  cursor: pointer;
}

.sim-active-card dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 10px;
  margin: 9px 0 0;
}

.sim-active-card dl > div {
  min-width: 0;
}

.sim-active-card dt {
  color: var(--c-overlay0);
  font-size: 9.5px;
}

.sim-active-card dd {
  margin: 2px 0 0;
  overflow: hidden;
  color: var(--c-subtext0);
  font: 500 11px/1.3 ui-monospace, "SF Mono", Menlo, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sim-active-card .sim-current-value {
  color: var(--c-green);
  font-weight: 700;
}

.sim-drawer-enter-active,
.sim-drawer-leave-active {
  transition: background-color 220ms ease, backdrop-filter 220ms ease;
}

.sim-drawer-enter-active .sim-drawer,
.sim-drawer-leave-active .sim-drawer {
  transition: transform 280ms cubic-bezier(0.32, 0.72, 0, 1), opacity 200ms ease;
}

.sim-drawer-enter-from,
.sim-drawer-leave-to {
  background: rgba(17, 17, 27, 0);
  backdrop-filter: blur(0);
}

.sim-drawer-enter-from .sim-drawer,
.sim-drawer-leave-to .sim-drawer {
  transform: translateX(100%);
  opacity: 0.6;
}
</style>

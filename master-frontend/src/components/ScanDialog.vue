<script setup lang="ts">
import { ref, inject, onMounted, onUnmounted, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { useI18n, showAlert } from 'shared-frontend'
import type { SlaveIdScanEvent, RegisterScanEvent, FoundRegisterDto } from '../types'

const { t } = useI18n()

const emit = defineEmits<{ (e: 'close'): void }>()
const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const refreshTree = inject<() => void>('refreshTree')!

const tab = ref<'slave' | 'register'>('slave')
const scanning = ref(false)
const autoAddGroups = ref(true)

// --- Slave ID Scan ---
const slaveScanTimeout = ref(500)
const slaveStartId = ref(1)
const slaveEndId = ref(247)
const slaveProgress = ref({ current_id: 0, total: 247, found_ids: [] as number[] })
const slaveScanDone = ref(false)
const slaveScanPhase = ref<'idle' | 'scanning_ids' | 'scanning_regs'>('idle')
const slaveScanStatus = ref('')

// --- Register Scan ---
const regFunction = ref('read_holding_registers')
const regStartAddr = ref(0)
const regEndAddr = ref(99)
const regChunkSize = ref(10)
const regTimeout = ref(1000)
const regProgress = ref({ current_address: 0, end_address: 0, found_registers: [] as FoundRegisterDto[] })
const regScanDone = ref(false)

const functionOptions = [
  { value: 'read_coils', label: 'FC01 Read Coils' },
  { value: 'read_discrete_inputs', label: 'FC02 Read Discrete Inputs' },
  { value: 'read_holding_registers', label: 'FC03 Read Holding Registers' },
  { value: 'read_input_registers', label: 'FC04 Read Input Registers' },
]

let unlistenSlave: (() => void) | null = null
let unlistenReg: (() => void) | null = null

onMounted(async () => {
  unlistenSlave = await listen<SlaveIdScanEvent>('scan-slave-progress', (event) => {
    const p = event.payload
    if (p.connection_id !== selectedConnectionId.value) return
    slaveProgress.value = { current_id: p.current_id, total: p.total, found_ids: p.found_ids }
    if (p.done || p.cancelled) {
      scanning.value = false
      slaveScanDone.value = true
      if ((p.done || p.cancelled) && p.found_ids.length > 0) {
        scanAndAddAllRegistersForSlaves(p.found_ids)
      }
    }
  })
  unlistenReg = await listen<RegisterScanEvent>('scan-register-progress', (event) => {
    const p = event.payload
    if (p.connection_id !== selectedConnectionId.value) return
    regProgress.value = { current_address: p.current_address, end_address: p.end_address, found_registers: p.found_registers }
    if (p.done || p.cancelled) {
      scanning.value = false
      regScanDone.value = true
      if ((p.done || p.cancelled) && p.found_registers.length > 0 && autoAddGroups.value) {
        createScanGroupsFromRegisters(p.found_registers, regFunction.value)
      }
    }
  })
})

onUnmounted(() => {
  unlistenSlave?.()
  unlistenReg?.()
})

async function startSlaveScan() {
  if (!selectedConnectionId.value) return
  scanning.value = true
  slaveScanDone.value = false
  slaveProgress.value = { current_id: 0, total: 247, found_ids: [] }
  try {
    await invoke('start_slave_id_scan', {
      connectionId: selectedConnectionId.value,
      request: { start_id: slaveStartId.value, end_id: slaveEndId.value, timeout_ms: slaveScanTimeout.value },
    })
  } catch (e) {
    scanning.value = false
    await showAlert(String(e))
  }
}

async function startRegisterScan() {
  if (!selectedConnectionId.value) return
  scanning.value = true
  regScanDone.value = false
  regProgress.value = { current_address: 0, end_address: regEndAddr.value, found_registers: [] }
  try {
    await invoke('start_register_scan', {
      connectionId: selectedConnectionId.value,
      request: {
        function: regFunction.value,
        start_address: regStartAddr.value,
        end_address: regEndAddr.value,
        chunk_size: regChunkSize.value,
        timeout_ms: regTimeout.value,
      },
    })
  } catch (e) {
    scanning.value = false
    await showAlert(String(e))
  }
}

async function cancelScan() {
  if (!selectedConnectionId.value) return
  const scanType = tab.value === 'slave' ? 'slave_scan' : 'register_scan'
  try {
    await invoke('cancel_scan', { connectionId: selectedConnectionId.value, scanType })
  } catch { /* ignore */ }
}

function slaveProgressPct() {
  if (slaveProgress.value.total === 0) return 0
  return (slaveProgress.value.current_id / slaveProgress.value.total) * 100
}

function regProgressPct() {
  const start = regStartAddr.value
  const end = regProgress.value.end_address || regEndAddr.value
  const cur = regProgress.value.current_address
  if (end <= start) return 0
  return ((cur - start) / (end - start)) * 100
}

function fmtHex(v: number): string {
  return '0x' + (v & 0xFFFF).toString(16).toUpperCase().padStart(4, '0')
}

// Group contiguous addresses into scan groups
function groupContiguous(regs: FoundRegisterDto[]): { start: number; count: number }[] {
  if (regs.length === 0) return []
  const sorted = [...regs].sort((a, b) => a.address - b.address)
  const groups: { start: number; count: number }[] = []
  let start = sorted[0].address
  let prev = start
  for (let i = 1; i < sorted.length; i++) {
    if (sorted[i].address === prev + 1) {
      prev = sorted[i].address
    } else {
      groups.push({ start, count: prev - start + 1 })
      start = sorted[i].address
      prev = start
    }
  }
  groups.push({ start, count: prev - start + 1 })
  return groups
}

async function createScanGroupsFromRegisters(regs: FoundRegisterDto[], fn: string) {
  if (!selectedConnectionId.value || regs.length === 0) return
  const groups = groupContiguous(regs)
  for (const g of groups) {
    await invoke('add_scan_group', {
      connectionId: selectedConnectionId.value,
      request: {
        name: `Scan ${g.start}-${g.start + g.count - 1}`,
        function: fn,
        start_address: g.start,
        quantity: g.count,
        interval_ms: 1000,
      },
    })
  }
  refreshTree()
}

async function scanAndAddAllRegistersForSlaves(slaveIds: number[]) {
  if (!selectedConnectionId.value || slaveIds.length === 0) return
  slaveScanPhase.value = 'scanning_regs'

  const fcs = [
    { fn: 'read_coils', label: 'Coil' },
    { fn: 'read_discrete_inputs', label: 'DI' },
    { fn: 'read_holding_registers', label: 'HR' },
    { fn: 'read_input_registers', label: 'IR' },
  ]

  for (const sid of slaveIds) {
    for (const fc of fcs) {
      slaveScanStatus.value = t('scanDialog.addingProgress', { id: sid, fc: fc.label })
      try {
        await invoke('add_scan_group', {
          connectionId: selectedConnectionId.value,
          request: {
            name: `S${sid} ${fc.label} 0-99`,
            function: fc.fn,
            start_address: 0,
            quantity: 100,
            interval_ms: 1000,
            slave_id: sid,
          },
        })
      } catch { /* ignore */ }
    }
  }

  slaveScanStatus.value = ''
  slaveScanPhase.value = 'idle'
  refreshTree()
}
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-box scan-dialog">
        <div class="modal-title">{{ t('scanDialog.title') }}</div>

        <div class="tab-bar">
          <button :class="['tab-btn', { active: tab === 'slave' }]" @click="tab = 'slave'" :disabled="scanning">{{ t('scanDialog.slaveScan') }}</button>
          <button :class="['tab-btn', { active: tab === 'register' }]" @click="tab = 'register'" :disabled="scanning">{{ t('scanDialog.registerScan') }}</button>
        </div>

        <!-- Slave ID Scan -->
        <div v-if="tab === 'slave'" class="tab-content">
          <div class="form-row-inline">
            <div class="form-row">
              <label>{{ t('scanDialog.startId') }}</label>
              <input v-model.number="slaveStartId" type="number" min="1" max="247" :disabled="scanning" />
            </div>
            <div class="form-row">
              <label>{{ t('scanDialog.endId') }}</label>
              <input v-model.number="slaveEndId" type="number" min="1" max="247" :disabled="scanning" />
            </div>
            <div class="form-row">
              <label>{{ t('dialog.timeout') }}</label>
              <input v-model.number="slaveScanTimeout" type="number" min="100" max="5000" step="100" :disabled="scanning" />
            </div>
          </div>

          <div class="scan-hint">{{ t('scanDialog.autoAddHint') }}</div>

          <div class="action-row">
            <button v-if="!scanning && slaveScanPhase === 'idle'" class="btn-primary" @click="startSlaveScan">{{ t('scanDialog.startScan') }}</button>
            <button v-else-if="scanning" class="btn-danger" @click="cancelScan">{{ t('common.cancel') }}</button>
            <div v-else-if="slaveScanPhase === 'scanning_regs'" class="status-text">{{ slaveScanStatus }}</div>
          </div>

          <div v-if="scanning || slaveScanDone" class="progress-section">
            <div class="progress-bar-bg">
              <div class="progress-bar-fill" :style="{ width: slaveProgressPct() + '%' }"></div>
            </div>
            <div class="progress-text">{{ slaveProgress.current_id }} / {{ slaveProgress.total }}</div>
          </div>

          <div v-if="slaveProgress.found_ids.length > 0" class="result-section">
            <div class="result-title">{{ t('scanDialog.foundSlaves', { count: slaveProgress.found_ids.length }) }}</div>
            <div class="result-scroll">
              <table class="result-table">
                <thead><tr><th>{{ t('dialog.slaveId') }}</th></tr></thead>
                <tbody>
                  <tr v-for="id in slaveProgress.found_ids" :key="id">
                    <td>{{ id }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-else-if="slaveScanDone" class="empty-result">{{ t('scanDialog.noSlavesFound') }}</div>
        </div>

        <!-- Register Scan -->
        <div v-if="tab === 'register'" class="tab-content">
          <div class="form-row">
            <label>{{ t('table.function') }}</label>
            <select v-model="regFunction" :disabled="scanning">
              <option v-for="opt in functionOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </div>
          <div class="form-row-inline">
            <div class="form-row">
              <label>{{ t('table.startAddress') }}</label>
              <input v-model.number="regStartAddr" type="number" min="0" max="65535" :disabled="scanning" />
            </div>
            <div class="form-row">
              <label>{{ t('table.endAddress') }}</label>
              <input v-model.number="regEndAddr" type="number" min="0" max="65535" :disabled="scanning" />
            </div>
          </div>
          <div class="form-row-inline">
            <div class="form-row">
              <label>{{ t('scanDialog.chunkSize') }}</label>
              <input v-model.number="regChunkSize" type="number" min="1" max="125" :disabled="scanning" />
            </div>
            <div class="form-row">
              <label>{{ t('dialog.timeout') }}</label>
              <input v-model.number="regTimeout" type="number" min="100" max="10000" step="100" :disabled="scanning" />
            </div>
          </div>

          <label class="checkbox-row">
            <input type="checkbox" v-model="autoAddGroups" :disabled="scanning" />
            <span>{{ t('scanDialog.autoAddAfterScan') }}</span>
          </label>

          <div class="action-row">
            <button v-if="!scanning" class="btn-primary" @click="startRegisterScan">{{ t('scanDialog.startScan') }}</button>
            <button v-else class="btn-danger" @click="cancelScan">{{ t('common.cancel') }}</button>
          </div>

          <div v-if="scanning || regScanDone" class="progress-section">
            <div class="progress-bar-bg">
              <div class="progress-bar-fill" :style="{ width: regProgressPct() + '%' }"></div>
            </div>
            <div class="progress-text">{{ regProgress.current_address }} / {{ regProgress.end_address }}</div>
          </div>

          <div v-if="regProgress.found_registers.length > 0" class="result-section">
            <div class="result-title">{{ t('scanDialog.foundRegisters', { count: regProgress.found_registers.length }) }}</div>
            <div class="result-scroll">
              <table class="result-table">
                <thead><tr><th>{{ t('table.address') }}</th><th>Hex</th><th>{{ t('dialog.simpleValue') }}</th></tr></thead>
                <tbody>
                  <tr v-for="reg in regProgress.found_registers" :key="reg.address">
                    <td>{{ reg.address }}</td>
                    <td class="mono">{{ fmtHex(reg.value) }}</td>
                    <td class="mono">{{ reg.value }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-else-if="regScanDone" class="empty-result">{{ t('scanDialog.noRegistersFound') }}</div>
        </div>

        <div class="modal-footer">
          <button class="btn-secondary" @click="emit('close')" :disabled="scanning">{{ t('common.close') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; width: 520px; max-height: 80vh; display: flex; flex-direction: column; }
.modal-title { padding: 12px 16px; font-size: 14px; font-weight: 600; color: var(--c-text); border-bottom: 1px solid var(--c-surface0); }
.modal-footer { padding: 8px 16px; border-top: 1px solid var(--c-surface0); display: flex; justify-content: flex-end; }

.tab-bar { display: flex; border-bottom: 1px solid var(--c-surface0); }
.tab-btn { flex: 1; padding: 8px; background: transparent; border: none; color: var(--c-subtext0); cursor: pointer; font-size: 12px; border-bottom: 2px solid transparent; }
.tab-btn.active { color: var(--c-blue); border-bottom-color: var(--c-blue); }
.tab-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.tab-content { padding: 12px 16px; overflow-y: auto; flex: 1; }

.form-row { margin-bottom: 8px; }
.form-row label { display: block; font-size: 11px; color: var(--c-subtext0); margin-bottom: 3px; }
.form-row input, .form-row select { width: 100%; padding: 5px 8px; background: var(--c-control-bg); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 12px; }
.form-row input:focus, .form-row select:focus { outline: none; border-color: var(--c-blue); }
.form-row-inline { display: flex; gap: 8px; }
.form-row-inline .form-row { flex: 1; }

.checkbox-row { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--c-text); cursor: pointer; margin-bottom: 8px; }
.checkbox-row input { accent-color: var(--c-blue); }
.action-row { margin: 10px 0; }
.btn-primary { width: 100%; padding: 6px; background: var(--c-blue); color: var(--c-on-accent); border: none; border-radius: 4px; cursor: pointer; font-size: 12px; font-weight: 600; }
.btn-primary:hover { background: var(--c-accent-hover); }
.btn-danger { width: 100%; padding: 6px; background: var(--c-red); color: var(--c-on-accent); border: none; border-radius: 4px; cursor: pointer; font-size: 12px; font-weight: 600; }
.btn-secondary { padding: 5px 16px; background: var(--c-control-bg); color: var(--c-text); border: 1px solid var(--c-surface1); border-radius: 4px; cursor: pointer; font-size: 12px; }
.btn-secondary:disabled { opacity: 0.5; cursor: not-allowed; }

.progress-section { margin: 8px 0; }
.progress-bar-bg { height: 6px; background: var(--c-control-bg); border-radius: 3px; overflow: hidden; }
.progress-bar-fill { height: 100%; background: var(--c-blue); transition: width 0.15s ease; }
.progress-text { font-size: 11px; color: var(--c-subtext0); text-align: center; margin-top: 4px; }

.result-section { margin-top: 8px; }
.result-title { font-size: 12px; color: var(--c-green); margin-bottom: 4px; font-weight: 600; }
.result-scroll { max-height: 200px; overflow-y: auto; border: 1px solid var(--c-surface0); border-radius: 4px; }
.result-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.result-table th { background: var(--c-mantle); color: var(--c-subtext0); font-weight: 500; padding: 4px 10px; text-align: left; position: sticky; top: 0; }
.result-table td { padding: 3px 10px; border-top: 1px solid var(--c-base); color: var(--c-text); }
.result-table .mono { font-family: var(--font-mono); }
.empty-result { text-align: center; color: var(--c-subtext0); font-size: 12px; padding: 16px; }
.scan-hint { font-size: 11px; color: var(--c-subtext0); margin-bottom: 8px; }
.status-text { font-size: 12px; color: var(--c-blue); text-align: center; padding: 6px; }
</style>

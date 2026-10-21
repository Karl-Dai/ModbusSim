<script setup lang="ts">
import { ref, inject, onMounted, onUnmounted, watch, type Ref } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { useI18n, showAlert, vModal } from 'shared-frontend'
import type { SlaveIdScanEvent, RegisterScanEvent, FoundRegisterDto } from '../types'
const { t } = useI18n()
const emit = defineEmits<{ (e: 'close'): void }>()
const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const refreshTree = inject<() => void>('refreshTree')!
const connectionID = selectedConnectionId.value!
const tab = ref<'slave' | 'register'>('slave')
const scanning = ref(false), cancelling = ref(false), autoAddGroups = ref(true)
const slaveScanTimeout = ref(500), slaveStartId = ref(1), slaveEndId = ref(247)
const slaveProgress = ref({ CurrentID: 0, Total: 247, FoundIDs: [] as number[] })
const slaveScanDone = ref(false), slaveScanPhase = ref<'idle' | 'scanning_ids' | 'scanning_regs'>('idle'), slaveScanStatus = ref('')
const regFunction = ref('read_holding_registers'), regStartAddr = ref(0), regEndAddr = ref(99), regChunkSize = ref(10), regTimeout = ref(1000)
const regProgress = ref({ CurrentAddress: 0, EndAddress: 0, FoundRegisters: [] as FoundRegisterDto[] })
const regScanDone = ref(false)
const functionOptions = [ { value: 'read_coils', label: 'FC01 Read Coils' }, { value: 'read_discrete_inputs', label: 'FC02 Read Discrete Inputs' }, { value: 'read_holding_registers', label: 'FC03 Read Holding Registers' }, { value: 'read_input_registers', label: 'FC04 Read Input Registers' } ]
let disposed = false, slaveTaskID: string | null = null, registerTaskID: string | null = null
let startingSlave = false, startingRegister = false
const pendingSlaves = new Map<string, SlaveIdScanEvent>(), pendingRegisters = new Map<string, RegisterScanEvent>()
let unlistenSlave: (() => void) | undefined, unlistenReg: (() => void) | undefined
function close() { if (!scanning.value && slaveScanPhase.value === 'idle') emit('close') }
async function handleSlave(p: SlaveIdScanEvent) {
 if (disposed || p.ConnectionID !== connectionID) return
 if (startingSlave) { pendingSlaves.set(p.TaskID, p); return }
 if (p.TaskID !== slaveTaskID) return
 slaveProgress.value = { CurrentID: p.CurrentID, Total: p.Total, FoundIDs: p.FoundIDs }
 if (p.Done || p.Cancelled) {
  slaveTaskID = null; scanning.value = false; cancelling.value = false; slaveScanDone.value = true; slaveScanPhase.value = 'idle'
  if (p.Done && !p.Cancelled && p.FoundIDs.length) await addSlaveGroups(p.FoundIDs)
 }
}
async function handleRegister(p: RegisterScanEvent) {
 if (disposed || p.ConnectionID !== connectionID) return
 if (startingRegister) { pendingRegisters.set(p.TaskID, p); return }
 if (p.TaskID !== registerTaskID) return
 regProgress.value = { CurrentAddress: p.CurrentAddress, EndAddress: p.EndAddress, FoundRegisters: p.FoundRegisters }
 if (p.Done || p.Cancelled) {
  registerTaskID = null; scanning.value = false; cancelling.value = false; regScanDone.value = true
  if (p.Done && !p.Cancelled && autoAddGroups.value && p.FoundRegisters.length) await addRegisterGroups(p.FoundRegisters, regFunction.value)
 }
}
function report(error: unknown) { if (!disposed) void showAlert(String(error)) }
onMounted(() => {
 unlistenSlave = EventsOn('scan-slave-progress', (p: SlaveIdScanEvent) => { void handleSlave(p).catch(report) })
 unlistenReg = EventsOn('scan-register-progress', (p: RegisterScanEvent) => { void handleRegister(p).catch(report) })
})
onUnmounted(() => {
 disposed = true; unlistenSlave?.(); unlistenReg?.()
 if (scanning.value) void App.CancelScan(connectionID, tab.value === 'slave' ? 'slave_scan' : 'register_scan').catch(() => {})
})
watch(selectedConnectionId, id => { if (id !== connectionID) emit('close') })
const intIn = (v: number, min: number, max: number) => Number.isInteger(v) && v >= min && v <= max
async function startSlaveScan() {
 if (scanning.value) return
 if (!intIn(slaveStartId.value, 1, 247) || !intIn(slaveEndId.value, slaveStartId.value, 247) || !intIn(slaveScanTimeout.value, 1, 600000)) { await showAlert(t('errors.invalidScanRange')); return }
 scanning.value = true; cancelling.value = false; startingSlave = true; slaveScanDone.value = false; slaveScanPhase.value = 'scanning_ids'
 slaveProgress.value = { CurrentID: slaveStartId.value - 1, Total: slaveEndId.value - slaveStartId.value + 1, FoundIDs: [] }
 try {
  slaveTaskID = await App.StartSlaveIDScan(connectionID, { StartID: slaveStartId.value, EndID: slaveEndId.value, TimeoutMS: slaveScanTimeout.value })
  startingSlave = false
  const pending = pendingSlaves.get(slaveTaskID); pendingSlaves.clear()
  if (disposed) { void App.CancelScan(connectionID, 'slave_scan').catch(() => {}); return }
  if (pending) await handleSlave(pending)
 } catch (e) { scanning.value = false; slaveScanPhase.value = 'idle'; report(e) }
 finally { startingSlave = false }
}
async function startRegisterScan() {
 if (scanning.value) return
 if (!intIn(regStartAddr.value, 0, 65535) || !intIn(regEndAddr.value, regStartAddr.value, 65535) || !intIn(regChunkSize.value, 1, 2000) || !intIn(regTimeout.value, 1, 600000)) { await showAlert(t('errors.invalidScanRange')); return }
 scanning.value = true; cancelling.value = false; startingRegister = true; regScanDone.value = false
 regProgress.value = { CurrentAddress: regStartAddr.value, EndAddress: regEndAddr.value, FoundRegisters: [] }
 try {
  registerTaskID = await App.StartRegisterScan(connectionID, { Function: regFunction.value, StartAddress: regStartAddr.value, EndAddress: regEndAddr.value, ChunkSize: regChunkSize.value, TimeoutMS: regTimeout.value })
  startingRegister = false
  const pending = pendingRegisters.get(registerTaskID); pendingRegisters.clear()
  if (disposed) { void App.CancelScan(connectionID, 'register_scan').catch(() => {}); return }
  if (pending) await handleRegister(pending)
 } catch (e) { scanning.value = false; report(e) }
 finally { startingRegister = false }
}
async function cancelScan() {
 if (cancelling.value) return
 cancelling.value = true
 try { await App.CancelScan(connectionID, tab.value === 'slave' ? 'slave_scan' : 'register_scan') } catch (e) { cancelling.value = false; report(e) }
}
function slaveProgressPct() { return Math.max(0, Math.min(100, (slaveProgress.value.CurrentID - slaveStartId.value + 1) / slaveProgress.value.Total * 100)) }
function regProgressPct() { return Math.max(0, Math.min(100, (regProgress.value.CurrentAddress - regStartAddr.value + 1) / (regEndAddr.value - regStartAddr.value + 1) * 100)) }
function fmtHex(v: number) { return '0x' + (v & 65535).toString(16).toUpperCase().padStart(4, '0') }
async function addRegisterGroups(regs: FoundRegisterDto[], fn: string) {
 const sorted = [...regs].sort((a,b) => a.Address - b.Address)
 const ranges: { start: number; count: number }[] = []
 for (const r of sorted) { const last = ranges.at(-1); if (last && r.Address === last.start + last.count && last.count < 65535) last.count++; else ranges.push({ start: r.Address, count: 1 }) }
 scanning.value = true
 try { for (const r of ranges) { if (disposed) return; await App.AddScanGroup(connectionID, { ID: '', Name: `Scan ${r.start}-${r.start+r.count-1}`, Function: fn, StartAddress: r.start, Quantity: r.count, IntervalMS: 1000, Enabled: true, IsPolling: false }) }; refreshTree() }
 finally { scanning.value = false }
}
async function addSlaveGroups(ids: number[]) {
 slaveScanPhase.value = 'scanning_regs'; scanning.value = true
 try {
  for (const id of ids) for (const fc of functionOptions) {
   if (disposed) return
   slaveScanStatus.value = t('scanDialog.addingProgress', { id, fc: fc.label })
   await App.AddScanGroup(connectionID, { ID: '', Name: `S${id} ${fc.label} 0-99`, Function: fc.value, StartAddress: 0, Quantity: 100, IntervalMS: 1000, Enabled: true, IsPolling: false, SlaveID: id })
  }
  refreshTree()
 } finally { slaveScanStatus.value = ''; slaveScanPhase.value = 'idle'; scanning.value = false }
}
</script>


<template>
  <Teleport to="body">
    <div class="modal-backdrop" @click.self="close">
      <div class="modal-box scan-dialog" v-modal="close" role="dialog" aria-modal="true" :aria-label="t('scanDialog.title')">
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
            <button v-else-if="scanning" class="btn-danger" :disabled="cancelling || slaveScanPhase === 'scanning_regs'" :aria-busy="cancelling" @click="cancelScan">{{ t('common.cancel') }}</button>
            <div v-else-if="slaveScanPhase === 'scanning_regs'" class="status-text">{{ slaveScanStatus }}</div>
          </div>

          <div v-if="scanning || slaveScanDone" class="progress-section">
            <div class="progress-bar-bg">
              <div class="progress-bar-fill" :style="{ width: slaveProgressPct() + '%' }"></div>
            </div>
            <div class="progress-text">{{ slaveProgress.CurrentID }} / {{ slaveProgress.Total }}</div>
          </div>

          <div v-if="slaveProgress.FoundIDs.length > 0" class="result-section">
            <div class="result-title">{{ t('scanDialog.foundSlaves', { count: slaveProgress.FoundIDs.length }) }}</div>
            <div class="result-scroll">
              <table class="result-table">
                <thead><tr><th>{{ t('dialog.slaveId') }}</th></tr></thead>
                <tbody>
                  <tr v-for="id in slaveProgress.FoundIDs" :key="id">
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
            <button v-else class="btn-danger" :disabled="cancelling || slaveScanPhase === 'scanning_regs'" :aria-busy="cancelling" @click="cancelScan">{{ t('common.cancel') }}</button>
          </div>

          <div v-if="scanning || regScanDone" class="progress-section">
            <div class="progress-bar-bg">
              <div class="progress-bar-fill" :style="{ width: regProgressPct() + '%' }"></div>
            </div>
            <div class="progress-text">{{ regProgress.CurrentAddress }} / {{ regProgress.EndAddress }}</div>
          </div>

          <div v-if="regProgress.FoundRegisters.length > 0" class="result-section">
            <div class="result-title">{{ t('scanDialog.foundRegisters', { count: regProgress.FoundRegisters.length }) }}</div>
            <div class="result-scroll">
              <table class="result-table">
                <thead><tr><th>{{ t('table.address') }}</th><th>Hex</th><th>{{ t('dialog.simpleValue') }}</th></tr></thead>
                <tbody>
                  <tr v-for="reg in regProgress.FoundRegisters" :key="reg.Address">
                    <td>{{ reg.Address }}</td>
                    <td class="mono">{{ fmtHex(reg.Value) }}</td>
                    <td class="mono">{{ reg.Value }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-else-if="regScanDone" class="empty-result">{{ t('scanDialog.noRegistersFound') }}</div>
        </div>

        <div class="modal-footer">
          <button class="btn-secondary" @click="close" :disabled="scanning">{{ t('common.close') }}</button>
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

<script setup lang="ts">
import { ref, inject, watch, computed, provide, shallowRef, onMounted, onUnmounted, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { float32ToU16Pair, useI18n, useFcLabel, formatAddress, showAlert, showConfirm, showPrompt, type ByteOrder } from 'shared-frontend'
import RegisterCsvDialog from './RegisterCsvDialog.vue'
import { encodeRegisterCsv, registerCsvTemplate } from '../utils/registerCsv'
import { saveExport } from '../utils/saveExport'
import RegisterModal from './RegisterModal.vue'
import MutationConfigModal from './MutationConfigModal.vue'
import DataSourceConfigModal from './DataSourceConfigModal.vue'
import BatchAddModal from './BatchAddModal.vue'
import SimulationSettingsDrawer from './SimulationSettingsDrawer.vue'
import { useRegisterValues } from '../composables/useRegisterValues'
import {
  formatU16, formatTypedValue, formatFloatPair, encodeTypedValue,
  is32BitType, isFloatFormat as isFloatFmt,
  type MutationMode, type PointMutationInfo,
  type RegisterDef as Register, type ValueFormat,
} from '../composables/useRegisterFormat'

const { t } = useI18n()
const { registerTypeLabel } = useFcLabel()

const emit = defineEmits<{
  (e: 'register-select', regs: { address: number; register_type: string; value: number }[]): void
}>()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedSlaveId = inject<Ref<number | null>>('selectedSlaveId')!
const selectedRegisterType = inject<Ref<string | null>>('selectedRegisterType')!
const registerRefreshKey = inject<Ref<number>>('registerRefreshKey')!

const {
  registers, registerValues, isLoading, error, changedKeys,
  loadRegisters, refreshValues, clearChangeTimers, getValue: getValueByKey,
} = useRegisterValues(selectedConnectionId, selectedSlaveId, selectedRegisterType)

const selectedRows = shallowRef<Register[]>([])
const actionBusy = ref(false)
const showCsvImport = ref(false)
const lastClickedIndex = ref<number>(-1)
const editingCell = ref<{ address: number; register_type: string } | null>(null)
const editValue = ref('')
const searchQuery = ref('')
const contextMenu = ref({ show: false, x: 0, y: 0, reg: null as Register | null })
const addrMode = ref<'hex' | 'dec'>('hex')
provide('addrMode', addrMode)
const showAddModal = ref(false)
const showEditModal = ref(false)
const editTarget = ref<Register | undefined>()
const showBatchModal = ref(false)
const showDataSourceModal = ref(false)
const dataSourceTarget = ref<Register | undefined>(undefined)

async function exportCsv() {
  if (!selectedConnectionId.value || selectedSlaveId.value === null) return
  try {
    const rows = await invoke<Register[]>('list_registers', { connectionId: selectedConnectionId.value, slaveId: selectedSlaveId.value })
    await saveExport(encodeRegisterCsv(rows), `modbus_${selectedSlaveId.value}_registers.csv`)
  } catch (e) { await showAlert(String(e)) }
}
async function template() { try { await saveExport(registerCsvTemplate(), 'modbus_register_template.csv') } catch (e) { await showAlert(String(e)) } }
function importCsv() { showCsvImport.value = true }
function simulation() {
  simTargetRegs.value = selectedRows.value.length ? [...selectedRows.value] : [...filteredRegisters.value]
  showSimDrawer.value = true
}
defineExpose({ exportCsv, template, importCsv, simulation })
async function copySelected() {
  try { await navigator.clipboard.writeText(selectedRows.value.map(reg => `${reg.register_type}\t${fmtAddress(reg)}\t${reg.name}\t${currentValueFor(reg)}`).join('\n')) }
  catch (e) { await showAlert(String(e)) }
}
async function deleteSelected() {
  const rows = [...selectedRows.value], connectionId = selectedConnectionId.value, slaveId = selectedSlaveId.value
  if (!rows.length || !connectionId || slaveId === null || actionBusy.value) return
  actionBusy.value = true
  try {
    if (!await showConfirm(t('parity.confirmDeleteRows', { n: rows.length }))) return
    let done = 0
    try {
      for (const reg of rows) {
        await invoke('remove_register', { connectionId, slaveId, address: reg.address, registerType: reg.register_type }); done++
      }
    } catch (e) { await showAlert(t('parity.partialResult', { n: done, total: rows.length }) + '\n' + String(e)) }
    clearSelection(); await loadRegisters()
  } finally { actionBusy.value = false }
}
async function writeSelected() {
  const rows = [...selectedRows.value], connectionId = selectedConnectionId.value, slaveId = selectedSlaveId.value
  if (!rows.length || !connectionId || slaveId === null || actionBusy.value) return
  actionBusy.value = true
  try {
    const text = await showPrompt(t('parity.batchValuePrompt', { n: rows.length }), '0')
    if (text === null) return
    const value = Number(text)
    if (!text.trim() || !Number.isFinite(value)) throw new Error(t('parity.invalidNumber'))
    const writes = rows.map(reg => {
      const bit = isBitType(reg.register_type)
      const ranges: Record<string, [number, number]> = { uint16: [0, 65535], int16: [-32768, 32767], uint32: [0, 4294967295], int32: [-2147483648, 2147483647] }
      const range = ranges[reg.data_type]
      if (bit && value !== 0 && value !== 1 || range && (!Number.isInteger(value) || value < range[0] || value > range[1]) || reg.data_type === 'float32' && !Number.isFinite(Math.fround(value))) throw new Error(t('parity.invalidNumber'))
      const encoded = is32BitType(reg.data_type) ? encodeTypedValue(value, reg.data_type, reg.endian) : [reg.data_type === 'int16' && value < 0 ? value + 65536 : value]
      return encoded.map((word, offset) => ({ connection_id: connectionId, slave_id: slaveId, register_type: reg.register_type, address: reg.address + offset, value: word }))
    }).flat()
    let done = 0
    try { for (const request of writes) { await invoke('write_register', { request }); done++ } }
    catch (e) { await showAlert(t('parity.partialResult', { n: done, total: writes.length }) + '\n' + String(e)) }
    await refreshValues(); emitSelection()
  } catch (e) { await showAlert(String(e)) }
  finally { actionBusy.value = false }
}

function openDataSource(reg: Register) {
  dataSourceTarget.value = reg
  showDataSourceModal.value = true
}

async function onDataSourceSaved() {
  await loadRegisters()
}

type ColumnKey = 'address' | 'name' | 'value' | 'comment'
const COLUMN_STORAGE_KEY = 'modbussim.registerTable.columnWidths.v1'
const columnMinimums: Record<ColumnKey, number> = { address: 80, name: 120, value: 100, comment: 140 }
const columnWidths = ref<Record<ColumnKey, number>>({ address: 100, name: 180, value: 140, comment: 260 })
const columnGridStyle = computed(() => ({
  '--col-address': `${columnWidths.value.address}px`,
  '--col-name': `${columnWidths.value.name}px`,
  '--col-value': `${columnWidths.value.value}px`,
  '--col-comment': `${columnWidths.value.comment}px`,
  '--table-min-width': `${Object.values(columnWidths.value).reduce((sum, width) => sum + width, 0)}px`,
}))
let stopColumnResize: (() => void) | null = null

function startColumnResize(event: PointerEvent, key: ColumnKey) {
  event.preventDefault()
  stopColumnResize?.()
  const startX = event.clientX
  const startWidth = columnWidths.value[key]
  const onMove = (moveEvent: PointerEvent) => {
    columnWidths.value = {
      ...columnWidths.value,
      [key]: Math.max(columnMinimums[key], startWidth + moveEvent.clientX - startX),
    }
  }
  const onUp = () => {
    localStorage.setItem(COLUMN_STORAGE_KEY, JSON.stringify(columnWidths.value))
    stopColumnResize?.()
  }
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', onUp, { once: true })
  stopColumnResize = () => {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    stopColumnResize = null
  }
}

// Point-mutation config modal + simulation settings drawer
const showMutationModal = ref(false)
const mutationTarget = ref<Register | undefined>(undefined)
const mutationModes = ref<Record<string, MutationMode>>({})
const mutationRows = ref<PointMutationInfo[]>([])
const showSimDrawer = ref(false)
const simTargetRegs = ref<Register[]>([])
let mutationPollTimer: number | null = null
let mutationPollPending = false
let disposed = false
const MODE_SYMBOL: Record<string, string> = { flip: '⇅', increment: '↑', decrement: '↓', random: '🎲' }
function modeSymbol(mode?: string): string {
  return mode ? (MODE_SYMBOL[mode] ?? '∿') : '∿'
}
function openMutation(reg: Register) {
  mutationTarget.value = reg
  showMutationModal.value = true
}
function mutationKey(reg: Register): string {
  return `${reg.register_type}-${reg.address}`
}
function pointMutationMode(reg: Register): MutationMode | undefined {
  return mutationModes.value[mutationKey(reg)]
    ?? (reg.mutation?.enabled ? reg.mutation.mode : undefined)
}
async function refreshMutationIndicators() {
  if (!selectedConnectionId.value || selectedSlaveId.value === null) {
    mutationModes.value = {}
    mutationRows.value = []
    return
  }
  const connectionId = selectedConnectionId.value
  const slaveId = selectedSlaveId.value
  try {
    const rows = await invoke<PointMutationInfo[]>('list_point_mutations', {
      request: {
        connection_id: selectedConnectionId.value,
        slave_id: selectedSlaveId.value,
      },
    })
    if (disposed || connectionId !== selectedConnectionId.value || slaveId !== selectedSlaveId.value) return
    mutationRows.value = rows
    mutationModes.value = Object.fromEntries(
      rows.map(row => [`${row.register_type}-${row.address}`, row.mode])
    )
  } catch {
    mutationModes.value = {}
    mutationRows.value = []
  }
}
// Keep live mutation values independent from the toolbar's presence.
async function pollMutationValues() {
  if (disposed || mutationPollPending) return
  mutationPollPending = true
  try {
    await refreshMutationIndicators()
    if (!disposed && mutationRows.value.length > 0) {
      await refreshValues()
      if (!disposed) emitSelection()
    }
  } finally { mutationPollPending = false }
}
async function onMutationSaved() {
  await loadRegisters()
  await refreshMutationIndicators()
}

/** Live value string for a point, used by the Simulation Settings drawer. */
function currentValueFor(reg: { register_type: string; address: number }): string {
  const full = registers.value.find(
    (r) => r.register_type === reg.register_type && r.address === reg.address,
  )
  const hi = getValueByKey(reg.register_type, reg.address)
  if (!full) return String(hi)
  const lo = is32BitType(full.data_type)
    ? getValueByKey(reg.register_type, reg.address + 1)
    : 0
  return formatTypedValue(full, hi, lo)
}

function openSimulationSettings() {
  const reg = contextMenu.value.reg
  contextMenu.value.show = false
  if (!reg) return
  simTargetRegs.value = selectedRows.value.length > 0 ? [...selectedRows.value] : [reg]
  showSimDrawer.value = true
}

async function onSimDrawerChanged() {
  await loadRegisters()
  await refreshMutationIndicators()
}

const valueFormat = ref<ValueFormat>('auto')

const formatOptions = computed<{ value: ValueFormat; label: string }[]>(() => [
  { value: 'auto', label: t('formats.auto') },
  { value: 'unsigned', label: t('formats.unsigned') },
  { value: 'signed', label: t('formats.signed') },
  { value: 'hex', label: t('formats.hex') },
  { value: 'binary', label: t('formats.binary') },
  { value: 'float32_abcd', label: t('formats.floatABCD') },
  { value: 'float32_cdab', label: t('formats.floatCDAB') },
  { value: 'float32_badc', label: t('formats.floatBADC') },
  { value: 'float32_dcba', label: t('formats.floatDCBA') },
])

const isFloatFormat = computed(() => isFloatFmt(valueFormat.value))

function getValue(reg: Register): number {
  return getValueByKey(reg.register_type, reg.address)
}

const companionKeys = computed(() => {
  const set = new Set<string>()
  for (const reg of registers.value) {
    if (is32BitType(reg.data_type)) {
      set.add(`${reg.register_type}-${reg.address + 1}`)
    }
  }
  return set
})

function isCompanionRegister(reg: Register): boolean {
  if (is32BitType(reg.data_type)) return false
  return companionKeys.value.has(`${reg.register_type}-${reg.address}`)
}

const floatCompanionIndices = computed(() => {
  if (!isFloatFormat.value) return new Set<number>()
  const set = new Set<number>()
  const list = filteredRegisters.value
  let i = 0
  while (i < list.length - 1) {
    if (list[i + 1].address === list[i].address + 1 && list[i + 1].register_type === list[i].register_type) {
      set.add(i + 1)
      i += 2
    } else {
      i += 1
    }
  }
  return set
})

function isDisplayCompanion(reg: Register, index: number): boolean {
  if (valueFormat.value === 'auto') return isCompanionRegister(reg)
  if (isFloatFormat.value) return floatCompanionIndices.value.has(index)
  return false
}

function getDisplayValue(reg: Register, index: number): string {
  if (valueFormat.value === 'auto') {
    const lo = registerValues.value[`${reg.register_type}-${reg.address + 1}`] ?? 0
    return formatTypedValue(reg, getValue(reg), lo)
  }
  if (isFloatFormat.value) {
    const list = filteredRegisters.value
    const nextCandidate = index + 1 < list.length ? list[index + 1] : undefined
    const nextReg = nextCandidate && nextCandidate.address === reg.address + 1 && nextCandidate.register_type === reg.register_type
      ? nextCandidate
      : undefined
    return formatFloatPair(valueFormat.value, getValue(reg), nextReg ? getValue(nextReg) : 0)
  }
  return formatU16(getValue(reg), valueFormat.value)
}

function onRegisterSaved() {
  registerRefreshKey.value++
}

const filteredRegisters = computed(() => {
  let result = registers.value
  if (selectedRegisterType.value) {
    result = result.filter(r => r.register_type === selectedRegisterType.value)
  }
  const q = searchQuery.value.trim()
  if (!q) return result
  if (q.startsWith('0x') || q.startsWith('0X')) {
    const hexPart = q.slice(2).toUpperCase()
    if (!hexPart) return result
    return result.filter(r => {
      const addrHex = r.address.toString(16).toUpperCase().padStart(4, '0')
      return addrHex.includes(hexPart)
    })
  }
  if (/^\d+$/.test(q)) {
    const num = Number(q)
    const hexPart = q.toUpperCase()
    return result.filter(r => {
      if (r.address === num) return true
      const addrHex = r.address.toString(16).toUpperCase().padStart(4, '0')
      return addrHex.includes(hexPart)
    })
  }
  const lower = q.toLowerCase()
  return result.filter(r => r.name.toLowerCase().includes(lower))
})

const scrollContainerRef = ref<HTMLElement | null>(null)
const ROW_HEIGHT = 32

const rowVirtualizer = useVirtualizer(computed(() => ({
  count: filteredRegisters.value.length,
  getScrollElement: () => scrollContainerRef.value,
  estimateSize: () => ROW_HEIGHT,
  overscan: 5,
})))

watch(searchQuery, () => {
  clearSelection()
})

watch([selectedConnectionId, selectedSlaveId, selectedRegisterType], async () => {
  clearSelection()
  clearChangeTimers()
  await loadRegisters()
  await refreshMutationIndicators()
})

watch(registerRefreshKey, async () => {
  await refreshValues()
  emitSelection()
})

onMounted(() => {
  try {
    const saved = JSON.parse(localStorage.getItem(COLUMN_STORAGE_KEY) ?? '{}') as Partial<Record<ColumnKey, number>>
    for (const key of Object.keys(columnMinimums) as ColumnKey[]) {
      const width = saved[key]
      if (typeof width === 'number' && Number.isFinite(width)) {
        columnWidths.value[key] = Math.max(columnMinimums[key], width)
      }
    }
  } catch { /* use defaults */ }
  refreshMutationIndicators()
  mutationPollTimer = window.setInterval(pollMutationValues, 2000)
})

onUnmounted(() => {
  disposed = true
  if (mutationPollTimer !== null) clearInterval(mutationPollTimer)
  stopColumnResize?.()
})

function clearSelection() {
  selectedRows.value = []
  lastClickedIndex.value = -1
  emitSelection()
}

function isSelected(reg: Register): boolean {
  return selectedRows.value.some(r => r.address === reg.address && r.register_type === reg.register_type)
}

function selectRow(e: MouseEvent, reg: Register) {
  const list = filteredRegisters.value
  const idx = list.indexOf(reg)
  const isCtrl = e.ctrlKey || e.metaKey

  if (e.shiftKey && lastClickedIndex.value >= 0) {
    const start = Math.min(lastClickedIndex.value, idx)
    const end = Math.max(lastClickedIndex.value, idx)
    selectedRows.value = list.slice(start, end + 1)
  } else if (isCtrl) {
    if (isSelected(reg)) {
      selectedRows.value = selectedRows.value.filter(r => !(r.address === reg.address && r.register_type === reg.register_type))
    } else {
      selectedRows.value = [...selectedRows.value, reg]
    }
    lastClickedIndex.value = idx
  } else {
    selectedRows.value = [reg]
    lastClickedIndex.value = idx
  }

  emitSelection()
}

function emitSelection() {
  const regs = selectedRows.value.map(r => ({
    address: r.address,
    register_type: r.register_type,
    value: getValue(r),
  }))
  emit('register-select', regs)
}

function handleTableKeydown(e: KeyboardEvent) {
  if (editingCell.value) return

  const list = filteredRegisters.value
  if (list.length === 0 || actionBusy.value) return
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') { e.preventDefault(); selectedRows.value = [...list]; emitSelection(); return }
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'c') { e.preventDefault(); void copySelected(); return }
  if (e.key === 'Escape') { clearSelection(); return }
  if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); void deleteSelected(); return }

  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    let currentIdx = -1
    if (selectedRows.value.length > 0) {
      const last = selectedRows.value[selectedRows.value.length - 1]
      currentIdx = list.findIndex(r => r.address === last.address && r.register_type === last.register_type)
    }

    let nextIdx: number
    if (e.key === 'ArrowDown') {
      nextIdx = currentIdx < list.length - 1 ? currentIdx + 1 : currentIdx
    } else {
      nextIdx = currentIdx > 0 ? currentIdx - 1 : 0
    }

    if (nextIdx >= 0 && nextIdx < list.length) {
      selectedRows.value = [list[nextIdx]]
      lastClickedIndex.value = nextIdx
      emitSelection()
      rowVirtualizer.value.scrollToIndex(nextIdx, { align: 'auto' })
    }
  }
}

function startEdit(reg: Register) {
  const idx = filteredRegisters.value.indexOf(reg)
  if (isDisplayCompanion(reg, idx)) return
  editingCell.value = { address: reg.address, register_type: reg.register_type }
  if (valueFormat.value === 'auto' || isFloatFormat.value) {
    editValue.value = getDisplayValue(reg, idx)
  } else {
    editValue.value = String(getValue(reg))
  }
}

function isBitType(rt: string): boolean {
  return rt === 'coil' || rt === 'discrete_input'
}

async function applyWrites(register_type: string, writes: Array<[number, number]>): Promise<boolean> {
  try {
    for (const [addr, value] of writes) {
      await invoke('write_register', {
        request: {
          connection_id: selectedConnectionId.value,
          slave_id: selectedSlaveId.value,
          register_type,
          address: addr,
          value,
        }
      })
      registerValues.value[`${register_type}-${addr}`] = value
    }
    emitSelection()
    return true
  } catch (e) {
    await showAlert(String(e))
    return false
  }
}

async function commitEdit() {
  if (!editingCell.value || !selectedConnectionId.value || selectedSlaveId.value === null) return
  const { address, register_type } = editingCell.value
  const reg = registers.value.find(r => r.address === address && r.register_type === register_type)
  editingCell.value = null

  const needsFloat32Write = isFloatFormat.value || (valueFormat.value === 'auto' && reg && is32BitType(reg.data_type))

  if (needsFloat32Write) {
    const inputVal = parseFloat(editValue.value)
    if (isNaN(inputVal)) return
    let hi: number, lo: number
    if (isFloatFormat.value) {
      const order = (valueFormat.value.replace('float32_', '').toUpperCase() as ByteOrder) || 'ABCD'
      ;[hi, lo] = float32ToU16Pair(inputVal, order)
    } else {
      ;[hi, lo] = encodeTypedValue(inputVal, reg!.data_type, reg!.endian)
    }
    await applyWrites(register_type, [[address, hi], [address + 1, lo]])
    return
  }

  if (valueFormat.value === 'auto' && reg && reg.data_type === 'int16') {
    const inputVal = Number(editValue.value)
    if (isNaN(inputVal)) return
    const value = inputVal < 0 ? (inputVal + 0x10000) & 0xFFFF : inputVal & 0xFFFF
    await applyWrites(register_type, [[address, value]])
    return
  }

  const inputVal = Number(editValue.value)
  if (isNaN(inputVal)) return
  const value = isBitType(register_type) ? (inputVal !== 0 ? 1 : 0) : inputVal
  await applyWrites(register_type, [[address, value]])
}

function cancelEdit() {
  editingCell.value = null
}

function handleEditKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    commitEdit()
  } else if (e.key === 'Escape') {
    cancelEdit()
  }
}

function showContextMenu(e: MouseEvent, reg: Register) {
  e.preventDefault()
  if (!isSelected(reg)) { selectedRows.value = [reg]; emitSelection() }
  contextMenu.value = { show: true, x: e.clientX, y: e.clientY, reg }
}

function closeContextMenu() {
  contextMenu.value.show = false
}

async function deleteRegister() {
  const reg = contextMenu.value.reg
  contextMenu.value.show = false
  if (!reg || !selectedConnectionId.value || selectedSlaveId.value === null) return
  if (!(await showConfirm(t('errors.confirmDeleteRegister')))) return
  try {
    await invoke('remove_register', {
      connectionId: selectedConnectionId.value,
      slaveId: selectedSlaveId.value,
      address: reg.address,
      registerType: reg.register_type,
    })
    if (isSelected(reg)) {
      selectedRows.value = selectedRows.value.filter(r => !(r.address === reg.address && r.register_type === reg.register_type))
      emitSelection()
    }
    await loadRegisters()
  } catch (e) {
    await showAlert(String(e))
  }
}

function fmtAddress(reg: Register): string {
  return formatAddress(reg.address, addrMode.value)
}

function toggleAddrMode() {
  addrMode.value = addrMode.value === 'hex' ? 'dec' : 'hex'
}
</script>

<template>
  <div class="register-table" @click="closeContextMenu">
    <div class="table-header-bar">
      <span class="table-title">
        {{ selectedRegisterType ? registerTypeLabel(selectedRegisterType) : t('table.allRegisters') }}
      </span>
      <input
        v-model="searchQuery"
        class="search-input"
        type="text"
        :placeholder="t('registerTable.searchPlaceholder')"
      />
      <button class="addr-mode-btn" @click="toggleAddrMode" :title="addrMode === 'hex' ? t('registerTable.switchToDecimal') : t('registerTable.switchToHex')">
        {{ addrMode === 'hex' ? 'HEX' : 'DEC' }}
      </button>
      <select v-model="valueFormat" class="format-select" :title="t('registerEdit.advanced')">
        <option v-for="opt in formatOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
      </select>
      <span v-if="error" class="table-error" :title="error">!</span>
      <button
        class="add-reg-btn"
        :disabled="!selectedConnectionId || selectedSlaveId === null"
        @click="showAddModal = true"
        :title="t('registerTable.addRegister')"
      >+</button>
      <button
        class="add-reg-btn batch"
        :disabled="!selectedConnectionId || selectedSlaveId === null"
        @click="showBatchModal = true"
        :title="t('registerTable.batchAddTitle')"
      >{{ t('registerTable.batchAdd') }}</button>
      <span class="table-count">{{ t('table.registerCount', { count: filteredRegisters.length }) }}</span>
    </div>

    <div v-if="selectedRows.length" class="selection-actions" role="toolbar" :aria-label="t('parity.selectedActions')">
      <span>{{ t('parity.selectedCount', { n: selectedRows.length }) }}</span>
      <button :disabled="actionBusy" @click="copySelected">{{ t('parity.copy') }}</button>
      <button :disabled="actionBusy" @click="writeSelected">{{ t('parity.batchWrite') }}</button>
      <button :disabled="actionBusy" @click="simulation">{{ t('simulationSettings.open') }}</button>
      <button :disabled="actionBusy" @click="deleteSelected">{{ t('common.delete') }}</button>
      <button :disabled="actionBusy" @click="clearSelection">{{ t('common.cancel') }}</button>
    </div>
    <div v-if="isLoading" class="table-loading">{{ t('common.loading') }}</div>
    <div v-else-if="!selectedConnectionId || selectedSlaveId === null" class="table-empty">
      {{ t('registerTable.selectSlave') }}
    </div>
    <div v-else-if="filteredRegisters.length === 0" class="table-empty">
      {{ t('registerTable.noRegisters') }}
    </div>

    <div
      v-else
      ref="scrollContainerRef"
      class="table-scroll-container"
      :style="columnGridStyle"
      tabindex="0"
      @keydown="handleTableKeydown"
    >
      <div class="table-head">
        <div class="head-cell">{{ t('table.address') }}<span class="column-resizer" @pointerdown="startColumnResize($event, 'address')"></span></div>
        <div class="head-cell">{{ t('dialog.simpleName') }}<span class="column-resizer" @pointerdown="startColumnResize($event, 'name')"></span></div>
        <div class="head-cell">{{ t('dialog.simpleValue') }}<span class="column-resizer" @pointerdown="startColumnResize($event, 'value')"></span></div>
        <div class="head-cell">{{ t('table.comment') }}<span class="column-resizer" @pointerdown="startColumnResize($event, 'comment')"></span></div>
      </div>
      <div class="virtual-body" :style="{ height: `${rowVirtualizer.getTotalSize()}px`, position: 'relative' }">
        <div
          v-for="virtualRow in rowVirtualizer.getVirtualItems()"
          :key="`${filteredRegisters[virtualRow.index]?.register_type}:${filteredRegisters[virtualRow.index]?.address}`"
          class="virtual-row"
          :class="{
            selected: isSelected(filteredRegisters[virtualRow.index]),
            'value-changed': changedKeys.has(`${filteredRegisters[virtualRow.index].register_type}-${filteredRegisters[virtualRow.index].address}`)
          }"
          :style="{
            position: 'absolute',
            top: 0,
            left: 0,
            width: '100%',
            height: `${ROW_HEIGHT}px`,
            transform: `translateY(${virtualRow.start}px)`,
          }"
          @click="selectRow($event, filteredRegisters[virtualRow.index])"
          @contextmenu.prevent="showContextMenu($event, filteredRegisters[virtualRow.index])"
        >
          <span class="vcol col-addr" :title="fmtAddress(filteredRegisters[virtualRow.index])">{{ fmtAddress(filteredRegisters[virtualRow.index]) }}</span>
          <span class="vcol col-name" :title="filteredRegisters[virtualRow.index].name || '-'">
            <button
              class="mut-badge"
              :class="{ active: !!pointMutationMode(filteredRegisters[virtualRow.index]) }"
              @click.stop="openMutation(filteredRegisters[virtualRow.index])"
              :title="t('mutation.configure')"
            >{{ modeSymbol(pointMutationMode(filteredRegisters[virtualRow.index])) }}</button>
            <button
              class="source-badge"
              :class="{ active: !!filteredRegisters[virtualRow.index].data_source }"
              @click.stop="openDataSource(filteredRegisters[virtualRow.index])"
              :title="t('dataSource.configure')"
            >≈</button>
            {{ filteredRegisters[virtualRow.index].name || '-' }}
          </span>
          <span :class="['vcol', 'col-value', { wide: valueFormat === 'auto', 'value-highlight': changedKeys.has(`${filteredRegisters[virtualRow.index].register_type}-${filteredRegisters[virtualRow.index].address}`) }]" @dblclick.stop="startEdit(filteredRegisters[virtualRow.index])">
            <template v-if="editingCell?.address === filteredRegisters[virtualRow.index].address && editingCell?.register_type === filteredRegisters[virtualRow.index].register_type">
              <input
                v-model="editValue"
                class="edit-input"
                :type="valueFormat === 'auto' || isFloatFormat ? 'text' : 'number'"
                autofocus
                @blur="commitEdit"
                @keydown="handleEditKeydown"
                @click.stop
              />
            </template>
            <template v-else>
              <span
                v-if="filteredRegisters[virtualRow.index].register_type === 'coil' || filteredRegisters[virtualRow.index].register_type === 'discrete_input'"
                :class="['bool-value', getValue(filteredRegisters[virtualRow.index]) ? 'on' : 'off']"
              >
                {{ getValue(filteredRegisters[virtualRow.index]) ? 'ON' : 'OFF' }}
              </span>
              <span v-else-if="isDisplayCompanion(filteredRegisters[virtualRow.index], virtualRow.index)" class="companion-value">&#x22EF;</span>
              <span v-else-if="valueFormat === 'auto' || isFloatFormat" class="num-value" :title="valueFormat === 'auto' ? `${filteredRegisters[virtualRow.index].data_type} (${filteredRegisters[virtualRow.index].endian})` : ''">{{ getDisplayValue(filteredRegisters[virtualRow.index], virtualRow.index) }}</span>
              <span v-else class="num-value">{{ formatU16(getValue(filteredRegisters[virtualRow.index]), valueFormat) }}</span>
            </template>
          </span>
          <span class="vcol col-comment" :title="filteredRegisters[virtualRow.index].comment || '-'">{{ filteredRegisters[virtualRow.index].comment || '-' }}</span>
        </div>
      </div>
    </div>

    <!-- Context Menu -->
    <div
      v-if="contextMenu.show"
      class="context-menu"
      :style="{ top: contextMenu.y + 'px', left: contextMenu.x + 'px' }"
      @click.stop
    >
      <div class="context-menu-item" @click="editTarget = contextMenu.reg ?? undefined; showEditModal = true; closeContextMenu()">{{ t('common.edit') }}</div>
      <div class="context-menu-item" @click="openSimulationSettings">{{ t('simulationSettings.open') }}</div>
      <div class="context-menu-item danger" @click="deleteRegister">{{ t('registerEdit.deleteRegister') }}</div>
    </div>

    <RegisterCsvDialog v-if="showCsvImport && selectedConnectionId && selectedSlaveId !== null"
      :connection-id="selectedConnectionId" :slave-id="selectedSlaveId" @close="showCsvImport = false" @imported="onRegisterSaved" />
    <RegisterModal :show="showEditModal" mode="edit" :register="editTarget" :existing-registers="registers" :connection-id="selectedConnectionId ?? ''" :slave-id="selectedSlaveId ?? 0" @close="showEditModal = false" @saved="onRegisterSaved" />
    <!-- Add Register Modal -->
    <RegisterModal
      :show="showAddModal"
      mode="add"
      :existing-registers="registers"
      :connection-id="selectedConnectionId ?? ''"
      :slave-id="selectedSlaveId ?? 0"
      @close="showAddModal = false"
      @saved="onRegisterSaved"
    />

    <!-- Point Mutation Config Modal -->
    <MutationConfigModal
      :show="showMutationModal"
      :register="mutationTarget"
      :connection-id="selectedConnectionId ?? ''"
      :slave-id="selectedSlaveId ?? 0"
      @close="showMutationModal = false"
      @saved="onMutationSaved"
    />

    <DataSourceConfigModal
      :show="showDataSourceModal"
      :register="dataSourceTarget"
      :connection-id="selectedConnectionId ?? ''"
      :slave-id="selectedSlaveId ?? 0"
      @close="showDataSourceModal = false"
      @saved="onDataSourceSaved"
    />

    <!-- Simulation Settings Drawer (batch point mutation) -->
    <SimulationSettingsDrawer
      :show="showSimDrawer"
      :connection-id="selectedConnectionId ?? ''"
      :slave-id="selectedSlaveId ?? 0"
      :selected-regs="simTargetRegs"
      :active-rows="mutationRows"
      :current-value-for="currentValueFor"
      @close="showSimDrawer = false"
      @changed="onSimDrawerChanged"
    />

    <!-- Batch Add Modal -->
    <BatchAddModal
      :show="showBatchModal"
      :existing-registers="registers"
      :connection-id="selectedConnectionId ?? ''"
      :slave-id="selectedSlaveId ?? 0"
      @close="showBatchModal = false"
      @saved="onRegisterSaved"
    />
  </div>
</template>

<style scoped>
.selection-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 6px 12px; background: var(--c-base); font-size: 12px; }
.selection-actions button { padding: 4px 8px; border: 1px solid var(--c-surface1); border-radius: 4px; background: var(--c-surface0); color: var(--c-text); cursor: pointer; }
.selection-actions span { color: var(--c-blue); }

.register-table {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.table-header-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 12px;
  border-bottom: 1px solid var(--c-surface0);
  flex-shrink: 0;
}

.table-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text);
  white-space: nowrap;
}

.search-input {
  flex: 1 1 160px;
  min-width: min(160px, 100%);
  padding: 4px 8px;
  background: var(--c-surface0);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  font-size: 12px;
  outline: none;
}

.search-input:focus {
  border-color: var(--c-blue);
}

.search-input::placeholder {
  color: var(--c-overlay0);
}

.addr-mode-btn {
  padding: 2px 8px;
  background: var(--c-surface0);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  font-size: 11px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  cursor: pointer;
  white-space: nowrap;
}

.addr-mode-btn:hover {
  background: var(--c-surface1);
}

.format-select {
  padding: 2px 6px;
  background: var(--c-surface0);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  font-size: 11px;
  cursor: pointer;
}

.format-select:focus {
  outline: none;
  border-color: var(--c-blue);
}

.add-reg-btn {
  padding: 2px 8px;
  background: var(--c-surface0);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-green);
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  line-height: 1;
}

.add-reg-btn.batch {
  font-size: 11px;
  font-weight: 400;
}

.add-reg-btn:hover:not(:disabled) {
  background: var(--c-surface1);
}

.add-reg-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.table-count {
  font-size: 11px;
  color: var(--c-overlay0);
  white-space: nowrap;
}

.table-error {
  color: var(--c-red);
  font-weight: 700;
  font-size: 14px;
  cursor: help;
}

.table-loading,
.table-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--c-overlay0);
  font-size: 13px;
}

.table-scroll-container {
  flex: 1;
  overflow: auto;
  outline: none;
  contain: strict;
}

.table-head,
.virtual-row {
  display: grid;
  grid-template-columns: var(--col-address) var(--col-name) var(--col-value) var(--col-comment);
  min-width: var(--table-min-width);
}

.table-head {
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--c-base);
  border-bottom: 1px solid var(--c-surface0);
}

.head-cell {
  position: relative;
  min-width: 0;
  padding: 6px 10px;
  color: var(--c-overlay0);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.column-resizer {
  position: absolute;
  top: 0;
  right: -3px;
  width: 7px;
  height: 100%;
  cursor: col-resize;
  touch-action: none;
}

.column-resizer:hover { background: rgba(137, 180, 250, 0.45); }

.virtual-body { min-width: var(--table-min-width); }

.table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.table thead {
  position: sticky;
  top: 0;
  z-index: 1;
}

.table th {
  background: var(--c-base);
  color: var(--c-overlay0);
  font-weight: 500;
  text-align: left;
  padding: 6px 10px;
  border-bottom: 1px solid var(--c-surface0);
  position: sticky;
  top: 0;
}

.table td {
  padding: 5px 10px;
  border-bottom: 1px solid var(--c-base);
  cursor: pointer;
}

.table tbody tr:hover {
  background: var(--c-base);
}

.table tbody tr.selected {
  background: var(--c-blue);
  color: var(--c-base);
}

.table tbody tr.selected .col-comment {
  color: var(--c-surface1);
}

.col-addr {
  font-family: 'SF Mono', 'Fira Code', monospace;
  color: var(--c-blue);
}

.table tbody tr.selected .col-addr {
  color: var(--c-base);
}

.col-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.col-comment {
  color: var(--c-overlay0);
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bool-value {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 600;
}

.bool-value.on {
  background: var(--c-green);
  color: var(--c-base);
}

.bool-value.off {
  background: var(--c-surface1);
  color: var(--c-overlay0);
}

.num-value {
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.companion-value {
  color: var(--c-surface1);
  font-size: 11px;
  font-style: italic;
}

.edit-input {
  width: 90px;
  padding: 2px 6px;
  background: var(--c-base);
  border: 1px solid var(--c-blue);
  border-radius: 3px;
  color: var(--c-text);
  font-family: monospace;
  font-size: 12px;
}

/* Virtual rows */
.virtual-row {
  align-items: center;
  cursor: pointer;
  font-size: 12px;
  border-bottom: 1px solid var(--c-base);
}

.virtual-row:hover {
  background: var(--c-base);
}

.virtual-row.selected {
  background: var(--c-blue);
  color: var(--c-base);
}

.virtual-row.selected .col-addr {
  color: var(--c-base);
}

.virtual-row.selected .col-comment {
  color: var(--c-surface1);
}

.virtual-row.value-changed {
  background: rgba(250, 179, 135, 0.18);
  transition: background 0.6s ease-out;
}

.virtual-row.value-changed.selected {
  background: var(--c-blue);
}

.col-value.value-highlight {
  color: var(--c-peach);
  font-weight: 700;
  transition: color 0.6s ease-out;
}

.vcol {
  padding: 5px 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.vcol.col-addr {
  min-width: 0;
}

.vcol.col-name {
  min-width: 0;
}

.vcol.col-value {
  min-width: 0;
}

.vcol.col-value.wide {
  min-width: 0;
}

.vcol.col-comment {
  min-width: 0;
}

/* Context Menu */
.mut-badge { background: none; border: none; color: var(--c-surface2); cursor: pointer; font-size: 11px; padding: 0 5px 0 0; line-height: 1; }
.mut-badge:hover { color: var(--c-text); }
.mut-badge.active { color: var(--c-green); font-weight: 700; }
.source-badge { background: none; border: none; color: var(--c-surface2); cursor: pointer; font-size: 13px; padding: 0 5px 0 0; line-height: 1; }
.source-badge:hover { color: var(--c-text); }
.source-badge.active { color: var(--c-blue); font-weight: 700; }

.context-menu {
  position: fixed;
  background: var(--c-base);
  border: 1px solid var(--c-surface1);
  border-radius: 6px;
  z-index: 999;
  min-width: 140px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
}

.context-menu-item {
  padding: 8px 14px;
  font-size: 13px;
  color: var(--c-text);
  cursor: pointer;
  border-radius: 6px;
}

.context-menu-item:hover {
  background: var(--c-surface0);
}

.context-menu-item.danger {
  color: var(--c-red);
}

.context-menu-item.danger:hover {
  background: #3d2a30;
}
</style>

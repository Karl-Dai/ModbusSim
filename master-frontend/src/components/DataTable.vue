<script setup lang="ts">
import { ref, inject, watch, onMounted, onUnmounted, computed, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { useI18n } from 'shared-frontend'
import type { ScanGroupInfo, RegisterValueDto, PollDataPayload, ReadResultDto } from '../types'

const { t } = useI18n()

const emit = defineEmits<{
  (e: 'register-select', regs: RegisterValueDto[]): void
}>()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedScanGroup = inject<Ref<ScanGroupInfo | null>>('selectedScanGroup')!
const addrMode = inject<Ref<'hex' | 'dec'>>('addrMode')!

const values = ref<RegisterValueDto[]>([])
const selectedIndices = ref<Set<number>>(new Set())
const lastClickedIndex = ref<number>(-1)
const searchFilter = ref('')
const lastUpdated = ref('')
const errorMsg = ref('')

// Display format for register values
type DisplayFormat = 'unsigned' | 'signed' | 'hex' | 'binary' | 'float32_abcd' | 'float32_cdab'
const displayFormat = ref<DisplayFormat>('unsigned')

const formatOptions: { value: DisplayFormat; label: string }[] = [
  { value: 'unsigned', label: 'Unsigned' },
  { value: 'signed', label: 'Signed' },
  { value: 'hex', label: 'Hex' },
  { value: 'binary', label: 'Binary' },
  { value: 'float32_abcd', label: 'Float AB CD' },
  { value: 'float32_cdab', label: 'Float CD AB' },
]

let unlisten: (() => void) | null = null
let unlistenError: (() => void) | null = null
let pollTimer: number | null = null

async function setupListeners() {
  if (unlisten) { unlisten(); unlisten = null }
  if (unlistenError) { unlistenError(); unlistenError = null }

  unlisten = await listen<PollDataPayload>('master-poll-data', (event) => {
    const payload = event.payload
    if (
      payload.connection_id === selectedConnectionId.value &&
      selectedScanGroup.value &&
      payload.scan_group_id === selectedScanGroup.value.id
    ) {
      values.value = payload.result.values
      lastUpdated.value = payload.result.timestamp
      errorMsg.value = ''
      emitSelection()
    }
  })

  unlistenError = await listen<{ connection_id: string; scan_group_id: string; error: string }>('master-poll-error', (event) => {
    const payload = event.payload
    if (
      payload.connection_id === selectedConnectionId.value &&
      selectedScanGroup.value &&
      payload.scan_group_id === selectedScanGroup.value.id
    ) {
      errorMsg.value = payload.error
    }
  })
}

function startPollTimer() {
  stopPollTimer()
  pollTimer = window.setInterval(async () => {
    await fetchCachedData()
  }, 2000)
}

function stopPollTimer() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}

async function fetchCachedData() {
  if (!selectedConnectionId.value || !selectedScanGroup.value) return
  try {
    const data = await invoke<ReadResultDto | null>('get_poll_data', {
      connectionId: selectedConnectionId.value,
      groupId: selectedScanGroup.value.id,
    })
    if (data && data.values.length > 0) {
      values.value = data.values
      lastUpdated.value = data.timestamp
      errorMsg.value = ''
    }
  } catch (_e) { /* ignore */ }
}

onMounted(async () => { await setupListeners() })
onUnmounted(() => { unlisten?.(); unlistenError?.(); stopPollTimer() })

watch([selectedConnectionId, selectedScanGroup], async () => {
  selectedIndices.value.clear()
  values.value = []
  lastUpdated.value = ''
  errorMsg.value = ''
  emit('register-select', [])
  stopPollTimer()
  if (!selectedConnectionId.value || !selectedScanGroup.value) return
  await fetchCachedData()
  startPollTimer()
}, { immediate: true })

// --- Format helpers ---

function fmtSigned(v: number): string {
  const n = v & 0xFFFF
  return (n >= 0x8000 ? n - 0x10000 : n).toString()
}

function fmtHex(v: number): string {
  return '0x' + (v & 0xFFFF).toString(16).toUpperCase().padStart(4, '0')
}

function fmtBinary(v: number): string {
  const b = (v & 0xFFFF).toString(2).padStart(16, '0')
  return `${b.slice(0, 4)} ${b.slice(4, 8)} ${b.slice(8, 12)} ${b.slice(12)}`
}

function toFloat32(hi: number, lo: number): string {
  const buf = new ArrayBuffer(4)
  const view = new DataView(buf)
  view.setUint16(0, hi & 0xFFFF)
  view.setUint16(2, lo & 0xFFFF)
  return view.getFloat32(0).toPrecision(7)
}

const isBoolScanGroup = computed(() => {
  if (!selectedScanGroup.value) return false
  return selectedScanGroup.value.function === 'read_coils' || selectedScanGroup.value.function === 'read_discrete_inputs'
})

const isFloat = computed(() => displayFormat.value === 'float32_abcd' || displayFormat.value === 'float32_cdab')

const filteredValues = computed(() => {
  if (!searchFilter.value) return values.value
  const q = searchFilter.value.toLowerCase()
  return values.value.filter(v => {
    const hexAddr = '0x' + v.address.toString(16).toUpperCase().padStart(4, '0')
    return hexAddr.toLowerCase().includes(q) ||
      v.address.toString().includes(q) ||
      v.display_value.toLowerCase().includes(q)
  })
})

// Build display rows: for float32 formats, merge consecutive register pairs
interface DisplayRow {
  address: number
  rawDisplay: string
  valueDisplay: string
  is_bool: boolean
  raw_value: number
  // original indices for selection
  sourceIndices: number[]
}

const displayRows = computed<DisplayRow[]>(() => {
  const src = filteredValues.value
  if (src.length === 0) return []

  if (isBoolScanGroup.value) {
    return src.map((v, i) => ({
      address: v.address,
      rawDisplay: '',
      valueDisplay: '',
      is_bool: true,
      raw_value: Number(v.raw_value),
      sourceIndices: [i],
    }))
  }

  const fmt = displayFormat.value

  if (fmt === 'float32_abcd' || fmt === 'float32_cdab') {
    const rows: DisplayRow[] = []
    for (let i = 0; i < src.length - 1; i += 2) {
      const hi = Number(src[i].raw_value)
      const lo = Number(src[i + 1].raw_value)
      const fv = fmt === 'float32_abcd' ? toFloat32(hi, lo) : toFloat32(lo, hi)
      rows.push({
        address: src[i].address,
        rawDisplay: `${fmtHex(hi)} ${fmtHex(lo)}`,
        valueDisplay: fv,
        is_bool: false,
        raw_value: hi,
        sourceIndices: [i, i + 1],
      })
    }
    // odd last register
    if (src.length % 2 !== 0) {
      const last = src[src.length - 1]
      rows.push({
        address: last.address,
        rawDisplay: fmtHex(Number(last.raw_value)),
        valueDisplay: '-',
        is_bool: false,
        raw_value: Number(last.raw_value),
        sourceIndices: [src.length - 1],
      })
    }
    return rows
  }

  return src.map((v, i) => {
    const raw = Number(v.raw_value)
    let valueDisplay: string
    switch (fmt) {
      case 'signed': valueDisplay = fmtSigned(raw); break
      case 'hex': valueDisplay = fmtHex(raw); break
      case 'binary': valueDisplay = fmtBinary(raw); break
      default: valueDisplay = raw.toString(); break
    }
    return {
      address: v.address,
      rawDisplay: raw.toString(),
      valueDisplay,
      is_bool: false,
      raw_value: raw,
      sourceIndices: [i],
    }
  })
})

// Virtual scrolling
const scrollContainerRef = ref<HTMLElement | null>(null)
const ROW_HEIGHT = 32

const rowVirtualizer = useVirtualizer(computed(() => ({
  count: displayRows.value.length,
  getScrollElement: () => scrollContainerRef.value,
  estimateSize: () => ROW_HEIGHT,
  overscan: 5,
})))

function fmtAddress(addr: number): string {
  if (addrMode.value === 'dec') return addr.toString()
  return '0x' + addr.toString(16).toUpperCase().padStart(4, '0')
}

function toggleAddrMode() {
  addrMode.value = addrMode.value === 'hex' ? 'dec' : 'hex'
}

function handleRowClick(index: number, event: MouseEvent) {
  if (event.ctrlKey || event.metaKey) {
    if (selectedIndices.value.has(index)) {
      selectedIndices.value.delete(index)
    } else {
      selectedIndices.value.add(index)
    }
  } else if (event.shiftKey && lastClickedIndex.value >= 0) {
    const start = Math.min(lastClickedIndex.value, index)
    const end = Math.max(lastClickedIndex.value, index)
    for (let i = start; i <= end; i++) {
      selectedIndices.value.add(i)
    }
  } else {
    selectedIndices.value.clear()
    selectedIndices.value.add(index)
  }
  lastClickedIndex.value = index
  emitSelection()
}

function emitSelection() {
  // Map display row selection back to original values
  const selected: RegisterValueDto[] = []
  for (const idx of selectedIndices.value) {
    const row = displayRows.value[idx]
    if (row) {
      for (const si of row.sourceIndices) {
        if (filteredValues.value[si]) {
          selected.push(filteredValues.value[si])
        }
      }
    }
  }
  emit('register-select', selected)
}

const fcLabel = computed(() => {
  if (!selectedScanGroup.value) return ''
  const map: Record<string, string> = {
    read_coils: '0x Coils',
    read_discrete_inputs: '1x Discrete Inputs',
    read_holding_registers: '3x Holding Registers',
    read_input_registers: '4x Input Registers',
  }
  return map[selectedScanGroup.value.function] || selectedScanGroup.value.function
})

// Column header for value
const valueColumnLabel = computed(() => {
  if (isBoolScanGroup.value) return t('dialog.simpleValue')
  const map: Record<string, string> = {
    unsigned: 'Unsigned',
    signed: 'Signed',
    hex: 'Hex',
    binary: 'Binary',
    float32_abcd: 'Float AB CD',
    float32_cdab: 'Float CD AB',
  }
  return map[displayFormat.value] || t('dialog.simpleValue')
})
</script>

<template>
  <div class="data-table-container">
    <div v-if="!selectedScanGroup" class="empty-state">
      {{ t('valuePanel.selectScanGroup') }}
    </div>

    <template v-else>
      <div class="table-header">
        <span class="header-title" :title="`${selectedScanGroup.name} - ${fcLabel}`">{{ selectedScanGroup.name }} - {{ fcLabel }}</span>
        <span v-if="errorMsg" class="error-badge" :title="errorMsg">ERR</span>

        <!-- Format selector (only for register types, not booleans) -->
        <select v-if="!isBoolScanGroup" v-model="displayFormat" class="format-select">
          <option v-for="opt in formatOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
        </select>

        <input
          v-model="searchFilter"
          class="search-input"
          type="text"
          :placeholder="t('dataTable.searchAddress')"
        />
        <button class="mode-btn" @click="toggleAddrMode">{{ addrMode === 'hex' ? 'HEX' : 'DEC' }}</button>
        <span class="register-count">{{ t('table.itemCount', { count: filteredValues.length }) }}</span>
      </div>

      <div v-if="values.length === 0 && !errorMsg" class="empty-state">
        <div>
          <div>{{ t('valuePanel.noData') }}</div>
          <div class="empty-hint">{{ t('dataTable.ensurePolling') }}</div>
        </div>
      </div>

      <div v-else-if="errorMsg && values.length === 0" class="empty-state">
        <div class="error-text">{{ errorMsg }}</div>
      </div>

      <div v-else ref="scrollContainerRef" class="table-scroll" :style="{ '--table-min-width': isBoolScanGroup ? '260px' : '430px' }">
        <table class="table">
          <colgroup>
            <col style="width: 90px" />
            <col v-if="!isBoolScanGroup" style="width: 120px" />
            <col />
          </colgroup>
          <thead>
            <tr>
              <th class="col-addr">{{ t('table.address') }}</th>
              <th v-if="!isBoolScanGroup && !isFloat" class="col-raw">{{ t('table.rawValue') }}</th>
              <th v-if="isFloat" class="col-raw">{{ t('table.rawBytes') }}</th>
              <th class="col-display">{{ valueColumnLabel }}</th>
            </tr>
          </thead>
        </table>
        <div class="virtual-body" :style="{ height: `${rowVirtualizer.getTotalSize()}px`, position: 'relative' }">
          <div
            v-for="virtualRow in rowVirtualizer.getVirtualItems()"
            :key="displayRows[virtualRow.index]?.address ?? virtualRow.index"
            class="virtual-row"
            :class="{ selected: selectedIndices.has(virtualRow.index) }"
            :style="{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              height: `${ROW_HEIGHT}px`,
              transform: `translateY(${virtualRow.start}px)`,
            }"
            @click="handleRowClick(virtualRow.index, $event)"
          >
            <span class="vcol col-addr">{{ fmtAddress(displayRows[virtualRow.index].address) }}</span>

            <!-- Bool -->
            <template v-if="displayRows[virtualRow.index].is_bool">
              <span class="vcol col-display">
                <span :class="['bool-value', displayRows[virtualRow.index].raw_value !== 0 ? 'on' : 'off']">
                  {{ displayRows[virtualRow.index].raw_value !== 0 ? 'ON' : 'OFF' }}
                </span>
              </span>
            </template>

            <!-- Register -->
            <template v-else>
              <span class="vcol col-raw">{{ displayRows[virtualRow.index].rawDisplay }}</span>
              <span class="vcol col-display">{{ displayRows[virtualRow.index].valueDisplay }}</span>
            </template>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.data-table-container {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #6c7086;
  font-size: 13px;
  text-align: center;
}

.empty-hint {
  font-size: 11px;
  color: #45475a;
  margin-top: 6px;
}

.error-text {
  color: #f38ba8;
  font-size: 12px;
}

.error-badge {
  font-size: 10px;
  background: #f38ba8;
  color: #1e1e2e;
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 600;
  cursor: help;
}

.table-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-bottom: 1px solid #313244;
  flex-shrink: 0;
  background: #1e1e2e;
}

.header-title {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  font-weight: 600;
  color: #89b4fa;
  white-space: nowrap;
}

.format-select {
  flex: none;
  padding: 2px 6px;
  background: #313244;
  border: 1px solid #45475a;
  border-radius: 4px;
  color: #cdd6f4;
  font-size: 11px;
  cursor: pointer;
}

.format-select:focus {
  outline: none;
  border-color: #89b4fa;
}

.search-input {
  flex: 1 1 140px;
  min-width: min(140px, 100%);
  max-width: 160px;
  padding: 3px 8px;
  background: #313244;
  border: 1px solid #45475a;
  border-radius: 4px;
  color: #cdd6f4;
  font-size: 12px;
  margin-left: auto;
}

.search-input:focus {
  outline: none;
  border-color: #89b4fa;
}

.mode-btn {
  padding: 2px 8px;
  background: #313244;
  border: 1px solid #45475a;
  border-radius: 4px;
  color: #cdd6f4;
  cursor: pointer;
  font-size: 11px;
  font-family: monospace;
}

.mode-btn:hover {
  background: #45475a;
}

.register-count {
  font-size: 11px;
  color: #6c7086;
  white-space: nowrap;
}

.table-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  contain: strict;
}

.table, .virtual-body {
  min-width: var(--table-min-width);
}

.table {
  position: sticky;
  top: 0;
  z-index: 1;
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  font-size: 12px;
}

.table th {
  background: #1e1e2e;
  color: #6c7086;
  font-weight: 500;
  padding: 6px 10px;
  text-align: left;
  border-bottom: 1px solid #313244;
  z-index: 1;
}

.table tbody tr {
  cursor: pointer;
}

.table tbody tr:hover {
  background: #1e1e2e;
}

.table tbody tr.selected {
  background: #89b4fa;
  color: #1e1e2e;
}

.table td {
  padding: 4px 10px;
  border-bottom: 1px solid #1e1e2e;
}

.col-addr {
  font-family: 'SF Mono', 'Fira Code', monospace;
  width: 90px;
  color: #89b4fa;
}

.table tbody tr.selected .col-addr {
  color: #1e1e2e;
}

.col-raw {
  font-family: monospace;
  width: 120px;
  color: #6c7086;
}

.col-display {
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.bool-value {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 600;
}

.bool-value.on {
  background: #a6e3a1;
  color: #1e1e2e;
}

.bool-value.off {
  background: #45475a;
  color: #6c7086;
}

/* Virtual rows */
.virtual-row {
  display: flex;
  align-items: center;
  cursor: pointer;
  font-size: 12px;
  border-bottom: 1px solid #1e1e2e;
}

.virtual-row:hover {
  background: #1e1e2e;
}

.virtual-row.selected {
  background: #89b4fa;
  color: #1e1e2e;
}

.virtual-row.selected .col-addr {
  color: #1e1e2e;
}

.vcol {
  padding: 4px 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.vcol.col-addr {
  flex: 0 0 90px;
  width: 90px;
  min-width: 90px;
}

.vcol.col-raw {
  flex: 0 0 120px;
  width: 120px;
  min-width: 120px;
  font-family: monospace;
  color: #6c7086;
}

.vcol.col-display {
  flex: 1;
  min-width: 0;
  font-family: 'SF Mono', 'Fira Code', monospace;
}
</style>

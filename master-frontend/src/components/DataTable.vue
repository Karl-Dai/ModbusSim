<script setup lang="ts">
import { ref, inject, watch, onMounted, onUnmounted, computed, type Ref } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { useI18n } from 'shared-frontend'
import type { ScanGroupInfo, RegisterValueDto, PollDataPayload, PollErrorPayload } from '../types'

const { t } = useI18n()

const emit = defineEmits<{
  (e: 'register-select', regs: RegisterValueDto[]): void
}>()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedScanGroup = inject<Ref<ScanGroupInfo | null>>('selectedScanGroup')!
const addrMode = inject<Ref<'hex' | 'dec'>>('addrMode')!
const dataRefreshKey = inject<Ref<number>>('dataRefreshKey', ref(0))

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

function setupListeners() {
  if (unlisten) { unlisten(); unlisten = null }
  if (unlistenError) { unlistenError(); unlistenError = null }

  unlisten = EventsOn('master-poll-data', (event: PollDataPayload) => {
    const payload = event
    if (
      payload.ConnectionID === selectedConnectionId.value &&
      selectedScanGroup.value &&
      payload.ScanGroupID === selectedScanGroup.value.ID
    ) {
      values.value = payload.Result.Values
      lastUpdated.value = payload.Result.Timestamp
      errorMsg.value = ''
      emitSelection()
    }
  })

  unlistenError = EventsOn('master-poll-error', (event: PollErrorPayload) => {
    const payload = event
    if (
      payload.ConnectionID === selectedConnectionId.value &&
      selectedScanGroup.value &&
      payload.ScanGroupID === selectedScanGroup.value.ID
    ) {
      errorMsg.value = payload.Error
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

let selectionGeneration = 0
async function fetchCachedData(refresh = false) {
  const id = selectedConnectionId.value, groupID = selectedScanGroup.value?.ID, generation = selectionGeneration
  if (!id || !groupID) return
  try {
    const data = await (refresh ? App.RefreshScanGroup(id, groupID) : App.GetPollData(id, groupID))
    if (generation !== selectionGeneration || id !== selectedConnectionId.value || groupID !== selectedScanGroup.value?.ID) return
    if (data) { values.value = data.Values; lastUpdated.value = data.Timestamp; if (refresh || data.Error) errorMsg.value = data.Error ?? ''; emitSelection() }
  } catch (e) { if (generation === selectionGeneration) errorMsg.value = String(e) }
}

watch(dataRefreshKey, () => { void fetchCachedData(true) })

onMounted(setupListeners)
onUnmounted(() => { selectionGeneration++; unlisten?.(); unlistenError?.(); stopPollTimer() })

watch([selectedConnectionId, selectedScanGroup], async (_value, _old, onCleanup) => {
  const generation = ++selectionGeneration
  onCleanup(() => { selectionGeneration++; stopPollTimer() })
  selectedIndices.value.clear()
  values.value = []
  lastUpdated.value = ''
  errorMsg.value = ''
  emit('register-select', [])
  stopPollTimer()
  if (!selectedConnectionId.value || !selectedScanGroup.value) return
  await fetchCachedData()
  if (generation === selectionGeneration) startPollTimer()
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
  return selectedScanGroup.value.Function === 'read_coils' || selectedScanGroup.value.Function === 'read_discrete_inputs'
})

const isFloat = computed(() => displayFormat.value === 'float32_abcd' || displayFormat.value === 'float32_cdab')

const filteredValues = computed(() => {
  if (!searchFilter.value) return values.value
  const q = searchFilter.value.toLowerCase()
  return values.value.filter(v => {
    const hexAddr = '0x' + v.Address.toString(16).toUpperCase().padStart(4, '0')
    return hexAddr.toLowerCase().includes(q) ||
      v.Address.toString().includes(q) ||
      v.DisplayValue.toLowerCase().includes(q)
  })
})

// Build display rows: for float32 formats, merge consecutive register pairs
interface DisplayRow {
  Address: number
  rawDisplay: string
  valueDisplay: string
  IsBool: boolean
  RawValue: number
  // original indices for selection
  sourceIndices: number[]
}

const displayRows = computed<DisplayRow[]>(() => {
  const src = filteredValues.value
  if (src.length === 0) return []

  if (isBoolScanGroup.value) {
    return src.map((v, i) => ({
      Address: v.Address,
      rawDisplay: '',
      valueDisplay: '',
      IsBool: true,
      RawValue: Number(v.RawValue),
      sourceIndices: [i],
    }))
  }

  const fmt = displayFormat.value

  if (fmt === 'float32_abcd' || fmt === 'float32_cdab') {
    const rows: DisplayRow[] = []
    for (let i = 0; i < src.length - 1; i += 2) {
      const hi = Number(src[i].RawValue)
      const lo = Number(src[i + 1].RawValue)
      const fv = fmt === 'float32_abcd' ? toFloat32(hi, lo) : toFloat32(lo, hi)
      rows.push({
        Address: src[i].Address,
        rawDisplay: `${fmtHex(hi)} ${fmtHex(lo)}`,
        valueDisplay: fv,
        IsBool: false,
        RawValue: hi,
        sourceIndices: [i, i + 1],
      })
    }
    // odd last register
    if (src.length % 2 !== 0) {
      const last = src[src.length - 1]
      rows.push({
        Address: last.Address,
        rawDisplay: fmtHex(Number(last.RawValue)),
        valueDisplay: '-',
        IsBool: false,
        RawValue: Number(last.RawValue),
        sourceIndices: [src.length - 1],
      })
    }
    return rows
  }

  return src.map((v, i) => {
    const raw = Number(v.RawValue)
    let valueDisplay: string
    switch (fmt) {
      case 'signed': valueDisplay = fmtSigned(raw); break
      case 'hex': valueDisplay = fmtHex(raw); break
      case 'binary': valueDisplay = fmtBinary(raw); break
      default: valueDisplay = raw.toString(); break
    }
    return {
      Address: v.Address,
      rawDisplay: raw.toString(),
      valueDisplay,
      IsBool: false,
      RawValue: raw,
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

function handleRowClick(index: number, event: MouseEvent | KeyboardEvent) {
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
  return map[selectedScanGroup.value.Function] || selectedScanGroup.value.Function
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
        <span class="header-title" :title="`${selectedScanGroup.Name} - ${fcLabel}`">{{ selectedScanGroup.Name }} - {{ fcLabel }}</span>
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

      <div v-if="errorMsg && values.length > 0" class="read-error" role="alert">{{ t('dataTable.readFailed', { error: errorMsg }) }}</div>
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
            :key="displayRows[virtualRow.index]?.Address ?? virtualRow.index"
            class="virtual-row" role="button" tabindex="0"
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
            @keydown.enter.prevent="handleRowClick(virtualRow.index, $event)"
            @keydown.space.prevent="handleRowClick(virtualRow.index, $event)"
          >
            <span class="vcol col-addr">{{ fmtAddress(displayRows[virtualRow.index].Address) }}</span>

            <!-- Bool -->
            <template v-if="displayRows[virtualRow.index].IsBool">
              <span class="vcol col-display">
                <span :class="['bool-value', displayRows[virtualRow.index].RawValue !== 0 ? 'on' : 'off']">
                  {{ displayRows[virtualRow.index].RawValue !== 0 ? 'ON' : 'OFF' }}
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
.virtual-row:focus-visible { outline: 2px solid var(--c-blue); outline-offset: -2px; }
.read-error { padding: 8px 12px; color: var(--c-red); font-size: 12px; overflow-wrap: anywhere; }
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
  color: var(--c-subtext0);
  font-size: 13px;
  text-align: center;
}

.empty-hint {
  font-size: 11px;
  color: var(--c-subtext0);
  margin-top: 6px;
}

.Error-text {
  color: var(--c-red);
  font-size: 12px;
}

.Error-badge {
  font-size: 10px;
  background: var(--c-red);
  color: var(--c-on-accent);
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
  border-bottom: 1px solid var(--c-surface0);
  flex-shrink: 0;
  background: var(--c-base);
}

.header-title {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  font-weight: 600;
  color: var(--c-blue);
  white-space: nowrap;
}

.format-select {
  flex: none;
  padding: 2px 6px;
  background: var(--c-control-bg);
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

.search-input {
  flex: 1 1 140px;
  min-width: min(140px, 100%);
  max-width: 160px;
  padding: 3px 8px;
  background: var(--c-control-bg);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  font-size: 12px;
  margin-left: auto;
}

.search-input:focus {
  outline: none;
  border-color: var(--c-blue);
}

.mode-btn {
  padding: 2px 8px;
  background: var(--c-control-bg);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  cursor: pointer;
  font-size: 11px;
  font-family: monospace;
}

.mode-btn:hover {
  background: var(--c-surface1);
}

.register-count {
  font-size: 11px;
  color: var(--c-subtext0);
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
  background: var(--c-mantle);
  color: var(--c-subtext0);
  font-weight: 500;
  padding: 6px 10px;
  text-align: left;
  border-bottom: 1px solid var(--c-surface0);
  z-index: 1;
}

.table tbody tr {
  cursor: pointer;
}

.table tbody tr:hover {
  background: var(--c-hover-bg);
}

.table tbody tr.selected {
  background: var(--c-selection-bg);
  color: var(--c-selection-text);
}

.table td {
  padding: 4px 10px;
  border-bottom: 1px solid var(--c-row-border);
}

.col-addr {
  font-family: var(--font-mono);
  width: 90px;
  color: var(--c-blue);
}

.table tbody tr.selected .col-addr {
  color: var(--c-selection-text);
}

.col-raw {
  font-family: monospace;
  width: 120px;
  color: var(--c-subtext0);
}

.col-display {
  font-family: var(--font-mono);
}

.bool-value {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 600;
}

.bool-value.on {
  background: var(--c-success-bg);
  color: var(--c-success-text);
}

.bool-value.off {
  background: var(--c-neutral-bg);
  color: var(--c-neutral-text);
}

/* Virtual rows */
.virtual-row {
  display: flex;
  align-items: center;
  cursor: pointer;
  font-size: 12px;
  border-bottom: 1px solid var(--c-row-border);
}

.virtual-row:hover {
  background: var(--c-hover-bg);
}

.virtual-row.selected {
  background: var(--c-selection-bg);
  color: var(--c-selection-text);
}

.virtual-row.selected .col-addr {
  color: var(--c-selection-text);
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
  color: var(--c-subtext0);
}

.vcol.col-display {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
}
</style>

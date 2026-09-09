<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useResizableColumns } from '../composables/useResizableColumns'
import { listen, type UnlistenFn } from '@tauri-apps/api/event'
import { useI18n } from '../i18n'
import { useLogPanel, type LogPanelDataSource } from '../composables/useLogPanel'
import { useLogFilter } from '../composables/useLogFilter'

interface ConnectionItem {
  id: string
  label: string
}

interface LogAppendedEvent {
  connection_id: string
}

interface Props {
  expanded: boolean
  /** Always-up-to-date list of connections from the parent. */
  connections: ConnectionItem[]
  /** Backend command bindings — see LogPanelDataSource. */
  source: LogPanelDataSource
  /** Filename prefix for the CSV export. */
  exportPrefix?: string
  /** When set, will auto-select this id when present in `connections`. */
  pinnedConnectionId?: string | null
  /** Optional formatter, e.g. for mapping `read_holding_registers` → `FC03`. */
  fcFormatter?: (fc: string) => string
  /** Optional override for timestamp formatting. */
  timestampFormatter?: (ts: string) => string
}

const props = withDefaults(defineProps<Props>(), {
  exportPrefix: 'modbus_log',
  pinnedConnectionId: null,
  fcFormatter: (fc: string) => fc,
  timestampFormatter: undefined,
})

const emit = defineEmits<{ (e: 'toggle'): void }>()

const { t, locale } = useI18n()
const { logs, isLoading, error, loadLogs, clearLogs, exportLogsCsv, reset } = useLogPanel(props.source)
const { searchQuery, directionFilter, fcFilter, filteredLogs, availableFcs, filterSummary } = useLogFilter(logs)

const paused = ref(false)
const autoScroll = ref(true)
const logBody = ref<HTMLElement | null>(null)
const { widths, startResize, resizeWithKeyboard } = useResizableColumns({ time: 110, direction: 70, function: 100, detail: 500 }, { time: 80, direction: 60, function: 80, detail: 160 })
watch(filteredLogs, async () => { if (autoScroll.value && !paused.value) { await nextTick(); if (logBody.value) logBody.value.scrollTop = logBody.value.scrollHeight } })
function filteredText() { return filteredLogs.value.map(log => [fmtTimestamp(log.timestamp), log.direction.toUpperCase(), props.fcFormatter(log.function_code), log.detail].join('\t')).join('\n') }
async function copyLogs() { try { await navigator.clipboard.writeText(filteredText()) } catch (e) { error.value = String(e) } }
async function exportFiltered() {
  const quote = (text: string) => '"' + text.replaceAll('"', '""') + '"'
  const csv = '\uFEFF' + ['timestamp,direction,function_code,detail', ...filteredLogs.value.map(log => [log.timestamp, log.direction, log.function_code, log.detail].map(quote).join(','))].join('\r\n')
  try {
    if (props.source.saveFile) await props.source.saveFile(csv, `${props.exportPrefix}_filtered.csv`)
    else {
      const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv' })); const a = document.createElement('a'); a.href = url; a.download = `${props.exportPrefix}_filtered.csv`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
    }
  } catch (e) { error.value = String(e) }
}
watch(paused, value => { if (!value) void scheduleReload() })
const selectedConnId = ref('')
let unlisten: UnlistenFn | null = null
let reloadInFlight = false
let reloadAgain = false
let disposed = false

function pickInitialConnection() {
  const list = props.connections
  if (!list.length) { selectedConnId.value = ''; reset(); return }
  if (props.pinnedConnectionId && list.some(c => c.id === props.pinnedConnectionId)) {
    selectedConnId.value = props.pinnedConnectionId
    return
  }
  if (list.length > 0 && !list.some(c => c.id === selectedConnId.value)) {
    selectedConnId.value = list[0].id
  }
}

/** Coalesce append events: drop additional events while a fetch is in flight.
 *  Each fetch returns the full buffer, so a single follow-up always catches
 *  up — no need to chain a second fetch. */
async function scheduleReload() {
  if (reloadInFlight) { reloadAgain = true; return }
  if (disposed || paused.value || !props.expanded || !selectedConnId.value) return
  reloadInFlight = true
  try { await loadLogs(selectedConnId.value) } finally {
    reloadInFlight = false
    if (reloadAgain && !disposed) { reloadAgain = false; void scheduleReload() }
  }
}

async function doLoadLogs() {
  if (!selectedConnId.value) return
  await loadLogs(selectedConnId.value)
}

async function doClearLogs() {
  if (!selectedConnId.value) return
  await clearLogs(selectedConnId.value)
}

async function doExportLogs() {
  if (!selectedConnId.value) return
  await exportLogsCsv(selectedConnId.value, props.exportPrefix)
}

function fmtTimestamp(ts: string): string {
  if (props.timestampFormatter) return props.timestampFormatter(ts)
  const date = new Date(ts)
  if (isNaN(date.getTime())) return ts
  return date.toLocaleTimeString(locale.value, {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

async function startListening() {
  if (unlisten) return
  const removeListener = await listen<LogAppendedEvent>('log-appended', (event) => {
    if (event.payload.connection_id === selectedConnId.value) {
      scheduleReload()
    }
  })
  if (disposed) removeListener()
  else unlisten = removeListener
}

function stopListening() {
  if (unlisten) {
    unlisten()
    unlisten = null
  }
}

watch(() => props.connections, pickInitialConnection, { deep: false })

watch(() => props.pinnedConnectionId, (id) => {
  if (id && props.connections.some(c => c.id === id)) {
    selectedConnId.value = id
  }
})

watch(selectedConnId, () => { reset(); void doLoadLogs() })

watch(() => props.expanded, async (expanded) => {
  if (expanded) {
    pickInitialConnection()
    await doLoadLogs()
  }
})

onMounted(async () => {
  await startListening()
  pickInitialConnection()
  await doLoadLogs()
})

onUnmounted(() => { disposed = true; stopListening(); reset() })
</script>

<template>
  <div :class="['log-panel', { expanded }]">
    <div class="log-header" @click="emit('toggle')">
      <span class="log-toggle">{{ expanded ? '▼' : '▲' }}</span>
      <span class="log-title">{{ t('log.title') }}</span>
      <span v-if="!expanded && logs.length > 0" class="log-count">{{ logs.length }}</span>
      <div class="log-controls" @click.stop>
        <select v-model="selectedConnId" class="conn-select">
          <option v-for="conn in connections" :key="conn.id" :value="conn.id">{{ conn.label }}</option>
        </select>
        <button class="log-btn" @click="doLoadLogs">{{ t('common.refresh') }}</button>
        <button class="log-btn" @click="doClearLogs">{{ t('common.clear') }}</button>
        <button class="log-btn" @click="doExportLogs">{{ t('common.export') }}</button>
      </div>
    </div>

    <div v-if="expanded" class="log-filters">
      <button class="log-btn" :aria-pressed="paused" @click="paused = !paused">{{ paused ? t('parity.resume') : t('parity.pause') }}</button>
      <button class="log-btn" :aria-pressed="autoScroll" @click="autoScroll = !autoScroll">{{ t('parity.autoScroll') }}</button>
      <button class="log-btn" :disabled="!filteredLogs.length" @click="copyLogs">{{ t('parity.copyLogs') }}</button>
      <button class="log-btn" :disabled="!filteredLogs.length" @click="exportFiltered">{{ t('parity.exportFiltered') }}</button>
      <input v-model="searchQuery" type="text" class="filter-input" :placeholder="t('log.searchPlaceholder')" />
      <select v-model="directionFilter" class="filter-select">
        <option value="all">{{ t('common.all') }}</option>
        <option value="rx">RX</option>
        <option value="tx">TX</option>
      </select>
      <select v-model="fcFilter" class="filter-select">
        <option value="all">{{ t('common.allFc') }}</option>
        <option v-for="fc in availableFcs" :key="fc" :value="fc">{{ fcFormatter(fc) }}</option>
      </select>
      <span v-if="filterSummary" class="filter-summary">{{ filterSummary }}</span>
      <span v-if="error" class="filter-error" :title="error">!</span>
    </div>

    <div v-if="expanded" ref="logBody" class="log-body">
      <div v-if="isLoading && !logs.length" class="log-empty">{{ t('common.loading') }}</div>
      <div v-else-if="connections.length === 0" class="log-empty">{{ t('tree.noConnection') }}</div>
      <div v-else-if="filteredLogs.length === 0" class="log-empty">{{ t('log.noLogs') }}</div>
      <table v-else class="log-table" :style="{ minWidth: Object.values(widths).reduce((a, b) => a + b, 0) + 'px' }">
        <colgroup><col v-for="(width, key) in widths" :key="key" :style="{ width: width + 'px' }" /></colgroup>
        <thead>
          <tr>
            <th>{{ t('log.timestamp') }}<span class="log-resizer" role="separator" tabindex="0" :aria-label="t('log.timestamp')" @pointerdown="startResize('time', $event)" @keydown="resizeWithKeyboard('time', $event)" /></th>
            <th>{{ t('log.direction') }}<span class="log-resizer" role="separator" tabindex="0" :aria-label="t('log.direction')" @pointerdown="startResize('direction', $event)" @keydown="resizeWithKeyboard('direction', $event)" /></th>
            <th>{{ t('table.function') }}<span class="log-resizer" role="separator" tabindex="0" :aria-label="t('table.function')" @pointerdown="startResize('function', $event)" @keydown="resizeWithKeyboard('function', $event)" /></th>
            <th>{{ t('log.detail') }}<span class="log-resizer" role="separator" tabindex="0" :aria-label="t('log.detail')" @pointerdown="startResize('detail', $event)" @keydown="resizeWithKeyboard('detail', $event)" /></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(log, idx) in filteredLogs" :key="idx">
            <td class="col-time">{{ fmtTimestamp(log.timestamp) }}</td>
            <td :class="['col-dir', (log.direction || '').toLowerCase()]">{{ (log.direction || '').toUpperCase() }}</td>
            <td class="col-func">{{ fcFormatter(log.function_code) }}</td>
            <td class="col-detail">{{ log.detail }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.log-resizer { position: absolute; top: 0; right: 0; bottom: 0; width: 6px; cursor: col-resize; }
.log-resizer:hover, .log-resizer:focus-visible { background: var(--c-blue); }
.log-btn[aria-pressed=true] { color: var(--c-blue); border-color: var(--c-blue); }

.log-panel { display: flex; flex-direction: column; height: 100%; transition: height 0.2s ease; }
.log-panel:not(.expanded) { height: 32px; }
.log-header { display: flex; align-items: center; gap: 8px; height: 32px; padding: 0 8px; cursor: pointer; flex-shrink: 0; background: var(--c-base); }
.log-toggle { font-size: 10px; color: var(--c-overlay0); width: 16px; text-align: center; }
.log-title { font-size: 12px; color: var(--c-overlay0); }
.log-count { font-size: 10px; background: var(--c-blue); color: var(--c-base); padding: 0 6px; border-radius: 8px; font-weight: 600; }
.log-controls { display: flex; gap: 4px; margin-left: auto; }
.conn-select { padding: 2px 6px; background: var(--c-surface0); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 11px; max-width: 160px; }
.log-btn { padding: 2px 8px; background: transparent; border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); cursor: pointer; font-size: 11px; }
.log-btn:hover { background: var(--c-surface0); }
.log-body { flex: 1; overflow-y: auto; background: var(--c-crust); }
.log-empty { padding: 24px; text-align: center; color: var(--c-overlay0); font-size: 12px; }
.log-table { width: 100%; table-layout: fixed; border-collapse: collapse; font-size: 12px; }
.log-table th, .log-table td { padding: 4px 10px; text-align: left; border-bottom: 1px solid var(--c-base); }
.log-table th { background: var(--c-mantle); color: var(--c-overlay0); font-weight: 500; position: sticky; top: 0; }
.col-time { font-family: monospace; color: var(--c-overlay0); width: 100px; }
.col-dir { font-weight: 600; width: 40px; }
.col-dir.rx { color: var(--c-blue); }
.col-dir.tx { color: var(--c-green); }
.col-func { font-family: monospace; width: 60px; }
.col-detail { font-family: var(--font-mono); overflow-wrap: anywhere; white-space: pre-wrap; }
.log-filters { flex-wrap: wrap; display: flex; gap: 6px; padding: 4px 8px; align-items: center; border-bottom: 1px solid var(--c-surface0); }
.filter-input { flex: 1; padding: 3px 8px; background: var(--c-crust); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 12px; }
.filter-input:focus { outline: none; border-color: var(--c-blue); }
.filter-select { padding: 3px 6px; background: var(--c-crust); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 12px; }
.filter-summary { font-size: 11px; color: var(--c-blue); white-space: nowrap; }
.filter-error { font-size: 12px; color: var(--c-red); font-weight: 700; cursor: help; }
</style>

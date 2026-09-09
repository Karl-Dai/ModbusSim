import { ref } from 'vue'
import type { LogEntry } from '../types/modbus'

export interface LogPanelDataSource {
  fetchLogs: (connectionId: string) => Promise<LogEntry[]>
  clearLogs: (connectionId: string) => Promise<void>
  exportCsv: (connectionId: string) => Promise<string>
  saveFile?: (content: string, defaultPath: string) => Promise<void>
}

/**
 * Shared log panel logic. Caller injects data source so this composable
 * does not depend on any specific Tauri command names.
 */
export function useLogPanel(source: LogPanelDataSource) {
  const logs = ref<LogEntry[]>([])
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  let generation = 0
  function reset() { generation++; logs.value = []; error.value = null; isLoading.value = false }
  async function loadLogs(connectionId: string) {
    if (!connectionId) { reset(); return }
    const epoch = ++generation
    isLoading.value = true
    try {
      const rows = await source.fetchLogs(connectionId)
      if (epoch !== generation) return
      logs.value = rows
      error.value = null
    } catch (e) {
      if (epoch === generation) error.value = String(e)
    }
    if (epoch === generation) isLoading.value = false
  }

  async function clearLogs(connectionId: string) {
    if (!connectionId) return
    const epoch = ++generation
    try {
      await source.clearLogs(connectionId)
      if (epoch !== generation) return
      logs.value = []
      error.value = null
      isLoading.value = false
    } catch (e) {
      error.value = String(e)
    }
  }

  async function exportLogsCsv(connectionId: string, filenamePrefix = 'modbus_log') {
    if (!connectionId) return
    try {
      const csv = await source.exportCsv(connectionId)
      if (source.saveFile) { await source.saveFile(csv, `${filenamePrefix}_${Date.now()}.csv`); return }
      const blob = new Blob([csv], { type: 'text/csv' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${filenamePrefix}_${Date.now()}.csv`
      a.click()
      URL.revokeObjectURL(url)
    } catch (e) {
      error.value = String(e)
    }
  }

  return { logs, isLoading, error, loadLogs, clearLogs, exportLogsCsv, reset }
}

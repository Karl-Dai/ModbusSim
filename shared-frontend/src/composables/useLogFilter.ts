import { ref, computed, type Ref } from 'vue'
import type { LogEntry } from '../types/modbus'

export type DirectionFilter = 'all' | 'rx' | 'tx'
export type FcFilter = 'all' | string

export interface LogFields<T> {
  timestamp: (row: T) => string
  direction: (row: T) => string
  functionCode: (row: T) => string
  detail: (row: T) => string
}
export const defaultLogFields: LogFields<LogEntry> = {
  timestamp: row => row.timestamp, direction: row => row.direction,
  functionCode: row => row.function_code, detail: row => row.detail,
}
export function useLogFilter<T = LogEntry>(logs: Ref<T[]>, fields: LogFields<T> = defaultLogFields as unknown as LogFields<T>) {
  const searchQuery = ref('')
  const directionFilter = ref<DirectionFilter>('all')
  const fcFilter = ref<FcFilter>('all')

  const filteredLogs = computed(() => {
    let result = logs.value
    if (directionFilter.value !== 'all') {
      const dir = directionFilter.value.toUpperCase()
      result = result.filter(log => (fields.direction(log) || '').toUpperCase() === dir)
    }
    if (fcFilter.value !== 'all') {
      result = result.filter(log => {
        const fc = (fields.functionCode(log) || '').toUpperCase()
        return fc === fcFilter.value.toUpperCase() || fc.includes(fcFilter.value.toUpperCase())
      })
    }
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.toLowerCase()
      result = result.filter(log =>
        (fields.detail(log) || '').toLowerCase().includes(q) ||
        (fields.functionCode(log) || '').toLowerCase().includes(q) ||
        (fields.timestamp(log) || '').toLowerCase().includes(q)
      )
    }
    return result
  })

  const availableFcs = computed(() => {
    const fcs = new Set<string>()
    logs.value.forEach(log => { if (fields.functionCode(log)) fcs.add(fields.functionCode(log)) })
    return Array.from(fcs).sort()
  })

  const filterSummary = computed(() => {
    const parts: string[] = []
    if (directionFilter.value !== 'all') parts.push(directionFilter.value.toUpperCase())
    if (fcFilter.value !== 'all') parts.push(fcFilter.value)
    if (searchQuery.value.trim()) parts.push(`"${searchQuery.value}"`)
    return parts.length > 0 ? parts.join(' + ') : ''
  })

  function resetFilters() {
    searchQuery.value = ''
    directionFilter.value = 'all'
    fcFilter.value = 'all'
  }

  return { searchQuery, directionFilter, fcFilter, filteredLogs, availableFcs, filterSummary, resetFilters }
}

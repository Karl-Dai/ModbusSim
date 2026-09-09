import { expect, it, vi } from 'vitest'
import type { LogEntry } from '../src/types/modbus'
import { useLogPanel } from '../src/composables/useLogPanel'
it('ignores a response from the old connection after switching or clearing the workspace', async () => {
  let resolveOld!: (rows: LogEntry[]) => void
  const panel = useLogPanel({ fetchLogs: vi.fn(() => new Promise<LogEntry[]>(resolve => { resolveOld = resolve })), clearLogs: async () => {}, exportCsv: async () => '' })
  const pending = panel.loadLogs('old')
  panel.reset()
  resolveOld([{ timestamp: 'old', direction: 'rx', function_code: 'read_coils', detail: 'must not reappear' }])
  await pending
  expect(panel.logs.value).toEqual([])
  expect(panel.isLoading.value).toBe(false)
})

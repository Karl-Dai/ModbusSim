import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import LogPanelShell from '../src/components/LogPanelShell.vue'
import { useI18n } from '../src/i18n'

type NativeLog = { Timestamp: string; Direction: string; FunctionCode: string; Detail: string }
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

it('renders and filters native logs, subscribes through the supplied host and unsubscribes on unmount', async () => {
  useI18n().setLocale('zh-CN')
  let appended!: (id: string) => void
  const unsubscribe = vi.fn()
  const rows: NativeLog[] = [
    { Timestamp: '2026-10-05T13:35:00Z', Direction: 'rx', FunctionCode: 'FC06', Detail: 'W 0 = 0x002a' },
    { Timestamp: '2026-10-05T13:35:00Z', Direction: 'tx', FunctionCode: 'FC06', Detail: 'OK' },
  ]
  const fetchLogs = vi.fn().mockResolvedValue(rows)
  wrapper = mount(LogPanelShell, { attachTo: document.body, props: {
    expanded: true, connections: [{ id: 'slave_1', label: '127.0.0.1:5020' }],
    source: { fetchLogs, clearLogs: vi.fn(), exportCsv: vi.fn(), subscribe: (callback: (id: string) => void) => { appended = callback; return unsubscribe } },
    fields: { timestamp: (r: NativeLog) => r.Timestamp, direction: (r: NativeLog) => r.Direction, functionCode: (r: NativeLog) => r.FunctionCode, detail: (r: NativeLog) => r.Detail },
  } })
  await flushPromises()
  expect(wrapper.text()).toContain('W 0 = 0x002a')
  expect(wrapper.findAll('th').map(node => node.text())).toEqual(['时间', '方向', '功能码', '详情'])
  await wrapper.get('.filter-input').setValue('002a')
  expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  const before = fetchLogs.mock.calls.length
  appended('other')
  await flushPromises()
  expect(fetchLogs).toHaveBeenCalledTimes(before)
  appended('slave_1')
  await flushPromises()
  expect(fetchLogs).toHaveBeenCalledTimes(before + 1)
  wrapper.unmount(); wrapper = undefined
  expect(unsubscribe).toHaveBeenCalledTimes(1)
})

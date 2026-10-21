import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import DataTable from '../../master-frontend/src/components/DataTable.vue'
import { useI18n } from '../src/i18n'

vi.mock('@tauri-apps/api/core', () => ({ invoke: vi.fn() }))
vi.mock('@tauri-apps/api/event', () => ({ listen: vi.fn(async () => () => {}) }))
vi.mock('@tanstack/vue-virtual', async () => {
  const { computed } = await import('vue')
  return { useVirtualizer: (options: { value: { count: number } }) => computed(() => ({
    getTotalSize: () => options.value.count * 32,
    getVirtualItems: () => Array.from({ length: options.value.count }, (_, index) => ({ index, start: index * 32 })),
  })) }
})

const result = (value = 0) => ({ values: [{ address: 0, raw_value: value, display_value: String(value), is_bool: true }], timestamp: 'now' })
let wrapper: ReturnType<typeof mount>
function table() {
  const dataRefreshKey = ref(0)
  const selectedScanGroup = ref({ id: 'group-2', name: 'Coils', function: 'read_coils', slave_id: 2, is_polling: false })
  wrapper = mount(DataTable, { global: { provide: {
    selectedConnectionId: ref('conn'), selectedScanGroup, addrMode: ref('hex'), dataRefreshKey,
  } } })
  return { dataRefreshKey, selectedScanGroup }
}
beforeEach(() => { vi.useFakeTimers(); useI18n().setLocale('en-US'); vi.mocked(invoke).mockResolvedValue(result()) })
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.resetAllMocks() })

it('reads the stopped scan group after a write and updates the selected value', async () => {
  const state = table()
  await flushPromises()
  await wrapper.get('.virtual-row').trigger('click')
  vi.mocked(invoke).mockResolvedValueOnce(result(1))
  state.dataRefreshKey.value++
  await flushPromises()
  expect(invoke).toHaveBeenLastCalledWith('get_poll_data', { connectionId: 'conn', groupId: 'group-2', refresh: true })
  expect(wrapper.get('.bool-value').text()).toBe('ON')
  expect(wrapper.emitted('register-select')?.at(-1)).toEqual([result(1).values])
})

it('keeps a readback error visible when the timer fetches old cached data', async () => {
  const state = table()
  await flushPromises()
  vi.mocked(invoke).mockRejectedValueOnce('ServerDeviceFailure')
  state.dataRefreshKey.value++
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('ServerDeviceFailure')
  await vi.advanceTimersByTimeAsync(2000)
  expect(wrapper.get('[role="alert"]').text()).toContain('ServerDeviceFailure')
  state.dataRefreshKey.value++
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
})

it('discards a late readback after switching scan groups', async () => {
  const state = table()
  await flushPromises()
  let finish!: (value: ReturnType<typeof result>) => void
  vi.mocked(invoke).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  state.dataRefreshKey.value++
  await flushPromises()
  state.selectedScanGroup.value = { ...state.selectedScanGroup.value, id: 'other' }
  await flushPromises()
  finish(result(1))
  await flushPromises()
  expect(wrapper.get('.bool-value').text()).toBe('OFF')
})

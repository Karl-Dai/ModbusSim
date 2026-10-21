import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
const { getData, refreshGroup } = vi.hoisted(() => ({ getData: vi.fn(), refreshGroup: vi.fn() }))
import DataTable from '../../master-frontend/src/components/DataTable.vue'
import { useI18n } from '../src/i18n'

vi.mock('../../master-frontend/wailsjs/go/main/App', () => ({ GetPollData: getData, RefreshScanGroup: refreshGroup }))
vi.mock('../../master-frontend/wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }))
vi.mock('@tanstack/vue-virtual', async () => {
  const { computed } = await import('vue')
  return { useVirtualizer: (options: { value: { count: number } }) => computed(() => ({
    getTotalSize: () => options.value.count * 32,
    getVirtualItems: () => Array.from({ length: options.value.count }, (_, index) => ({ index, start: index * 32 })),
  })) }
})

const result = (value = 0) => ({ Values: [{ Address: 0, RawValue: value, DisplayValue: String(value), IsBool: true }], Timestamp: 'now' })
let wrapper: ReturnType<typeof mount>
function table() {
  const dataRefreshKey = ref(0)
  const selectedScanGroup = ref({ ID: 'group-2', Name: 'Coils', Function: 'read_coils', SlaveID: 2, IsPolling: false })
  wrapper = mount(DataTable, { global: { provide: {
    selectedConnectionId: ref('conn'), selectedScanGroup, addrMode: ref('hex'), dataRefreshKey,
  } } })
  return { dataRefreshKey, selectedScanGroup }
}
beforeEach(() => { vi.useFakeTimers(); useI18n().setLocale('en-US'); getData.mockResolvedValue(result()); refreshGroup.mockResolvedValue(result()) })
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.resetAllMocks() })

it('reads the stopped scan group after a write and updates the selected value', async () => {
  const state = table()
  await flushPromises()
  await wrapper.get('.virtual-row').trigger('click')
  refreshGroup.mockResolvedValueOnce(result(1))
  state.dataRefreshKey.value++
  await flushPromises()
  expect(refreshGroup).toHaveBeenLastCalledWith('conn', 'group-2')
  expect(wrapper.get('.bool-value').text()).toBe('ON')
  expect(wrapper.emitted('register-select')?.at(-1)).toEqual([result(1).Values])
})

it('keeps a readback error visible when the timer fetches old cached data', async () => {
  const state = table()
  await flushPromises()
  refreshGroup.mockRejectedValueOnce('ServerDeviceFailure')
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
  refreshGroup.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  state.dataRefreshKey.value++
  await flushPromises()
  state.selectedScanGroup.value = { ...state.selectedScanGroup.value, ID: 'other' }
  await flushPromises()
  finish(result(1))
  await flushPromises()
  expect(wrapper.get('.bool-value').text()).toBe('OFF')
})

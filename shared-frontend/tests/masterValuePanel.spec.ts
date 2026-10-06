import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n } from '../src/i18n'
import ValuePanel from '../../master-frontend/src/components/ValuePanel.vue'
import type { RegisterValueDto, ScanGroupInfo } from '../../master-frontend/src/types'

vi.mock('@tauri-apps/api/core', () => ({ invoke: vi.fn() }))

function register(address: number, is_bool = false): RegisterValueDto {
  return { address, raw_value: 0, display_value: '0', is_bool }
}
function scanGroup(overrides: Partial<ScanGroupInfo> = {}): ScanGroupInfo {
  return { id: 'group-2', name: 'Slave 2', function: 'read_holding_registers', start_address: 0,
    quantity: 4, interval_ms: 1000, enabled: true, is_polling: true, slave_id: 2, ...overrides }
}
let wrapper: ReturnType<typeof mount>
function panel(regs: RegisterValueDto[], group = scanGroup()) {
  const selectedRegisters = ref(regs)
  const selectedScanGroup = ref(group)
  const selectedConnectionId = ref('connection-1')
  wrapper = mount(ValuePanel, { global: { provide: { selectedConnectionId, selectedScanGroup, selectedRegisters } } })
  return { selectedRegisters, selectedScanGroup, selectedConnectionId }
}
function row(label: string) {
  return wrapper.findAll('.value-row').find(item => item.find('.value-label').text() === label)!
}
async function edit(label: string, value: string) {
  await row(label).find('.value-data').trigger('click')
  const input = row(label).find('input')
  await input.setValue(value)
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
}
beforeEach(() => { useI18n().setLocale('en-US'); vi.mocked(invoke).mockResolvedValue(undefined) })
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks() })

describe('multi-word selection safety', () => {
  it.each([
    [[0, 2], false], [[0, 1, 3, 4], false], [[0, 0], false],
    [[0, 1], true], [[65535, 65536], false], [[-1, 0], false],
  ])('does not combine invalid addresses/types %s (mixed: %s)', async (addresses, mixed) => {
    panel(addresses.map((address, index) => register(address, mixed && index === 1)))
    expect(wrapper.text()).not.toContain('32-bit')
    expect(wrapper.text()).not.toContain('64-bit')
    // The send boundary also rejects invalid packed writes even when called directly.
    await (wrapper.vm as unknown as { writeRegisters: (writes: { address: number; value: number }[]) => Promise<void> })
      .writeRegisters(addresses.map(address => ({ address, value: 1 })))
    expect(invoke).not.toHaveBeenCalled()
  })

  it('requires enough words, and only shows 64-bit for at least four contiguous words', () => {
    const { selectedRegisters } = panel([register(0)])
    expect(wrapper.text()).not.toContain('32-bit')
    selectedRegisters.value = [register(0), register(1), register(2)]
    return flushPromises().then(() => {
      expect(wrapper.text()).toContain('32-bit')
      expect(wrapper.text()).not.toContain('64-bit')
    })
  })

  it.each([
    [], [{ address: 0, value: 1 }, { address: 2, value: 2 }],
    [{ address: 1, value: 1 }, { address: 0, value: 2 }],
    [{ address: 0, value: 1 }, { address: 0, value: 2 }],
    [{ address: 4, value: 1 }],
  ])('rejects an invalid send payload %j without an RPC', async (...writes) => {
    panel([0, 1, 2, 3].map(address => register(address)))
    await (wrapper.vm as unknown as { writeRegisters: (writes: unknown) => Promise<void> }).writeRegisters(writes)
    expect(invoke).not.toHaveBeenCalled()
  })

  it('cancels an active edit if Ctrl-selection introduces a hole', async () => {
    const { selectedRegisters } = panel([register(0), register(1)])
    await row('Float AB CD').find('.value-data').trigger('click')
    const input = row('Float AB CD').find('input')
    await input.setValue('1')
    selectedRegisters.value = [register(0), register(2)]
    await input.trigger('keydown', { key: 'Enter' })
    expect(invoke).not.toHaveBeenCalled()
  })
})

describe('word order and scan-group write target', () => {
  it.each([
    ['Long AB CD', '16909060', [0x0102, 0x0304]],
    ['Long CD AB', '16909060', [0x0304, 0x0102]],
    ['Long BA DC', '16909060', [0x0201, 0x0403]],
    ['Long DC BA', '16909060', [0x0403, 0x0201]],
    ['Float AB CD', '1', [0x3f80, 0]],
    ['Float CD AB', '1', [0, 0x3f80]],
    ['Float BA DC', '1', [0x803f, 0]],
    ['Float DC BA', '1', [0, 0x803f]],
    ['Double AB CD EF GH', '1', [0x3ff0, 0, 0, 0]],
    ['Double GH EF CD AB', '1', [0, 0, 0, 0x3ff0]],
    ['Double BA DC FE HG', '1', [0xf03f, 0, 0, 0]],
    ['Double HG FE DC BA', '1', [0, 0, 0, 0xf03f]],
  ] as const)('preserves %s for a contiguous selection made in reverse order', async (label, value, values) => {
    panel([3, 2, 1, 0].map(address => register(address)))
    expect(wrapper.find('.write-target').text()).toContain('Slave ID 2')
    await edit(label, value)
    expect(invoke).toHaveBeenCalledExactlyOnceWith('write_multiple_registers', {
      connectionId: 'connection-1', request: { slave_id: 2, address: 0, values: [...values] },
    })
  })

  it.each([2, null])('uses target %s for single-register writes and labels default fallback', async slave_id => {
    panel([register(0)], scanGroup({ slave_id }))
    expect(wrapper.find('.write-target').text()).toContain(slave_id === null ? 'default Slave ID' : 'Slave ID 2')
    await edit('Unsigned', '123')
    expect(invoke).toHaveBeenCalledExactlyOnceWith('write_single_register', {
      connectionId: 'connection-1', request: { slave_id, address: 0, value: 123 },
    })
  })

  it('uses the scan-group target for coil toggles', async () => {
    panel([register(0, true)], scanGroup({ function: 'read_coils' }))
    await wrapper.find('.value-data').trigger('click')
    expect(invoke).toHaveBeenCalledExactlyOnceWith('write_single_coil', {
      connectionId: 'connection-1', request: { slave_id: 2, address: 0, value: true },
    })
  })

  it('keeps an edit open across polling-only value updates', async () => {
    const { selectedRegisters } = panel([register(0), register(1)])
    await row('Long AB CD').find('.value-data').trigger('click')
    const input = row('Long AB CD').find('input')
    await input.setValue('16909060')
    selectedRegisters.value[0].raw_value = 42
    await flushPromises()
    expect(wrapper.find('input').exists()).toBe(true)
    await input.trigger('keydown', { key: 'Enter' })
    expect(invoke).toHaveBeenCalledExactlyOnceWith('write_multiple_registers', {
      connectionId: 'connection-1', request: { slave_id: 2, address: 0, values: [0x0102, 0x0304] },
    })
  })

  it.each(['connection', 'group', 'slave', 'function', 'type'])('cancels an active edit when the %s changes at the same address', async change => {
    const state = panel([register(0), register(1)])
    await row('Long AB CD').find('.value-data').trigger('click')
    const input = row('Long AB CD').find('input')
    await input.setValue('123')
    if (change === 'connection') state.selectedConnectionId.value = 'other'
    if (change === 'group') state.selectedScanGroup.value.id = 'other'
    if (change === 'slave') state.selectedScanGroup.value.slave_id = 3
    if (change === 'function') state.selectedScanGroup.value.function = 'read_input_registers'
    if (change === 'type') state.selectedRegisters.value[1].is_bool = true
    await input.trigger('keydown', { key: 'Enter' })
    expect(invoke).not.toHaveBeenCalled()
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
const { writeMock } = vi.hoisted(() => ({ writeMock: vi.fn() }))
import { useI18n } from '../src/i18n'
import ValuePanel from '../../master-frontend/src/components/ValuePanel.vue'
import type { RegisterValueDto, ScanGroupInfo } from '../../master-frontend/src/types'

vi.mock('../../master-frontend/wailsjs/go/main/App', () => Object.fromEntries(['WriteSingleRegisterSlave', 'WriteSingleCoilSlave', 'WriteMultipleRegistersSlave', 'WriteMultipleCoilsSlave'].map(name => [name, (...args: unknown[]) => writeMock(name, ...args)])))

function register(address: number, is_bool = false): RegisterValueDto {
  return { Address: address, RawValue: 0, DisplayValue: '0', IsBool: is_bool }
}
function scanGroup(overrides: Partial<ScanGroupInfo> = {}): ScanGroupInfo {
  return { ID: 'group-2', Name: 'Slave 2', Function: 'read_holding_registers', StartAddress: 0,
    Quantity: 4, IntervalMS: 1000, Enabled: true, IsPolling: true, SlaveID: 2, ...overrides }
}
let wrapper: ReturnType<typeof mount>
const refreshData = vi.fn()
function panel(regs: RegisterValueDto[], group = scanGroup()) {
  const selectedRegisters = ref(regs)
  const selectedScanGroup = ref(group)
  const selectedConnectionId = ref('connection-1')
  wrapper = mount(ValuePanel, { global: { provide: { selectedConnectionId, selectedScanGroup, selectedRegisters, refreshData } } })
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
beforeEach(() => { useI18n().setLocale('en-US'); vi.mocked(writeMock).mockResolvedValue(undefined) })
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
    await (wrapper.vm as unknown as { writeRegisters: (writes: { Address: number; value: number }[]) => Promise<void> })
      .writeRegisters(addresses.map(address => ({ address, value: 1 })))
    expect(writeMock).not.toHaveBeenCalled()
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
    [], [{ Address: 0, value: 1 }, { Address: 2, value: 2 }],
    [{ Address: 1, value: 1 }, { Address: 0, value: 2 }],
    [{ Address: 0, value: 1 }, { Address: 0, value: 2 }],
    [{ Address: 4, value: 1 }],
  ])('rejects an invalid send payload %j without an RPC', async (...writes) => {
    panel([0, 1, 2, 3].map(address => register(address)))
    await (wrapper.vm as unknown as { writeRegisters: (writes: unknown) => Promise<void> }).writeRegisters(writes)
    expect(writeMock).not.toHaveBeenCalled()
  })

  it('cancels an active edit if Ctrl-selection introduces a hole', async () => {
    const { selectedRegisters } = panel([register(0), register(1)])
    await row('Float AB CD').find('.value-data').trigger('click')
    const input = row('Float AB CD').find('input')
    await input.setValue('1')
    selectedRegisters.value = [register(0), register(2)]
    await input.trigger('keydown', { key: 'Enter' })
    expect(writeMock).not.toHaveBeenCalled()
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
    expect(writeMock).toHaveBeenCalledExactlyOnceWith('WriteMultipleRegistersSlave', 'connection-1', 2, 0, [...values])
  })

  it.each([2, null])('uses target %s for single-register writes and labels default fallback', async slave_id => {
    panel([register(0)], scanGroup({ SlaveID: slave_id }))
    expect(wrapper.find('.write-target').text()).toContain(slave_id === null ? 'default Slave ID' : 'Slave ID 2')
    await edit('Unsigned', '123')
    expect(writeMock).toHaveBeenCalledExactlyOnceWith('WriteSingleRegisterSlave', 'connection-1', slave_id, 0, 123)
  })

  it('uses the scan-group target for coil toggles', async () => {
    panel([register(0, true)], scanGroup({ Function: 'read_coils' }))
    await wrapper.get('button[aria-label="Set 0x0000 to ON"]').trigger('click')
    expect(writeMock).toHaveBeenCalledExactlyOnceWith('WriteSingleCoilSlave', 'connection-1', 2, 0, true)
    await flushPromises()
    expect(refreshData).toHaveBeenCalledOnce()
    expect(wrapper.get('[role="status"]').text()).toBe('Write succeeded')
  })

  it('sends explicit OFF even if the cached value is already OFF', async () => {
    panel([register(0, true)], scanGroup({ Function: 'read_coils' }))
    await wrapper.get('button[aria-label="Set 0x0000 to OFF"]').trigger('click')
    await flushPromises()
    expect(writeMock).toHaveBeenCalledExactlyOnceWith('WriteSingleCoilSlave', 'connection-1', 2, 0, false)
  })

  it('blocks repeated commands while a write is pending', async () => {
    let finish!: () => void
    vi.mocked(writeMock).mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    panel([register(0, true)], scanGroup({ Function: 'read_coils' }))
    const buttons = wrapper.findAll('button')
    await buttons[0]!.trigger('click')
    expect(buttons.every(button => button.element.disabled)).toBe(true)
    expect(wrapper.get('[role="status"]').text()).toBe('Sending…')
    await buttons[1]!.trigger('click')
    expect(writeMock).toHaveBeenCalledTimes(1)
    finish()
    await flushPromises()
    expect(buttons.every(button => !button.element.disabled)).toBe(true)
  })

  it('does not refresh or show an old write result on a different target', async () => {
    let finish!: () => void
    vi.mocked(writeMock).mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const state = panel([register(0, true)], scanGroup({ Function: 'read_coils' }))
    await wrapper.find('button').trigger('click')
    state.selectedScanGroup.value = scanGroup({ ID: 'other', Function: 'read_coils', SlaveID: 3 })
    finish()
    await flushPromises()
    expect(refreshData).not.toHaveBeenCalled()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('shows a write error and allows retry without changing the cached value', async () => {
    vi.mocked(writeMock).mockRejectedValueOnce('Illegal data address')
    panel([register(0, true)], scanGroup({ Function: 'read_coils' }))
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Illegal data address')
    expect(wrapper.find('.value-data').text()).toBe('false (0)')
    expect(refreshData).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').every(button => !button.element.disabled)).toBe(true)
  })

  it('keeps an edit open across polling-only value updates', async () => {
    const { selectedRegisters } = panel([register(0), register(1)])
    await row('Long AB CD').find('.value-data').trigger('click')
    const input = row('Long AB CD').find('input')
    await input.setValue('16909060')
    selectedRegisters.value[0].RawValue = 42
    await flushPromises()
    expect(wrapper.find('input').exists()).toBe(true)
    await input.trigger('keydown', { key: 'Enter' })
    expect(writeMock).toHaveBeenCalledExactlyOnceWith('WriteMultipleRegistersSlave', 'connection-1', 2, 0, [0x0102, 0x0304])
  })

  it.each(['connection', 'group', 'slave', 'function', 'type'])('cancels an active edit when the %s changes at the same address', async change => {
    const state = panel([register(0), register(1)])
    await row('Long AB CD').find('.value-data').trigger('click')
    const input = row('Long AB CD').find('input')
    await input.setValue('123')
    if (change === 'connection') state.selectedConnectionId.value = 'other'
    if (change === 'group') state.selectedScanGroup.value.ID = 'other'
    if (change === 'slave') state.selectedScanGroup.value.SlaveID = 3
    if (change === 'function') state.selectedScanGroup.value.Function = 'read_input_registers'
    if (change === 'type') state.selectedRegisters.value[1].IsBool = true
    await input.trigger('keydown', { key: 'Enter' })
    expect(writeMock).not.toHaveBeenCalled()
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { invoke } from '@tauri-apps/api/core'
import { useI18n } from '../src/i18n'
import WriteDialog from '../../master-frontend/src/components/WriteDialog.vue'

vi.mock('@tauri-apps/api/core', () => ({ invoke: vi.fn() }))

let wrapper: ReturnType<typeof mount>
const writeButton = () => wrapper.get<HTMLButtonElement>('.btn-primary')
const addressInput = () => wrapper.get('input[type="number"]')
const quantityInput = () => wrapper.findAll('input[type="number"]')[1]!
const valuesInput = () => wrapper.get('textarea')

beforeEach(async () => {
  vi.mocked(invoke).mockReset().mockResolvedValue(undefined)
  useI18n().setLocale('zh-CN')
  wrapper = mount(WriteDialog, {
    props: { show: true, connectionId: 'test-connection' },
    global: { stubs: { teleport: true } },
  })
  await wrapper.get('select').setValue('write_multiple_coils')
})
afterEach(() => wrapper.unmount())

describe('FC15 coil editing', () => {
  it('sets individual coil states and submits one contiguous range', async () => {
    await addressInput().setValue('5')
    await quantityInput().setValue('4')
    await wrapper.findAll('.coil-toggle')[0]!.trigger('click')
    await wrapper.findAll('.coil-toggle')[2]!.trigger('click')
    expect(wrapper.findAll('.coil-toggle').map(button => button.text())).toEqual(['5ON', '6OFF', '7ON', '8OFF'])
    expect(valuesInput().element.value).toBe('1, 0, 1, 0')
    await writeButton().trigger('click')
    await flushPromises()
    expect(invoke).toHaveBeenCalledExactlyOnceWith('write_multiple_coils', {
      connectionId: 'test-connection', request: { slave_id: null, address: 5, values: [true, false, true, false] },
    })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('parses pasted values with comma, Chinese comma, spaces and newlines', async () => {
    await valuesInput().setValue('1, false，ON\n0 true OFF')
    expect(quantityInput().element.value).toBe('6')
    expect(wrapper.findAll('.coil-toggle').map(button => button.attributes('aria-pressed')))
      .toEqual(['true', 'false', 'true', 'false', 'true', 'false'])
    await writeButton().trigger('click')
    await flushPromises()
    expect(vi.mocked(invoke).mock.calls[0]![1]).toEqual({
      connectionId: 'test-connection', request: { slave_id: null, address: 0, values: [true, false, true, false, true, false] },
    })
  })

  it('keeps the explicitly selected slave as the FC15 write target', async () => {
    await wrapper.setProps({ slaveId: 7 })
    await valuesInput().setValue('1,0,1')
    await writeButton().trigger('click')
    await flushPromises()
    expect(invoke).toHaveBeenCalledExactlyOnceWith('write_multiple_coils', {
      connectionId: 'test-connection', request: { slave_id: 7, address: 0, values: [true, false, true] },
    })
  })

  it('preserves existing states when extending or shrinking the quantity', async () => {
    await valuesInput().setValue('ON, OFF, 1')
    await quantityInput().setValue('5')
    expect(valuesInput().element.value).toBe('ON, OFF, 1, 0, 0')
    await quantityInput().setValue('2')
    expect(valuesInput().element.value).toBe('ON, OFF')
  })

  it.each(['', '1,,0', '1,', ',1', '1,2,0', '1,tru,0', '1,0.5', '1010'])('blocks invalid input %j without sending a write', async value => {
    await valuesInput().setValue(value)
    expect(writeButton().element.disabled).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).not.toBe('')
    await writeButton().trigger('click')
    expect(invoke).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it.each(['-1', '0.5', '', '65536'])('blocks invalid address %j', async address => {
    await addressInput().setValue(address)
    expect(writeButton().element.disabled).toBe(true)
    expect(invoke).not.toHaveBeenCalled()
  })

  it('accepts the last address but blocks a range that extends past it', async () => {
    await addressInput().setValue('65535')
    expect(writeButton().element.disabled).toBe(false)
    await quantityInput().setValue('2')
    expect(writeButton().element.disabled).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('65535')
  })

  it.each(['0', '1969', '1.5', ''])('blocks invalid quantity %j until corrected', async quantity => {
    await quantityInput().setValue(quantity)
    expect(writeButton().element.disabled).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('1968')
    await writeButton().trigger('click')
    expect(invoke).not.toHaveBeenCalled()
    await quantityInput().setValue('2')
    expect(writeButton().element.disabled).toBe(false)
    expect(wrapper.findAll('.coil-toggle')).toHaveLength(2)
  })

  it('enforces the FC15 quantity limit', async () => {
    await valuesInput().setValue(Array(1968).fill('0').join(','))
    expect(writeButton().element.disabled).toBe(false)
    await valuesInput().setValue(Array(1969).fill('0').join(','))
    expect(writeButton().element.disabled).toBe(true)
    expect(invoke).not.toHaveBeenCalled()
  })

  it('prevents duplicate submissions while the write is pending', async () => {
    let finish!: () => void
    vi.mocked(invoke).mockImplementationOnce(() => new Promise(resolve => { finish = () => resolve(undefined) }))
    await writeButton().trigger('click')
    expect(writeButton().element.disabled).toBe(true)
    expect(valuesInput().element.disabled).toBe(true)
    await writeButton().trigger('click')
    expect(invoke).toHaveBeenCalledTimes(1)
    finish()
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('keeps FC05 and raw FC16 inputs and payloads working', async () => {
    await wrapper.get('select').setValue('write_single_coil')
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper.get('input[type="text"]').setValue('1')
    await writeButton().trigger('click')
    await flushPromises()
    expect(invoke).toHaveBeenLastCalledWith('write_single_coil', {
      connectionId: 'test-connection', request: { slave_id: null, address: 0, value: true },
    })
    await wrapper.get('select').setValue('write_multiple_registers')
    await wrapper.get('input[type="text"]').setValue('10,20')
    await writeButton().trigger('click')
    await flushPromises()
    expect(invoke).toHaveBeenLastCalledWith('write_multiple_registers', {
      connectionId: 'test-connection', request: { slave_id: null, address: 0, values: [10, 20] },
    })
  })
})

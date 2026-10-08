import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { invoke } from '@tauri-apps/api/core'
import { useI18n } from '../src/i18n'
import WriteDialog from '../../master-frontend/src/components/WriteDialog.vue'

vi.mock('@tauri-apps/api/core', () => ({ invoke: vi.fn() }))
let wrapper: ReturnType<typeof mount>
beforeEach(() => { useI18n().setLocale('en-US'); vi.mocked(invoke).mockResolvedValue(undefined) })
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks(); document.body.innerHTML = '' })

for (const slaveId of [2, null]) {
  it.each([
    ['write_single_register', '123', { value: 123 }],
    ['write_single_coil', 'true', { value: true }],
    ['write_multiple_registers', '123,456', { values: [123, 456] }],
    ['write_multiple_coils', 'true,false', { values: [true, false] }],
  ])(`sends %s to ${slaveId ?? 'the connection default'} and displays its target`, async (command, input, values) => {
    wrapper = mount(WriteDialog, {
      attachTo: document.body,
      props: { show: true, connectionId: 'connection-1', slaveId },
    })
    expect(document.querySelector('.write-target')!.textContent).toContain(slaveId === null ? 'default Slave ID' : 'Slave ID 2')
    const select = document.querySelector('select')!
    select.value = command
    select.dispatchEvent(new Event('change', { bubbles: true }))
    await flushPromises()
    const addressInput = document.querySelector<HTMLInputElement>('input[type="number"]')!
    addressInput.value = '10'
    addressInput.dispatchEvent(new Event('input', { bubbles: true }))
    const valueInput = document.querySelector<HTMLInputElement>('input[type="text"]')!
    valueInput.value = input
    valueInput.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.btn-primary')!.click()
    await flushPromises()
    expect(invoke).toHaveBeenCalledExactlyOnceWith(command, {
      connectionId: 'connection-1', request: { slave_id: slaveId, address: 10, ...values },
    })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
}

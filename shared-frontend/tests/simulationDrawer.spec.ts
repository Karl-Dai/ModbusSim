import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { invoke } from '@tauri-apps/api/core'
import SimulationSettingsDrawer from '../../frontend/src/components/SimulationSettingsDrawer.vue'

vi.mock('@tauri-apps/api/core', () => ({ invoke: vi.fn() }))

const points = [0, 1].map(address => ({
  address, register_type: 'coil', data_type: 'bool', endian: 'big', name: '', comment: '',
}))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks() })

for (const operation of ['apply', 'stop'] as const) {
  for (const dismissal of ['button', 'escape', 'backdrop'] as const) {
    it(`allows ${dismissal} during pending ${operation} and keeps the original targets`, async () => {
      let finish!: () => void
      vi.mocked(invoke).mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
      vi.mocked(invoke).mockResolvedValue(undefined)
      wrapper = mount(SimulationSettingsDrawer, {
        attachTo: document.body,
        props: {
          show: true, connectionId: 'original', slaveId: 1, selectedRegs: points,
          activeRows: points.map(point => ({ ...point, mode: 'flip', period_ms: 1000, step: 1, min: 0, max: 1 })),
          currentValueFor: () => 'OFF',
        },
      })
      const button = document.querySelector<HTMLButtonElement>(operation === 'apply' ? '.sim-btn-primary' : '.sim-btn-danger')!
      button.click()
      await flushPromises()
      expect(button.disabled).toBe(true)
      const close = document.querySelector<HTMLButtonElement>('.sim-close')!
      expect(close.disabled).toBe(false)
      if (dismissal === 'button') close.click()
      else if (dismissal === 'escape') document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      else document.querySelector('.sim-drawer-backdrop')!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      expect(wrapper.emitted('close')).toHaveLength(1)
      await wrapper.setProps({ show: false, connectionId: 'other', slaveId: 2, selectedRegs: [] })
      finish()
      await flushPromises()
      const command = operation === 'apply' ? 'set_point_mutation' : 'clear_point_mutation'
      const calls = vi.mocked(invoke).mock.calls.filter(([name]) => name === command)
      expect(calls).toHaveLength(2)
      expect(calls.map(([, args]) => (args as { request: unknown }).request)).toEqual(points.map(point => expect.objectContaining({
        connection_id: 'original', slave_id: 1, register_type: 'coil', address: point.address,
      })))
      expect(wrapper.emitted('changed')).toHaveLength(1)
    })
  }
}

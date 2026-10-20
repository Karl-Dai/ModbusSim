import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as App from '../../frontend/wailsjs/go/main/App'
import SimulationSettingsDrawer from '../../frontend/src/components/SimulationSettingsDrawer.vue'

vi.mock('../../frontend/wailsjs/go/main/App', () => ({ SetPointMutation: vi.fn(), ClearPointMutation: vi.fn(), SetMutationRunning: vi.fn() }))

const points = [0, 1].map(address => ({
  Address: address, RegisterType: 'coil', DataType: 'bool', Endian: 'big', Name: '', Comment: '',
}))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks() })

for (const operation of ['apply', 'stop'] as const) {
  for (const dismissal of ['button', 'escape', 'backdrop'] as const) {
    it(`allows ${dismissal} during pending ${operation} and keeps the original targets`, async () => {
      let finish!: () => void
      vi.mocked(operation === 'apply' ? App.SetPointMutation : App.ClearPointMutation).mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
      vi.mocked(operation === 'apply' ? App.SetPointMutation : App.ClearPointMutation).mockResolvedValue(undefined)
      wrapper = mount(SimulationSettingsDrawer, {
        attachTo: document.body,
        props: {
          show: true, connectionId: 'original', slaveId: 1, selectedRegs: points,
          activeRows: points.map(point => ({ ...point, Mode: 'flip', PeriodMS: 1000, Step: 1, Min: 0, Max: 1 })),
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
      const calls = vi.mocked(operation === 'apply' ? App.SetPointMutation : App.ClearPointMutation).mock.calls
      expect(calls).toHaveLength(2)
      if (operation === 'apply') {
        expect(calls.map(([request]) => request)).toEqual(points.map(point => expect.objectContaining({
          ConnectionID: 'original', SlaveID: 1, RegisterType: 'coil', Address: point.Address,
        })))
      } else {
        expect(calls).toEqual(points.map(point => ['original', 1, 'coil', point.Address]))
      }
      expect(wrapper.emitted('changed')).toHaveLength(1)
    })
  }
}

import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { useI18n } from '../src/i18n'
import NewConnectionDialog from '../../master-frontend/src/components/NewConnectionDialog.vue'
import * as App from '../../master-frontend/wailsjs/go/main/App'

vi.mock('../../master-frontend/wailsjs/go/main/App', () => ({
  GetSettings: vi.fn(),
  ListSerialPorts: vi.fn().mockResolvedValue([]),
  OpenFileDialog: vi.fn(),
  UpdateConnection: vi.fn(),
  CreateConnection: vi.fn(),
}))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks(); document.body.innerHTML = '' })

it('requires an explicit per-connection opt-in and resets it for the next connection', async () => {
  useI18n().setLocale('zh-CN')
  vi.mocked(App.CreateConnection).mockResolvedValue({ id: 'qa' } as never)
  wrapper = mount(NewConnectionDialog, { attachTo: document.body, props: { show: true } })
  const enableTls = () => {
    const label = [...document.querySelectorAll('label')].find(node => node.textContent?.trim() === '启用 TLS')!
    label.querySelector<HTMLInputElement>('input')!.click()
  }
  enableTls()
  await flushPromises()
  let checkbox = document.querySelector<HTMLInputElement>('#tls-compatibility-hint')!.previousElementSibling!.querySelector<HTMLInputElement>('input')!
  expect(checkbox.checked).toBe(false)
  expect(document.querySelector('#tls-compatibility-hint')!.textContent).toContain('不验证服务器身份')
  checkbox.click()
  await flushPromises()
  const create = [...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent === '创建')!
  create.click()
  await flushPromises()
  expect(App.CreateConnection).toHaveBeenCalledWith(expect.objectContaining({
    Transport: expect.objectContaining({
      Type: 'tcp_tls',
      TLS: expect.objectContaining({ Enabled: true, AcceptInvalidCerts: true }),
    }),
  }))
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true })
  enableTls()
  await flushPromises()
  checkbox = document.querySelector<HTMLInputElement>('#tls-compatibility-hint')!.previousElementSibling!.querySelector<HTMLInputElement>('input')!
  expect(checkbox.checked).toBe(false)
})

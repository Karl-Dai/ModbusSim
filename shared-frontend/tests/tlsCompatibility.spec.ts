import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { invoke } from '@tauri-apps/api/core'
import { useI18n } from '../src/i18n'
import NewConnectionDialog from '../../master-frontend/src/components/NewConnectionDialog.vue'

vi.mock('@tauri-apps/api/core', () => ({ invoke: vi.fn() }))
vi.mock('@tauri-apps/plugin-dialog', () => ({ open: vi.fn() }))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks(); document.body.innerHTML = '' })

it('requires an explicit per-connection opt-in and resets it for the next connection', async () => {
  useI18n().setLocale('zh-CN')
  vi.mocked(invoke).mockResolvedValue({ id: 'qa' })
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
  expect(invoke).toHaveBeenCalledWith('create_master_connection', expect.objectContaining({ request: expect.objectContaining({ use_tls: true, accept_invalid_certs: true }) }))
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true })
  enableTls()
  await flushPromises()
  checkbox = document.querySelector<HTMLInputElement>('#tls-compatibility-hint')!.previousElementSibling!.querySelector<HTMLInputElement>('input')!
  expect(checkbox.checked).toBe(false)
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import * as SlaveApp from '../../frontend/wailsjs/go/main/App'
import * as MasterApp from '../../master-frontend/wailsjs/go/main/App'
import { BrowserOpenURL } from '../../frontend/wailsjs/runtime/runtime'
import AppToolbar from '../src/components/AppToolbar.vue'
import MasterToolbar from '../../master-frontend/src/components/Toolbar.vue'
import SlaveToolbar from '../../frontend/src/components/Toolbar.vue'
import { useI18n } from '../src/i18n'
import { showConfirm } from '../src/composables/useDialog'

vi.mock('../../frontend/wailsjs/go/main/App', () => ({ StartConnection: vi.fn().mockResolvedValue(undefined), StopConnection: vi.fn().mockResolvedValue(undefined), OpenFileDialog: vi.fn(), SaveFileDialog: vi.fn(), LoadProject: vi.fn(), SaveProject: vi.fn() }))
vi.mock('../../frontend/wailsjs/runtime/runtime', () => ({ BrowserOpenURL: vi.fn() }))
vi.mock('../../master-frontend/wailsjs/go/main/App', () => ({ Connect: vi.fn().mockResolvedValue(undefined), Disconnect: vi.fn().mockResolvedValue(undefined), DeleteConnection: vi.fn(), StartAllPolling: vi.fn().mockResolvedValue(undefined), StopAllPolling: vi.fn().mockResolvedValue(undefined), OpenFileDialog: vi.fn(), SaveFileDialog: vi.fn(), LoadProject: vi.fn(), SaveProject: vi.fn() }))
vi.mock('../../master-frontend/wailsjs/runtime/runtime', () => ({ BrowserOpenURL: vi.fn() }))

vi.mock('../src/composables/useDialog', async importOriginal => ({
  ...await importOriginal<typeof import('../src/composables/useDialog')>(),
  showAlert: vi.fn().mockResolvedValue(undefined),
  showConfirm: vi.fn().mockResolvedValue(false),
}))

let wrapper: ReturnType<typeof mount> | undefined
const stubs = {
  VersionBadge: true, LangToggle: true, NewConnectionDialog: true, NewSlaveDialog: true,
  ConnectionSettingsDialog: true, NewScanGroupDialog: true, WriteDialog: true, ScanDialog: true,
  ToolsDialog: true, AboutDialog: true,
}
const button = (id: string) => document.querySelector<HTMLButtonElement>(`[data-testid="${id}"]`)!
async function click(id: string) { button(id).click(); await flushPromises() }
async function key(value: string, options: KeyboardEventInit = {}) {
  document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true, ...options }))
  await flushPromises()
}

beforeEach(() => { useI18n().setLocale('zh-CN') })
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('shared menu interaction', () => {
  function mountMenu(action = vi.fn()) {
    wrapper = mount(AppToolbar, { attachTo: document.body, props: {
      title: 'Modbus', actions: [{ id: 'run', label: '运行', action }],
      menus: [
        { id: 'file', label: '文件', items: [
          { id: 'disabled', label: '不可用', disabled: true, action },
          { id: 'run', label: '运行', separatorBefore: true, shortcut: 'Ctrl+S', action },
        ] },
        { id: 'help', label: '帮助', items: [{ id: 'about', label: '关于', action }] },
      ],
    }, global: { stubs } })
    return action
  }

  it('supports roving focus, cross-menu arrows, Home/End and Escape', async () => {
    mountMenu()
    button('menu-file').focus()
    await key('ArrowDown')
    expect(document.activeElement).toBe(button('disabled'))
    await key('End')
    expect(document.activeElement).toBe(button('run'))
    await key('Home')
    expect(document.activeElement).toBe(button('disabled'))
    await key('ArrowRight')
    expect(document.activeElement).toBe(button('about'))
    expect(button('menu-file').getAttribute('aria-expanded')).toBe('false')
    expect(button('menu-help').getAttribute('tabindex')).toBe('0')
    await key('Escape')
    expect(document.activeElement).toBe(button('menu-help'))
    expect(button('menu-help').getAttribute('aria-expanded')).toBe('false')
    await key('ArrowLeft')
    await key('ArrowUp')
    expect(document.activeElement).toBe(button('run'))
  })

  it('keeps disabled actions discoverable without executing and restores focus after selection', async () => {
    const action = mountMenu()
    await click('menu-file')
    await click('disabled')
    expect(action).not.toHaveBeenCalled()
    expect(button('menu-file').getAttribute('aria-expanded')).toBe('true')
    await click('run')
    expect(action).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(button('menu-file'))
    expect(button('menu-file').getAttribute('aria-expanded')).toBe('false')
    await click('quick-run')
    expect(action).toHaveBeenCalledTimes(2)
  })

  it('switches on hover only when open, closes outside, and blocks actions while busy', async () => {
    const action = mountMenu()
    await wrapper!.get('[data-testid="menu-help"]').trigger('pointerenter')
    expect(button('menu-help').getAttribute('aria-expanded')).toBe('false')
    await click('menu-file')
    await wrapper!.get('[data-testid="menu-help"]').trigger('pointerenter')
    await flushPromises()
    expect(button('menu-help').getAttribute('aria-expanded')).toBe('true')
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(button('menu-help').getAttribute('aria-expanded')).toBe('false')
    await click('menu-file')
    await wrapper!.setProps({ busy: true })
    expect(button('menu-file').getAttribute('aria-expanded')).toBe('false')
    await click('quick-run')
    await click('run')
    expect(action).not.toHaveBeenCalled()
  })
})

function mountStation(station: 'master' | 'slave') {
  const connectionId = ref<string | null>(null)
  const state = ref(station === 'master' ? 'Disconnected' : 'Stopped')
  const slaveId = ref<number | null>(null)
  const checkUpdate = vi.fn().mockResolvedValue(null)
  const registerActions = { importCsv: vi.fn(), exportCsv: vi.fn(), template: vi.fn(), simulation: vi.fn() }
  wrapper = mount(station === 'master' ? MasterToolbar : SlaveToolbar, {
    attachTo: document.body,
    global: { stubs, provide: {
      selectedConnectionId: connectionId, selectedConnectionState: state, selectedSlaveId: slaveId,
      refreshTree: vi.fn(), resetWorkspaceView: vi.fn(), registerActions, checkUpdate,
    } },
  })
  return { connectionId, state, slaveId, checkUpdate, registerActions }
}

describe('station menus', () => {
  it('slave opens Releases without using the Tauri updater', async () => {
    mountStation('slave')
    await click('menu-help')
    await click('releases')
    expect(BrowserOpenURL).toHaveBeenCalledWith('https://github.com/Karl-Dai/ModbusSim/releases')
  })
  it('preserves master connection, reconnect and polling availability', async () => {
    const { connectionId, state } = mountStation('master')
    await click('menu-connection')
    expect(button('connect').getAttribute('aria-disabled')).toBe('true')
    expect(button('quick-connect').disabled).toBe(true)
    connectionId.value = 'master-1'
    await flushPromises()
    expect(button('connect').getAttribute('aria-disabled')).toBe('false')
    expect(button('connection-settings').getAttribute('aria-disabled')).toBe('false')
    state.value = 'Reconnecting'
    await flushPromises()
    expect(button('quick-connect').disabled).toBe(true)
    expect(button('quick-disconnect').textContent).toContain('取消重连')
    expect(button('quick-disconnect').disabled).toBe(false)
    expect(button('connection-settings').getAttribute('aria-disabled')).toBe('true')
    state.value = 'Connected'
    await click('menu-polling')
    expect(button('start-polling').getAttribute('aria-disabled')).toBe('false')
    await click('start-polling')
    expect(MasterApp.StartAllPolling).toHaveBeenCalledWith('master-1')
    await click('quick-stop-polling')
    expect(MasterApp.StopAllPolling).toHaveBeenCalledWith('master-1')
    await click('menu-connection')
    await click('delete-connection')
    expect(showConfirm).toHaveBeenCalled()
    expect(MasterApp.DeleteConnection).not.toHaveBeenCalled()
  })

  it('preserves slave register guards and routes menu and quick actions to the same connection', async () => {
    const { connectionId, state, slaveId, registerActions } = mountStation('slave')
    await click('menu-registers')
    expect(button('import-csv').getAttribute('aria-disabled')).toBe('true')
    connectionId.value = 'slave-1'
    slaveId.value = 1
    await flushPromises()
    expect(button('import-csv').getAttribute('aria-disabled')).toBe('false')
    await click('import-csv')
    expect(registerActions.importCsv).toHaveBeenCalledTimes(1)
    await click('quick-start')
    expect(SlaveApp.StartConnection).toHaveBeenCalledWith('slave-1')
    expect(state.value).toBe('Running')
    await click('menu-registers')
    expect(button('import-csv').getAttribute('aria-disabled')).toBe('true')
    expect(button('export-csv').getAttribute('aria-disabled')).toBe('false')
    await click('menu-connection')
    await click('stop')
    expect(SlaveApp.StopConnection).toHaveBeenCalledWith('slave-1')
    expect(state.value).toBe('Stopped')
  })

  it.each(['master', 'slave'] as const)('%s supports project shortcuts, busy exclusion and modal suppression', async station => {
    mountStation(station)
    let finishOpen!: (path: string) => void
    const app = station === 'master' ? MasterApp : SlaveApp
    const openDialog = app.OpenFileDialog
    const saveDialog = app.SaveFileDialog
    vi.mocked(openDialog).mockImplementationOnce(() => new Promise(resolve => { finishOpen = resolve }))
    button('menu-file').focus()
    await key('o', { ctrlKey: true })
    await key('o', { ctrlKey: true })
    expect(openDialog).toHaveBeenCalledTimes(1)
    finishOpen('/projects/菜单重构.modbusproj')
    await flushPromises()
    expect(app.LoadProject).toHaveBeenCalledWith('/projects/菜单重构.modbusproj')
    expect(wrapper!.find('.toolbar-project-name').text()).toBe('菜单重构.modbusproj')
    button('menu-file').focus()
    await key('s', { metaKey: true })
    expect(app.SaveProject).toHaveBeenCalledWith('/projects/菜单重构.modbusproj')
    vi.mocked(saveDialog).mockResolvedValueOnce('/projects/copy.modbusproj')
    await key('s', { ctrlKey: true, shiftKey: true })
    expect(app.SaveProject).toHaveBeenCalledWith('/projects/copy.modbusproj')
    const modal = document.createElement('div')
    modal.setAttribute('aria-modal', 'true')
    document.body.append(modal)
    await key('o', { ctrlKey: true })
    expect(openDialog).toHaveBeenCalledTimes(1)
  })

})

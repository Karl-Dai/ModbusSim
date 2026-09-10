<script setup lang="ts">
import { AppToolbar, useProjectShortcuts, showPrompt, type ToolbarAction, type ToolbarMenuDefinition } from 'shared-frontend'
import ConnectionSettingsDialog from './ConnectionSettingsDialog.vue'
import ToolsDialog from './ToolsDialog.vue'
import AboutDialog from './AboutDialog.vue'
import { computed, ref, inject, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { save, open } from '@tauri-apps/plugin-dialog'
import {
  useI18n,
  useUpdateProgress,
  localizeUpdateError,
  showAlert,
  showConfirm,
} from 'shared-frontend'
import NewConnectionDialog from './NewConnectionDialog.vue'
import NewSlaveDialog from './NewSlaveDialog.vue'

const { t } = useI18n()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedConnectionState = inject<Ref<string>>('selectedConnectionState')!
const selectedSlaveId = inject<Ref<number | null>>('selectedSlaveId')!
const refreshTree = inject<() => void>('refreshTree')!

const resetWorkspaceView = inject<() => void>('resetWorkspaceView')!
const registerActions = inject<{ importCsv: () => void; exportCsv: () => void; template: () => void; simulation: () => void }>('registerActions')!
const busy = ref(false)
const statusText = ref('')
const showTools = ref(false)
const showSettings = ref(false)
const showAbout = ref(false)
async function run(action: () => unknown) {
  if (busy.value) return
  busy.value = true
  try { await action() } catch (error) { await showAlert(String(error)) } finally { busy.value = false }
}
async function changeAll(action: 'start' | 'stop') {
  const conns = await invoke<Array<{ id: string; state: string }>>('list_slave_connections')
  const targets = conns.filter(c => c.state !== (action === 'start' ? 'Running' : 'Stopped'))
  const failures: string[] = []
  for (const [index, conn] of targets.entries()) {
    statusText.value = `${index + 1} / ${targets.length}`
    try { await invoke(`${action}_slave_connection`, { id: conn.id }) }
    catch (error) { failures.push(`${conn.id}: ${String(error)}`) }
  }
  refreshTree()
  statusText.value = t('parity.bulkResult', { n: targets.length - failures.length, failed: failures.length })
  if (failures.length) await showAlert(failures.join('\n'))
}
async function openProjectByPath() {
  const path = await showPrompt(t('parity.enterProjectPath'), currentProjectPath.value ?? '')
  if (!path?.trim()) return
  try {
    await invoke('load_project_file', { path: path.trim() })
    currentProjectPath.value = path.trim()
    resetWorkspaceView()
  } catch (error) { await showAlert(String(error)) }
}
const shortcuts = useProjectShortcuts({
  open: () => run(openProject),
  save: () => run(saveProject),
  saveAs: () => run(saveProjectAs),
})
const currentProjectPath = ref<string | null>(null)
const showNewConn = ref(false)
const showNewSlave = ref(false)

type UpdateMeta = { version: string; notes: string; pub_date?: string | null }
const checkUpdate = inject<(force?: boolean) => Promise<UpdateMeta | null>>('checkUpdate')!
const updateChecking = ref(false)
const updateProgress = useUpdateProgress(t)
const updateBusy = computed(() => updateChecking.value || updateProgress.active.value)
const updateButtonLabel = computed(() =>
  updateProgress.active.value
    ? updateProgress.label.value
    : updateChecking.value
      ? t('toolbar.checkingUpdate')
      : t('toolbar.checkUpdate'),
)

async function manualCheckUpdate() {
  if (updateBusy.value) return
  updateChecking.value = true
  let message: string | null = null
  try {
    const meta = await checkUpdate(true)
    if (!meta) message = t('toolbar.alreadyLatest')
  } catch (e) {
    message = `${t('toolbar.updateCheckFailed')}: ${localizeUpdateError(e, t)}`
  } finally {
    updateChecking.value = false
  }
  if (message) await showAlert(message)
}

async function openProject() {
  try {
    const path = await open({
      filters: [{ name: 'Modbus Project', extensions: ['modbusproj'] }],
    })
    if (!path) return
    await invoke('load_project_file', { path })
    currentProjectPath.value = path as string
    selectedConnectionId.value = null
    selectedSlaveId.value = null
    selectedConnectionState.value = 'Stopped'
    resetWorkspaceView()
  } catch (e) { await showAlert(String(e)) }
}

async function saveProject() {
  if (!currentProjectPath.value) return saveProjectAs()
  try {
    await invoke('save_project_file', { path: currentProjectPath.value })
  } catch (e) { await showAlert(String(e)) }
}

async function saveProjectAs() {
  try {
    const path = await save({
      filters: [{ name: 'Modbus Project', extensions: ['modbusproj'] }],
      defaultPath: 'untitled.modbusproj',
    })
    if (!path) return
    await invoke('save_project_file', { path })
    currentProjectPath.value = path
  } catch (e) { await showAlert(String(e)) }
}

async function startConnection() {
  if (!selectedConnectionId.value) return
  try {
    await invoke('start_slave_connection', { id: selectedConnectionId.value })
    selectedConnectionState.value = 'Running'
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function stopConnection() {
  if (!selectedConnectionId.value) return
  try {
    await invoke('stop_slave_connection', { id: selectedConnectionId.value })
    selectedConnectionState.value = 'Stopped'
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function closeConnection() {
  if (!selectedConnectionId.value) return
  if (!(await showConfirm(t('errors.confirmDeleteConnection')))) return
  try {
    await invoke('delete_slave_connection', { id: selectedConnectionId.value })
    selectedConnectionId.value = null
    selectedConnectionState.value = 'Stopped'
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}


const actions = computed(() => ({
  open: { id: 'open', label: t('toolbar.openProjectTitle'), shortcut: shortcuts.open, action: () => run(openProject) },
  openPath: { id: 'open-path', label: t('parity.openPath'), action: () => run(openProjectByPath) },
  save: { id: 'save', label: t('toolbar.saveProjectTitle'), icon: 'save', shortcut: shortcuts.save, separatorBefore: true, action: () => run(saveProject) },
  saveAs: { id: 'save-as', label: t('toolbar.saveAsTitle'), shortcut: shortcuts.saveAs, action: () => run(saveProjectAs) },
  newConnection: { id: 'new-connection', label: t('toolbar.newConnection'), icon: 'add', action: () => { showNewConn.value = true } },
  newSlave: { id: 'new-slave', label: t('toolbar.newSlave'), icon: 'device', disabled: !selectedConnectionId.value, action: () => { showNewSlave.value = true } },
  start: { id: 'start', label: t('toolbar.start'), icon: 'play', tone: 'start', separatorBefore: true, disabled: !selectedConnectionId.value || selectedConnectionState.value === 'Running', action: () => run(startConnection) },
  stop: { id: 'stop', label: t('toolbar.stop'), icon: 'stop', tone: 'stop', disabled: !selectedConnectionId.value || selectedConnectionState.value === 'Stopped', action: () => run(stopConnection) },
  startAll: { id: 'start-all', label: t('toolbar.startAllConnections'), separatorBefore: true, action: () => run(() => changeAll('start')) },
  stopAll: { id: 'stop-all', label: t('toolbar.stopAllConnections'), action: () => run(() => changeAll('stop')) },
  settings: { id: 'connection-settings', label: t('parity.connectionSettings'), separatorBefore: true, disabled: !selectedConnectionId.value, action: () => { showSettings.value = true } },
  remove: { id: 'close-connection', label: t('toolbar.deleteConnection'), tone: 'danger', separatorBefore: true, disabled: !selectedConnectionId.value, action: () => run(closeConnection) },
  simulation: { id: 'simulation', label: t('simulationSettings.open'), disabled: selectedSlaveId.value === null, separatorBefore: true, action: registerActions.simulation },
}) satisfies Record<string, ToolbarAction>)
const menus = computed<ToolbarMenuDefinition[]>(() => [
  { id: 'file', label: t('toolbar.menuFile'), items: [actions.value.open, actions.value.openPath, actions.value.save, actions.value.saveAs] },
  { id: 'connection', label: t('toolbar.menuConnection'), items: [
    actions.value.newConnection, actions.value.newSlave, actions.value.settings,
    actions.value.start, actions.value.stop, actions.value.startAll, actions.value.stopAll, actions.value.remove,
  ] },
  { id: 'registers', label: t('parity.menuRegisters'), items: [
    { id: 'import-csv', label: t('parity.importCsv'), disabled: selectedSlaveId.value === null || selectedConnectionState.value !== 'Stopped', action: registerActions.importCsv },
    { id: 'export-csv', label: t('parity.exportCsv'), disabled: selectedSlaveId.value === null, action: registerActions.exportCsv },
    { id: 'csv-template', label: t('parity.csvTemplate'), action: registerActions.template },
    actions.value.simulation,
  ] },
  { id: 'tools', label: t('common.tools'), items: [
    { id: 'tools-dialog', label: t('parity.toolsTitle'), action: () => { showTools.value = true } },
  ] },
  { id: 'help', label: t('parity.menuHelp'), items: [
    { id: 'update', label: updateButtonLabel.value, disabled: updateBusy.value, busy: updateBusy.value, action: manualCheckUpdate },
    { id: 'about', label: t('about.title'), action: () => { showAbout.value = true } },
  ] },
])
const quickActions = computed<ToolbarAction[]>(() => [
  actions.value.newConnection, actions.value.newSlave, actions.value.save, actions.value.start, actions.value.stop,
])
const toolbarStatus = computed(() => updateBusy.value ? updateButtonLabel.value : statusText.value)
</script>


<template>
  <AppToolbar :title="t('toolbar.appTitleSlave')" :menus="menus" :actions="quickActions"
    :busy="busy" :project-path="currentProjectPath" :status="toolbarStatus" />
  <NewConnectionDialog :show="showNewConn" @close="showNewConn = false" @created="refreshTree" />
  <NewSlaveDialog :show="showNewSlave" :connection-id="selectedConnectionId" @close="showNewSlave = false" @created="refreshTree" />
  <ConnectionSettingsDialog v-if="showSettings && selectedConnectionId" :connection-id="selectedConnectionId" @saved="refreshTree" @close="showSettings = false" />
  <ToolsDialog v-if="showTools" @close="showTools = false" />
  <AboutDialog v-if="showAbout" @close="showAbout = false" />
</template>

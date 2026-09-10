<script setup lang="ts">
import { computed, inject, ref, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { save, open } from '@tauri-apps/plugin-dialog'
import {
  useI18n,
  useUpdateProgress,
  localizeUpdateError,
  AppToolbar,
  useProjectShortcuts,
  type ToolbarAction,
  type ToolbarMenuDefinition,
  showAlert,
  showConfirm,
} from 'shared-frontend'
import ScanDialog from './ScanDialog.vue'
import NewConnectionDialog from './NewConnectionDialog.vue'
import NewScanGroupDialog from './NewScanGroupDialog.vue'
import WriteDialog from './WriteDialog.vue'

const { t } = useI18n()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedConnectionState = inject<Ref<string>>('selectedConnectionState')!
const refreshTree = inject<() => void>('refreshTree')!

const busy = ref(false)
async function run(action: () => unknown) {
  if (busy.value) return
  busy.value = true
  try { await action() } catch (error) { await showAlert(String(error)) } finally { busy.value = false }
}
const shortcuts = useProjectShortcuts({
  open: () => run(openProject),
  save: () => run(saveProject),
  saveAs: () => run(saveProjectAs),
})
const currentProjectPath = ref<string | null>(null)
const showNewConn = ref(false)
const editingConnectionId = ref<string | null>(null)
const showNewScanGroup = ref(false)
const showWriteModal = ref(false)
const showScanDialog = ref(false)

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
    const path = await open({ filters: [{ name: 'Modbus Project', extensions: ['modbusproj'] }] })
    if (!path) return
    await invoke('load_project_file', { path })
    currentProjectPath.value = path as string
    refreshTree()
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

async function connectMaster() {
  if (!selectedConnectionId.value) return
  try {
    await invoke('connect_master', { connectionId: selectedConnectionId.value })
    selectedConnectionState.value = 'Connected'
    refreshTree()
    const doScan = await showConfirm(t('errors.connectSuccessAskScan'))
    if (doScan) showScanDialog.value = true
  } catch (e) { await showAlert(String(e)) }
}

async function disconnectMaster() {
  if (!selectedConnectionId.value) return
  try {
    await invoke('disconnect_master', { connectionId: selectedConnectionId.value })
    selectedConnectionState.value = 'Disconnected'
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function deleteMaster() {
  if (!selectedConnectionId.value) return
  if (!await showConfirm(t('errors.confirmDeleteConnection'))) return
  try {
    await invoke('delete_master_connection', { connectionId: selectedConnectionId.value })
    selectedConnectionId.value = null
    selectedConnectionState.value = 'Disconnected'
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function startAllPolling() {
  if (!selectedConnectionId.value) return
  try {
    await invoke('start_all_polling', { connectionId: selectedConnectionId.value })
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function stopAllPolling() {
  if (!selectedConnectionId.value) return
  try {
    await invoke('stop_all_polling', { connectionId: selectedConnectionId.value })
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

const isConnected = computed(() => selectedConnectionState.value === 'Connected')
const isReconnecting = computed(() => selectedConnectionState.value === 'Reconnecting')
const isDisconnected = computed(() => selectedConnectionState.value === 'Disconnected')
const hasConnection = computed(() => selectedConnectionId.value !== null)
const actions = computed(() => ({
  open: { id: 'open', label: t('toolbar.openProjectTitle'), shortcut: shortcuts.open, action: () => run(openProject) },
  save: { id: 'save', label: t('toolbar.saveProjectTitle'), icon: 'save', shortcut: shortcuts.save, separatorBefore: true, action: () => run(saveProject) },
  saveAs: { id: 'save-as', label: t('toolbar.saveAsTitle'), shortcut: shortcuts.saveAs, action: () => run(saveProjectAs) },
  newConnection: { id: 'new-connection', label: t('toolbar.newConnection'), icon: 'add', action: () => { showNewConn.value = true } },
  settings: { id: 'connection-settings', label: t('parity.connectionSettings'), title: t('dialog.editConnectionHint'), disabled: !hasConnection.value || !isDisconnected.value, action: () => { editingConnectionId.value = selectedConnectionId.value } },
  connect: { id: 'connect', label: t('toolbar.connect'), icon: 'play', tone: 'start', separatorBefore: true, disabled: !hasConnection.value || isConnected.value || isReconnecting.value, action: () => run(connectMaster) },
  disconnect: { id: 'disconnect', label: isReconnecting.value ? t('toolbar.cancelReconnect') : t('toolbar.disconnect'), icon: 'stop', tone: 'stop', disabled: !hasConnection.value || isDisconnected.value, action: () => run(disconnectMaster) },
  remove: { id: 'delete-connection', label: t('toolbar.deleteConnection'), tone: 'danger', separatorBefore: true, disabled: !hasConnection.value, action: () => run(deleteMaster) },
  newScanGroup: { id: 'new-scan-group', label: t('toolbar.newScanGroup'), icon: 'add', disabled: !hasConnection.value, action: () => { showNewScanGroup.value = true } },
  startPolling: { id: 'start-polling', label: t('toolbar.startPolling'), icon: 'play', tone: 'start', separatorBefore: true, disabled: !hasConnection.value || !isConnected.value, action: () => run(startAllPolling) },
  stopPolling: { id: 'stop-polling', label: t('toolbar.stopPolling'), icon: 'stop', tone: 'stop', disabled: !hasConnection.value || !isConnected.value, action: () => run(stopAllPolling) },
  write: { id: 'write', label: t('toolbar.write'), icon: 'write', separatorBefore: true, disabled: !hasConnection.value || !isConnected.value, action: () => { showWriteModal.value = true } },
  scan: { id: 'scan', label: t('toolbar.scan'), icon: 'scan', disabled: !hasConnection.value || !isConnected.value, action: () => { showScanDialog.value = true } },
}) satisfies Record<string, ToolbarAction>)
const menus = computed<ToolbarMenuDefinition[]>(() => [
  { id: 'file', label: t('toolbar.menuFile'), items: [actions.value.open, actions.value.save, actions.value.saveAs] },
  { id: 'connection', label: t('toolbar.menuConnection'), items: [
    actions.value.newConnection, actions.value.settings, actions.value.connect, actions.value.disconnect, actions.value.remove,
  ] },
  { id: 'polling', label: t('toolbar.menuPolling'), items: [actions.value.newScanGroup, actions.value.startPolling, actions.value.stopPolling] },
  { id: 'tools', label: t('common.tools'), items: [{ ...actions.value.write, separatorBefore: false }, actions.value.scan] },
  { id: 'help', label: t('parity.menuHelp'), items: [
    { id: 'update', label: updateButtonLabel.value, disabled: updateBusy.value, busy: updateBusy.value, action: manualCheckUpdate },
  ] },
])
const quickActions = computed<ToolbarAction[]>(() => [
  actions.value.newConnection, actions.value.save, actions.value.connect, actions.value.disconnect,
  actions.value.startPolling, actions.value.stopPolling,
])
</script>

<template>
  <AppToolbar :title="t('toolbar.appTitleMaster')" :menus="menus" :actions="quickActions"
    :busy="busy" :project-path="currentProjectPath" :status="updateBusy ? updateButtonLabel : ''" />

  <NewConnectionDialog :show="showNewConn" @close="showNewConn = false" @created="refreshTree" />
  <NewConnectionDialog v-if="editingConnectionId" :show="true" :connection-id="editingConnectionId"
    @close="editingConnectionId = null" @saved="refreshTree" />
  <NewScanGroupDialog
    :show="showNewScanGroup"
    :connection-id="selectedConnectionId"
    @close="showNewScanGroup = false"
    @created="refreshTree"
  />
  <WriteDialog
    :show="showWriteModal"
    :connection-id="selectedConnectionId"
    @close="showWriteModal = false"
  />
  <ScanDialog v-if="showScanDialog" @close="showScanDialog = false" />
</template>

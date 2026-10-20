<script setup lang="ts">
import { computed, inject, ref, type Ref } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'
import {
  useI18n,
  AppToolbar,
  useProjectShortcuts,
  useTheme,
  type ThemeMode,
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
const { mode: themeMode, setMode: setThemeMode } = useTheme()
function themeItem(id: ThemeMode, labelKey: string): ToolbarAction {
  return {
    id: `theme-${id}`,
    label: `${themeMode.value === id ? '✓ ' : ''}${t(labelKey)}`,
    separatorBefore: id === 'auto',
    action: () => setThemeMode(id),
  }
}

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

const workspaceReplaced = inject<() => void>('workspaceReplaced', refreshTree)
const projectFilters = [{ Name: 'Modbus Project', Extensions: ['modbusproj'] }]
async function openProject() {
  try {
    const path = await App.OpenFileDialog(projectFilters)
    if (!path) return
    await App.LoadProject(path)
    currentProjectPath.value = path
    workspaceReplaced()
  } catch (e) { await showAlert(String(e)) }
}

async function saveProject() {
  if (!currentProjectPath.value) return saveProjectAs()
  try {
    await App.SaveProject(currentProjectPath.value)
  } catch (e) { await showAlert(String(e)) }
}

async function saveProjectAs() {
  try {
    const path = await App.SaveFileDialog('untitled.modbusproj', projectFilters)
    if (!path) return
    await App.SaveProject(path)
    currentProjectPath.value = path
  } catch (e) { await showAlert(String(e)) }
}

async function connectMaster() {
  if (!selectedConnectionId.value) return
  try {
    await App.Connect(selectedConnectionId.value)
    refreshTree()
    const doScan = await showConfirm(t('errors.connectSuccessAskScan'))
    if (doScan) showScanDialog.value = true
  } catch (e) { await showAlert(String(e)) }
}

async function disconnectMaster() {
  if (!selectedConnectionId.value) return
  try {
    await App.Disconnect(selectedConnectionId.value)
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function deleteMaster() {
  if (!selectedConnectionId.value) return
  if (!await showConfirm(t('errors.confirmDeleteConnection'))) return
  try {
    await App.DeleteConnection(selectedConnectionId.value)
    selectedConnectionId.value = null
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function startAllPolling() {
  if (!selectedConnectionId.value) return
  try {
    await App.StartAllPolling(selectedConnectionId.value)
    refreshTree()
  } catch (e) { await showAlert(String(e)) }
}

async function stopAllPolling() {
  if (!selectedConnectionId.value) return
  try {
    await App.StopAllPolling(selectedConnectionId.value)
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
  { id: 'tools', label: t('common.tools'), items: [
    { ...actions.value.write, separatorBefore: false }, actions.value.scan,
    themeItem('auto', 'parity.themeSystem'),
    themeItem('light', 'parity.themeLight'),
    themeItem('dark', 'parity.themeDark'),
  ] },
  { id: 'help', label: t('parity.menuHelp'), items: [
    { id: 'releases', label: t('parity.openReleases'), action: () => BrowserOpenURL('https://github.com/Karl-Dai/ModbusSim/releases') },
  ] },
])
const quickActions = computed<ToolbarAction[]>(() => [
  actions.value.newConnection, actions.value.save, actions.value.connect, actions.value.disconnect,
  actions.value.startPolling, actions.value.stopPolling,
])
</script>

<template>
  <AppToolbar :title="t('toolbar.appTitleMaster')" :menus="menus" :actions="quickActions"
    :busy="busy" :project-path="currentProjectPath"  />

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

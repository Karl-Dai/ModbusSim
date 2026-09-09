<script setup lang="ts">
import { ToolbarMenu, showPrompt } from 'shared-frontend'
import ConnectionSettingsDialog from './ConnectionSettingsDialog.vue'
import ToolsDialog from './ToolsDialog.vue'
import AboutDialog from './AboutDialog.vue'
import { computed, onMounted, onBeforeUnmount, ref, inject, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { save, open } from '@tauri-apps/plugin-dialog'
import {
  useI18n,
  useUpdateProgress,
  localizeUpdateError,
  LangToggle,
  VersionBadge,
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
const openMenu = ref<string | null>(null)
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
function keyboard(event: KeyboardEvent) {
  if (!(event.metaKey || event.ctrlKey) || document.querySelector('[aria-modal="true"]')) return
  if (event.key.toLowerCase() === 's') { event.preventDefault(); void run(event.shiftKey ? saveProjectAs : saveProject) }
  if (event.key.toLowerCase() === 'o') { event.preventDefault(); void run(openProject) }
}
onMounted(() => window.addEventListener('keydown', keyboard))
onBeforeUnmount(() => window.removeEventListener('keydown', keyboard))
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


const menus = computed(() => [
  { id: 'config', label: t('parity.menuConfig'), items: [
    { id: 'open', label: t('toolbar.openProjectTitle'), action: () => run(openProject) },
    { id: 'open-path', label: t('parity.openPath'), action: () => run(openProjectByPath) },
    { id: 'save', label: t('toolbar.saveProjectTitle'), action: () => run(saveProject) },
    { id: 'save-as', label: t('toolbar.saveAsTitle'), action: () => run(saveProjectAs) },
  ] },
  { id: 'new', label: t('parity.menuNew'), items: [
    { id: 'new-connection', label: t('toolbar.newConnection'), action: () => { showNewConn.value = true } },
    { id: 'new-slave', label: t('toolbar.newSlave'), disabled: !selectedConnectionId.value, action: () => { showNewSlave.value = true } },
  ] },
])
const secondaryMenus = computed(() => [
  { id: 'registers', label: t('parity.menuRegisters'), items: [
    { id: 'import-csv', label: t('parity.importCsv'), disabled: selectedSlaveId.value === null || selectedConnectionState.value !== 'Stopped', action: registerActions.importCsv },
    { id: 'export-csv', label: t('parity.exportCsv'), disabled: selectedSlaveId.value === null, action: registerActions.exportCsv },
    { id: 'csv-template', label: t('parity.csvTemplate'), action: registerActions.template },
  ] },
  { id: 'settings', label: t('parity.menuSettings'), items: [
    { id: 'connection-settings', label: t('parity.connectionSettings'), disabled: !selectedConnectionId.value, action: () => { showSettings.value = true } },
    { id: 'simulation', label: t('simulationSettings.open'), disabled: selectedSlaveId.value === null, action: registerActions.simulation },
    { id: 'close-connection', label: t('toolbar.closeConnection'), disabled: !selectedConnectionId.value, action: () => run(closeConnection) },
  ] },
  { id: 'tools', label: t('common.tools'), items: [
    { id: 'tools-dialog', label: t('parity.toolsTitle'), action: () => { showTools.value = true } },
  ] },
  { id: 'help', label: t('parity.menuHelp'), items: [
    { id: 'update', label: updateButtonLabel.value, disabled: updateBusy.value, action: manualCheckUpdate },
    { id: 'about', label: t('about.title'), action: () => { showAbout.value = true } },
  ] },
])
</script>


<template>
  <div class="toolbar slave-toolbar" :aria-busy="busy">
    <div class="toolbar-main">
      <ToolbarMenu v-for="menu in menus" :key="menu.id" v-bind="menu" :open="openMenu === menu.id" :disabled="busy" @toggle="openMenu = openMenu === menu.id ? null : menu.id" @close="openMenu = null" />
      <div class="toolbar-divider" aria-hidden="true" />
      <div class="toolbar-group" :aria-label="t('parity.currentConnection')">
        <button class="toolbar-btn btn-start" :disabled="busy || !selectedConnectionId || selectedConnectionState === 'Running'" @click="run(startConnection)">{{ t('toolbar.start') }}</button>
        <button class="toolbar-btn btn-stop" :disabled="busy || !selectedConnectionId || selectedConnectionState === 'Stopped'" @click="run(stopConnection)">{{ t('toolbar.stop') }}</button>
      </div>
      <div class="toolbar-divider" aria-hidden="true" />
      <button class="toolbar-btn btn-start" :disabled="busy" @click="run(() => changeAll('start'))">{{ t('parity.startAll') }}</button>
      <button class="toolbar-btn btn-stop" :disabled="busy" @click="run(() => changeAll('stop'))">{{ t('parity.stopAll') }}</button>
      <div class="toolbar-divider" aria-hidden="true" />
      <ToolbarMenu v-for="menu in secondaryMenus.slice(0, 3)" :key="menu.id" v-bind="menu" :open="openMenu === menu.id" :disabled="busy" @toggle="openMenu = openMenu === menu.id ? null : menu.id" @close="openMenu = null" />
    </div>
    <div class="toolbar-aside">
      <span class="operation-status" role="status">{{ statusText }}</span>
      <ToolbarMenu v-bind="secondaryMenus[3]" :open="openMenu === 'help'" :disabled="busy" @toggle="openMenu = openMenu === 'help' ? null : 'help'" @close="openMenu = null" />
      <LangToggle /><VersionBadge />
    </div>
  </div>
  <NewConnectionDialog :show="showNewConn" @close="showNewConn = false" @created="refreshTree" />
  <NewSlaveDialog :show="showNewSlave" :connection-id="selectedConnectionId" @close="showNewSlave = false" @created="refreshTree" />
  <ConnectionSettingsDialog v-if="showSettings && selectedConnectionId" :connection-id="selectedConnectionId" @saved="refreshTree" @close="showSettings = false" />
  <ToolsDialog v-if="showTools" @close="showTools = false" />
  <AboutDialog v-if="showAbout" @close="showAbout = false" />
</template>
<style scoped>
.slave-toolbar :deep(.toolbar-btn) { padding: 5px 7px; }
.operation-status { color: var(--c-subtext0); font-size: 11px; max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 1100px) { .operation-status { display: none; } }
</style>

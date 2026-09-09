<script setup lang="ts">
import { computed, inject, ref, type Ref } from 'vue'
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
import ScanDialog from './ScanDialog.vue'
import NewConnectionDialog from './NewConnectionDialog.vue'
import NewScanGroupDialog from './NewScanGroupDialog.vue'
import WriteDialog from './WriteDialog.vue'

const { t } = useI18n()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedConnectionState = inject<Ref<string>>('selectedConnectionState')!
const refreshTree = inject<() => void>('refreshTree')!

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

const isConnected = () => selectedConnectionState.value === 'Connected'
const isReconnecting = () => selectedConnectionState.value === 'Reconnecting'
const isDisconnected = () => selectedConnectionState.value === 'Disconnected'
const hasConnection = () => selectedConnectionId.value !== null
</script>

<template>
  <div class="toolbar">
    <div class="toolbar-main">
      <div class="toolbar-group">
        <button class="toolbar-btn" @click="openProject" :title="t('toolbar.openProjectTitle')">{{ t('toolbar.open') }}</button>
        <button class="toolbar-btn" @click="saveProject" :title="t('toolbar.saveProjectTitle')">{{ t('common.save') }}</button>
        <button class="toolbar-btn" @click="saveProjectAs" :title="t('toolbar.saveAsTitle')">{{ t('toolbar.saveAs') }}</button>
      </div>

      <div class="toolbar-divider"></div>

      <div class="toolbar-group">
        <button class="toolbar-btn" @click="showNewConn = true">
          <span class="btn-icon">+</span> {{ t('toolbar.newConnection') }}
        </button>
        <button class="toolbar-btn" :disabled="!hasConnection() || !isDisconnected()"
          :title="t('dialog.editConnectionHint')" @click="editingConnectionId = selectedConnectionId">
          {{ t('parity.connectionSettings') }}
        </button>
      </div>

      <div class="toolbar-divider"></div>

      <div class="toolbar-group">
        <button class="toolbar-btn btn-start" :disabled="!hasConnection() || isConnected() || isReconnecting()" @click="connectMaster">
          {{ t('toolbar.connect') }}
        </button>
        <button class="toolbar-btn btn-stop" :disabled="!hasConnection() || isDisconnected()" @click="disconnectMaster">
          {{ isReconnecting() ? t('toolbar.cancelReconnect') : t('toolbar.disconnect') }}
        </button>
        <button class="toolbar-btn btn-close" :disabled="!hasConnection()" @click="deleteMaster">
          {{ t('common.delete') }}
        </button>
      </div>

      <div class="toolbar-divider"></div>

      <div class="toolbar-group">
        <button class="toolbar-btn" :disabled="!hasConnection()" @click="showNewScanGroup = true">
          <span class="btn-icon">+</span> {{ t('toolbar.addScanGroup') }}
        </button>
        <button class="toolbar-btn btn-start" :disabled="!hasConnection() || !isConnected()" @click="startAllPolling">
          {{ t('toolbar.startAll') }}
        </button>
        <button class="toolbar-btn btn-stop" :disabled="!hasConnection() || !isConnected()" @click="stopAllPolling">
          {{ t('toolbar.stopAll') }}
        </button>
      </div>

      <div class="toolbar-divider"></div>

      <div class="toolbar-group">
        <button class="toolbar-btn" :disabled="!hasConnection() || !isConnected()" @click="showWriteModal = true">
          {{ t('toolbar.write') }}
        </button>
        <button class="toolbar-btn" :disabled="!hasConnection() || !isConnected()" @click="showScanDialog = true">
          {{ t('toolbar.scan') }}
        </button>
      </div>

    </div>
    <div class="toolbar-aside">
      <button class="toolbar-btn" :disabled="updateBusy" @click="manualCheckUpdate">
        <span aria-live="polite">{{ updateButtonLabel }}</span>
      </button>
      <LangToggle />
      <VersionBadge />
      <span class="toolbar-title">{{ t('toolbar.appTitleMaster') }}</span>
    </div>
  </div>

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

<style scoped>
.toolbar { display: flex; align-items: center; height: 42px; padding: 0 8px; gap: 0; }
.toolbar-group { display: flex; gap: 2px; }
.toolbar-divider { width: 1px; height: 20px; background: #313244; margin: 0 6px; }
.toolbar-btn {
  display: flex; align-items: center; gap: 4px;
  padding: 4px 10px; border: none; background: transparent;
  color: #cdd6f4; cursor: pointer; border-radius: 4px;
  font-size: 12px; white-space: nowrap;
}
.toolbar-btn:hover:not(:disabled) { background: #313244; }
.toolbar-btn:disabled { opacity: 0.4; cursor: default; }
.btn-icon { font-weight: bold; font-size: 14px; }
.btn-start { color: #a6e3a1; }
.btn-stop { color: #fab387; }
.btn-close { color: #f38ba8; }
.toolbar-aside > * { flex: none; }
.toolbar-title { font-size: 13px; font-weight: 600; color: #6c7086; padding-right: 8px; }
</style>

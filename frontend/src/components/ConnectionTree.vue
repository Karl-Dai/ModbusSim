<script setup lang="ts">
import { ref, inject, watch, onMounted, onBeforeUnmount, computed, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert, showConfirm } from 'shared-frontend'
import EditSlaveDialog from './EditSlaveDialog.vue'
import ClientConnectionsModal from './ClientConnectionsModal.vue'
import type { ClientInfo } from '../types/clients'

const { t } = useI18n()

interface SlaveConnection {
  id: string
  bind_address: string
  port: number
  state: string
  device_count: number
  clients: ClientInfo[] | null
}

interface SlaveDevice {
  slave_id: number
  name: string
  register_count: number
}

interface TreeConnection {
  conn: SlaveConnection
  expanded: boolean
  devices: TreeDevice[]
}

interface TreeDevice {
  device: SlaveDevice
  expanded: boolean
  connectionId: string
}

const REGISTER_GROUPS = [
  { type: 'coil', fc: 'FC01', labelKey: 'table.coil', descKey: 'connectionTree.coilDesc' as const },
  { type: 'discrete_input', fc: 'FC02', labelKey: 'table.discreteInput', descKey: 'connectionTree.discreteInputDesc' as const },
  { type: 'input_register', fc: 'FC04', labelKey: 'table.inputRegister', descKey: 'connectionTree.inputRegisterDesc' as const },
  { type: 'holding_register', fc: 'FC03', labelKey: 'table.holdingRegister', descKey: 'connectionTree.holdingRegisterDesc' as const },
]

const helpTooltip = ref<{ type: string; x: number; y: number } | null>(null)

function showHelpTooltip(e: MouseEvent, type: string) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  helpTooltip.value = { type, x: rect.right + 8, y: rect.top + rect.height / 2 }
}

function hideHelpTooltip() {
  helpTooltip.value = null
}

const emit = defineEmits<{
  (e: 'connection-select', id: string, state: string): void
  (e: 'slave-select', connectionId: string, slaveId: number): void
  (e: 'group-select', connectionId: string, slaveId: number, regType: string): void
}>()

const treeRefreshKey = inject<Ref<number>>('treeRefreshKey')!
const selectedConnectionState = inject<Ref<string>>('selectedConnectionState')!
const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedSlaveId = inject<Ref<number | null>>('selectedSlaveId')!
const selectedRegisterType = inject<Ref<string | null>>('selectedRegisterType')!

const treeData = ref<TreeConnection[]>([])
const batchMode = ref(false)
const checkedConnections = ref(new Set<string>())
const batchBusy = ref(false)
function toggleChecked(id: string) {
  const next = new Set(checkedConnections.value)
  if (next.has(id)) next.delete(id); else next.add(id)
  checkedConnections.value = next
}
async function deleteCheckedConnections() {
  if (batchBusy.value || !checkedConnections.value.size) return
  const ids = [...checkedConnections.value]
  batchBusy.value = true
  try {
    if (!await showConfirm(t('parity.confirmDeleteConnections', { n: ids.length }))) return
    const failures: string[] = []
    for (const id of ids) {
      try {
        await invoke('delete_slave_connection', { id })
        checkedConnections.value.delete(id)
        if (selectedConnectionId.value === id) {
          selectedConnectionId.value = null; selectedConnectionState.value = 'Stopped'
          selectedSlaveId.value = null; selectedRegisterType.value = null
        }
      } catch (e) { failures.push(`${id}: ${String(e)}`) }
    }
    await loadTree()
    if (failures.length) await showAlert(failures.join('\n'))
    else { batchMode.value = false; checkedConnections.value = new Set() }
  } finally { batchBusy.value = false }
}
const clientsConnectionId = ref<string | null>(null)
const clientsConnection = computed(() => treeData.value.find(row => row.conn.id === clientsConnectionId.value)?.conn)
const clientsError = ref('')
let clientsTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
async function refreshClients() {
  try {
    const connections = await invoke<SlaveConnection[]>('list_slave_connections')
    if (disposed) return
    for (const row of treeData.value) {
      const latest = connections.find(conn => conn.id === row.conn.id)
      if (latest) {
        row.conn = latest
        if (latest.id === selectedConnectionId.value) selectedConnectionState.value = latest.state
      }
    }
    clientsError.value = ''
  } catch (error) {
    if (!disposed) clientsError.value = String(error)
  } finally {
    if (!disposed) clientsTimer = setTimeout(refreshClients, 1000)
  }
}
onMounted(() => { void refreshClients() })
onBeforeUnmount(() => {
  disposed = true
  clearTimeout(clientsTimer)
})

const contextMenu = ref({ show: false, x: 0, y: 0, type: '' as 'connection' | 'slave', connectionId: '', slaveId: 0, slaveName: '', connState: '' })
const editSlave = ref({ show: false, connectionId: '', slaveId: 0, name: '' })

let treeLoadEpoch = 0
async function loadTree() {
  const epoch = ++treeLoadEpoch
  try {
    const connections = await invoke<SlaveConnection[]>('list_slave_connections')
    const newTree: TreeConnection[] = []

    for (const conn of connections) {
      const existing = treeData.value.find(t => t.conn.id === conn.id)
      const devices = await invoke<SlaveDevice[]>('list_slave_devices', { connectionId: conn.id })
      newTree.push({
        conn,
        expanded: existing ? existing.expanded : true,
        devices: devices.map(d => ({
          device: d,
          expanded: existing?.devices.find(ed => ed.device.slave_id === d.slave_id)?.expanded ?? true,
          connectionId: conn.id,
        })),
      })
    }
    if (disposed || epoch !== treeLoadEpoch) return
    treeData.value = newTree
    checkedConnections.value = new Set([...checkedConnections.value].filter(id => newTree.some(row => row.conn.id === id)))
  } catch (e) {
    console.error('Failed to load tree:', e)
  }
}

watch(treeRefreshKey, () => loadTree())
onMounted(loadTree)

function toggleConnection(tc: TreeConnection) {
  tc.expanded = !tc.expanded
}

function toggleDevice(td: TreeDevice) {
  td.expanded = !td.expanded
}

function selectConnection(tc: TreeConnection) {
  emit('connection-select', tc.conn.id, tc.conn.state)
}

function selectSlave(tc: TreeConnection, td: TreeDevice) {
  selectedConnectionState.value = tc.conn.state
  emit('slave-select', tc.conn.id, td.device.slave_id)
}

function selectGroup(tc: TreeConnection, td: TreeDevice, regType: string) {
  selectedConnectionState.value = tc.conn.state
  emit('group-select', tc.conn.id, td.device.slave_id, regType)
}

function showContextMenuForConnection(e: MouseEvent, tc: TreeConnection) {
  e.preventDefault()
  contextMenu.value = {
    show: true,
    x: e.clientX,
    y: e.clientY,
    type: 'connection',
    connectionId: tc.conn.id,
    slaveId: 0,
    slaveName: '',
    connState: tc.conn.state,
  }
}

function showContextMenuForSlave(e: MouseEvent, tc: TreeConnection, td: TreeDevice) {
  e.preventDefault()
  contextMenu.value = {
    show: true,
    x: e.clientX,
    y: e.clientY,
    type: 'slave',
    connectionId: tc.conn.id,
    slaveId: td.device.slave_id,
    slaveName: td.device.name,
    connState: '',
  }
}

function ctxEditSlave() {
  editSlave.value = {
    show: true,
    connectionId: contextMenu.value.connectionId,
    slaveId: contextMenu.value.slaveId,
    name: contextMenu.value.slaveName,
  }
  closeContextMenu()
}

async function onSlaveUpdated(newSlaveId: number) {
  if (
    selectedConnectionId.value === editSlave.value.connectionId
    && selectedSlaveId.value === editSlave.value.slaveId
  ) {
    selectedSlaveId.value = newSlaveId
  }
  await loadTree()
}

function closeContextMenu() {
  contextMenu.value.show = false
}

async function ctxStartConnection() {
  closeContextMenu()
  try {
    await invoke('start_slave_connection', { id: contextMenu.value.connectionId })
    await loadTree()
  } catch (e) { await showAlert(String(e)) }
}

async function ctxStopConnection() {
  closeContextMenu()
  try {
    await invoke('stop_slave_connection', { id: contextMenu.value.connectionId })
    await loadTree()
  } catch (e) { await showAlert(String(e)) }
}

async function ctxDeleteConnection() {
  closeContextMenu()
  if (!(await showConfirm(t('errors.confirmDeleteConnection')))) return
  try {
    await invoke('delete_slave_connection', { id: contextMenu.value.connectionId })
    if (selectedConnectionId.value === contextMenu.value.connectionId) {
      selectedConnectionId.value = null
    }
    await loadTree()
  } catch (e) { await showAlert(String(e)) }
}

async function ctxDeleteSlave() {
  closeContextMenu()
  if (!(await showConfirm(t('errors.confirmDeleteSlave')))) return
  try {
    await invoke('remove_slave_device', {
      connectionId: contextMenu.value.connectionId,
      slaveId: contextMenu.value.slaveId,
    })
    await loadTree()
  } catch (e) { await showAlert(String(e)) }
}
</script>

<template>
  <div class="connection-tree" @click="closeContextMenu">
    <div class="tree-header"><span>{{ t('tree.connections') }}</span><button class="tree-action" :aria-pressed="batchMode" :disabled="batchBusy" @click="batchMode = !batchMode; checkedConnections = new Set()">{{ t('parity.batchMode') }}</button></div>
    <div v-if="batchMode" class="tree-batch-actions">
      <button class="tree-action" :disabled="batchBusy" @click="checkedConnections = new Set(treeData.map(row => row.conn.id))">{{ t('parity.selectAll') }}</button>
      <button class="tree-action" :disabled="batchBusy || !checkedConnections.size" @click="deleteCheckedConnections">{{ t('common.delete') }} ({{ checkedConnections.size }})</button>
    </div>
    <div v-if="treeData.length === 0" class="tree-empty">{{ t('tree.noConnection') }}</div>

    <div v-for="tc in treeData" :key="tc.conn.id" class="tree-node-group">
      <!-- Connection Node -->
      <div
        :class="['tree-node connection-node', { selected: tc.conn.id === selectedConnectionId && selectedSlaveId === null }]"
        @click.stop="selectConnection(tc)"
        @contextmenu.prevent="showContextMenuForConnection($event, tc)"
      >
        <input v-if="batchMode" type="checkbox" :checked="checkedConnections.has(tc.conn.id)" :disabled="batchBusy" :aria-label="`${tc.conn.bind_address}:${tc.conn.port}`" @click.stop @change="toggleChecked(tc.conn.id)" />
        <span class="node-arrow" @click.stop="toggleConnection(tc)">{{ tc.expanded ? '▼' : '▶' }}</span>
        <span :class="['node-status', tc.conn.state === 'Running' ? 'running' : 'stopped']"></span>
        <span class="node-label">{{ tc.conn.bind_address }}:{{ tc.conn.port }}</span>
        <button v-if="tc.conn.clients != null" class="clients-badge"
          :class="{ active: !clientsError && tc.conn.clients.length > 0 }"
          :title="t('clients.title')" :aria-label="t('clients.title') + ': ' + (clientsError ? t('clients.unavailable') : t('clients.count', { n: tc.conn.clients.length }))"
          @click.stop="clientsConnectionId = tc.conn.id">
          {{ clientsError ? '—' : t('clients.count', { n: tc.conn.clients.length }) }}
        </button>
      </div>

      <!-- Slave Nodes -->
      <template v-if="tc.expanded">
        <div v-for="td in tc.devices" :key="td.device.slave_id" class="tree-child">
          <div
            :class="['tree-node slave-node', { selected: tc.conn.id === selectedConnectionId && td.device.slave_id === selectedSlaveId && selectedRegisterType === null }]"
            @click.stop="selectSlave(tc, td)"
            @contextmenu.prevent="showContextMenuForSlave($event, tc, td)"
          >
            <span class="node-arrow" @click.stop="toggleDevice(td)">{{ td.expanded ? '▼' : '▶' }}</span>
            <span class="node-label" :title="td.device.name?.trim() || t('station.defaultName', { id: td.device.slave_id })">
              {{ td.device.name?.trim() ? t('station.named', { name: td.device.name.trim(), id: td.device.slave_id }) : t('station.defaultName', { id: td.device.slave_id }) }}
            </span>
          </div>

          <!-- Register Group Nodes -->
          <template v-if="td.expanded">
            <div
              v-for="group in REGISTER_GROUPS"
              :key="group.type"
              :class="['tree-node group-node', { selected: tc.conn.id === selectedConnectionId && td.device.slave_id === selectedSlaveId && selectedRegisterType === group.type }]"
              @click.stop="selectGroup(tc, td, group.type)"
            >
              <span class="node-label" :title="`${group.fc} ${t(group.labelKey)}`">{{ group.fc }} {{ t(group.labelKey) }}</span>
              <span class="help-icon" @mouseenter="showHelpTooltip($event, group.type)" @mouseleave="hideHelpTooltip">?</span>
            </div>
          </template>
        </div>
      </template>
    </div>

    <!-- Context Menu -->
    <div
      v-if="contextMenu.show"
      class="context-menu"
      :style="{ top: contextMenu.y + 'px', left: contextMenu.x + 'px' }"
      @click.stop
    >
      <template v-if="contextMenu.type === 'connection'">
        <div
          v-if="contextMenu.connState === 'Stopped'"
          class="context-menu-item"
          @click="ctxStartConnection"
        >{{ t('toolbar.startConnection') }}</div>
        <div
          v-else
          class="context-menu-item"
          @click="ctxStopConnection"
        >{{ t('toolbar.stopConnection') }}</div>
        <div class="context-menu-item danger" @click="ctxDeleteConnection">{{ t('tree.deleteConnection') }}</div>
      </template>
      <template v-if="contextMenu.type === 'slave'">
        <div class="context-menu-item" @click="ctxEditSlave">{{ t('tree.editSlave') }}</div>
        <div class="context-menu-item danger" @click="ctxDeleteSlave">{{ t('tree.deleteSlave') }}</div>
      </template>
    </div>

    <ClientConnectionsModal v-if="clientsConnection && clientsConnection.clients != null"
      :label="`${clientsConnection.bind_address}:${clientsConnection.port}`"
      :clients="clientsConnection.clients" :error="clientsError"
      @close="clientsConnectionId = null" />

    <EditSlaveDialog
      :show="editSlave.show"
      :connection-id="editSlave.connectionId"
      :original-slave-id="editSlave.slaveId"
      :initial-name="editSlave.name"
      @close="editSlave.show = false"
      @updated="onSlaveUpdated"
    />

    <!-- Help Tooltip (fixed position, avoids clipping) -->
    <Teleport to="body">
      <div
        v-if="helpTooltip"
        class="help-tooltip"
        :style="{ left: helpTooltip.x + 'px', top: helpTooltip.y + 'px' }"
      >
        {{ (() => { const g = REGISTER_GROUPS.find(g => g.type === helpTooltip!.type); return g ? t(g.descKey) : '' })() }}
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.tree-header { display: flex; align-items: center; justify-content: space-between; gap: 4px; }
.tree-action { padding: 3px 5px; border: 1px solid var(--c-surface1); border-radius: 4px; background: transparent; color: var(--c-subtext1); font-size: 11px; cursor: pointer; }
.tree-action:hover { background: var(--c-surface0); }
.tree-batch-actions { display: flex; gap: 6px; padding: 4px 8px; }

.clients-badge { flex-shrink: 0; margin-left: auto; padding: 3px 6px; border: 1px solid var(--c-surface2); border-radius: 4px; background: var(--c-surface0); color: var(--c-text); font: inherit; font-size: 12px; cursor: pointer; }
.clients-badge.active { color: var(--c-green); border-color: var(--c-green); }
.clients-badge:hover { background: var(--c-surface1); }
.clients-badge:focus-visible { outline: 2px solid var(--c-blue); outline-offset: 2px; }

.connection-tree {
  padding: 0;
  font-size: 13px;
  user-select: none;
  height: 100%;
  position: relative;
}

.tree-header {
  padding: 8px 12px;
  font-size: 11px;
  text-transform: uppercase;
  color: var(--c-overlay0);
  letter-spacing: 0.5px;
}

.tree-empty {
  padding: 16px 12px;
  color: var(--c-overlay0);
  font-size: 12px;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  cursor: pointer;
  border-radius: 3px;
  margin: 1px 4px;
}

.tree-node:hover {
  background: var(--c-surface0);
}

.tree-node.selected {
  background: var(--c-blue);
  color: var(--c-base);
}

.tree-child {
  padding-left: 16px;
}

.group-node {
  padding-left: 44px;
}

.help-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--c-surface1);
  color: var(--c-subtext0);
  font-size: 10px;
  font-weight: 600;
  cursor: pointer;
  flex-shrink: 0;
  margin-left: auto;
}

.help-icon:hover {
  background: var(--c-surface2);
  color: var(--c-text);
}

.tree-node.selected .help-icon {
  background: rgba(0, 0, 0, 0.2);
  color: var(--c-base);
}

:global(.help-tooltip) {
  position: fixed;
  z-index: 10000;
  background: var(--c-surface0);
  color: var(--c-text);
  border: 1px solid var(--c-surface1);
  border-radius: 6px;
  padding: 6px 10px;
  font-size: 11px;
  white-space: nowrap;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
  transform: translateY(-50%);
  pointer-events: none;
}

.node-arrow {
  font-size: 8px;
  width: 12px;
  text-align: center;
  flex-shrink: 0;
  color: var(--c-overlay0);
}

.tree-node.selected .node-arrow {
  color: var(--c-base);
}

.node-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.node-status.running {
  background: var(--c-green);
}

.node-status.stopped {
  background: var(--c-red);
}

.node-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Context Menu */
.context-menu {
  position: fixed;
  background: var(--c-base);
  border: 1px solid var(--c-surface1);
  border-radius: 6px;
  z-index: 999;
  min-width: 140px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
}

.context-menu-item {
  padding: 8px 14px;
  font-size: 13px;
  color: var(--c-text);
  cursor: pointer;
}

.context-menu-item:first-child {
  border-radius: 6px 6px 0 0;
}

.context-menu-item:last-child {
  border-radius: 0 0 6px 6px;
}

.context-menu-item:hover {
  background: var(--c-surface0);
}

.context-menu-item.danger {
  color: var(--c-red);
}

.context-menu-item.danger:hover {
  background: #3d2a30;
}
</style>

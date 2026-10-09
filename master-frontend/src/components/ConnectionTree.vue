<script setup lang="ts">
import { ref, inject, watch, onMounted, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n } from 'shared-frontend'
import type { MasterConnectionInfo, ScanGroupInfo } from '../types'

const { t } = useI18n()

const emit = defineEmits<{
  (e: 'connection-select', id: string, state: string): void
  (e: 'scan-group-select', connectionId: string, group: ScanGroupInfo): void
}>()

const treeRefreshKey = inject<Ref<number>>('treeRefreshKey')!
const refreshTree = inject<() => void>('refreshTree')!

interface TreeConnection {
  info: MasterConnectionInfo
  expanded: boolean
  scanGroups: ScanGroupInfo[]
}

const connections = ref<TreeConnection[]>([])
const selectedNodeId = ref<string | null>(null)

// Context menu
const contextMenu = ref<{ visible: boolean; x: number; y: number; type: 'connection' | 'scan_group'; connId: string; groupId?: string }>({
  visible: false, x: 0, y: 0, type: 'connection', connId: '', groupId: undefined
})

async function loadTree() {
  try {
    const conns = await invoke<MasterConnectionInfo[]>('list_master_connections')
    const newTree: TreeConnection[] = []
    for (const conn of conns) {
      const existing = connections.value.find(c => c.info.id === conn.id)
      const groups = await invoke<ScanGroupInfo[]>('list_scan_groups', { connectionId: conn.id })
      newTree.push({
        info: conn,
        expanded: existing?.expanded ?? true,
        scanGroups: groups,
      })
    }
    connections.value = newTree
  } catch (_e) {
    // Ignore errors on load
  }
}

watch(treeRefreshKey, loadTree)
onMounted(loadTree)

function selectConnection(conn: TreeConnection) {
  selectedNodeId.value = conn.info.id
  emit('connection-select', conn.info.id, conn.info.state)
}

function selectScanGroup(conn: TreeConnection, group: ScanGroupInfo) {
  selectedNodeId.value = `${conn.info.id}:${group.id}`
  emit('connection-select', conn.info.id, conn.info.state)
  emit('scan-group-select', conn.info.id, group)
}

function toggleExpand(conn: TreeConnection) {
  conn.expanded = !conn.expanded
}

function showContextMenu(e: MouseEvent, type: 'connection' | 'scan_group', connId: string, groupId?: string) {
  e.preventDefault()
  contextMenu.value = { visible: true, x: e.clientX, y: e.clientY, type, connId, groupId }
}

function hideContextMenu() {
  contextMenu.value.visible = false
}

async function ctxStartPolling() {
  if (!contextMenu.value.groupId) return
  try {
    await invoke('start_polling', {
      connectionId: contextMenu.value.connId,
      groupId: contextMenu.value.groupId,
    })
    refreshTree()
  } catch (_e) { /* ignore */ }
  hideContextMenu()
}

async function ctxStopPolling() {
  if (!contextMenu.value.groupId) return
  try {
    await invoke('stop_polling', {
      connectionId: contextMenu.value.connId,
      groupId: contextMenu.value.groupId,
    })
    refreshTree()
  } catch (_e) { /* ignore */ }
  hideContextMenu()
}

async function ctxRemoveScanGroup() {
  if (!contextMenu.value.groupId) return
  try {
    await invoke('remove_scan_group', {
      connectionId: contextMenu.value.connId,
      groupId: contextMenu.value.groupId,
    })
    refreshTree()
  } catch (_e) { /* ignore */ }
  hideContextMenu()
}

async function ctxDeleteConnection() {
  try {
    await invoke('delete_master_connection', {
      connectionId: contextMenu.value.connId,
    })
    refreshTree()
  } catch (_e) { /* ignore */ }
  hideContextMenu()
}

function fcLabel(fn: string): string {
  const map: Record<string, string> = {
    read_coils: 'FC01',
    read_discrete_inputs: 'FC02',
    read_holding_registers: 'FC03',
    read_input_registers: 'FC04',
  }
  return map[fn] || fn
}
</script>

<template>
  <div class="tree-container" @click="hideContextMenu">
    <div class="tree-header">{{ t('tree.connections') }}</div>

    <div v-if="connections.length === 0" class="tree-empty">
      {{ t('tree.noConnection') }}
    </div>

    <div v-for="conn in connections" :key="conn.info.id" class="tree-node-group">
      <!-- Connection node -->
      <div
        :class="['tree-node', { selected: selectedNodeId === conn.info.id }]"
        @click="selectConnection(conn)"
        @contextmenu="showContextMenu($event, 'connection', conn.info.id)"
      >
        <span class="node-expand" @click.stop="toggleExpand(conn)">
          {{ conn.expanded ? '▼' : '▶' }}
        </span>
        <span :class="['node-status', conn.info.state.toLowerCase()]"></span>
        <span class="node-label">{{ conn.info.target_address }}:{{ conn.info.port }}</span>
        <span class="node-slave">ID:{{ conn.info.slave_id }}</span>
      </div>

      <!-- Scan group children -->
      <div v-if="conn.expanded" class="tree-children">
        <div
          v-for="group in conn.scanGroups"
          :key="group.id"
          :class="['tree-node', 'tree-child', { selected: selectedNodeId === `${conn.info.id}:${group.id}` }]"
          @click="selectScanGroup(conn, group)"
          @contextmenu="showContextMenu($event, 'scan_group', conn.info.id, group.id)"
        >
          <span :class="['poll-indicator', { active: group.is_polling }]"></span>
          <span class="node-label">{{ group.name }}</span>
          <span class="node-meta">{{ fcLabel(group.function) }} {{ group.start_address }}x{{ group.quantity }}</span>
        </div>
      </div>
    </div>

    <!-- Context Menu -->
    <div v-if="contextMenu.visible" class="context-menu" :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }">
      <template v-if="contextMenu.type === 'connection'">
        <div class="ctx-item danger" @click="ctxDeleteConnection">{{ t('tree.deleteConnection') }}</div>
      </template>
      <template v-else>
        <div class="ctx-item" @click="ctxStartPolling">{{ t('tree.startPolling') }}</div>
        <div class="ctx-item" @click="ctxStopPolling">{{ t('tree.stopPolling') }}</div>
        <div class="ctx-item danger" @click="ctxRemoveScanGroup">{{ t('tree.deleteScanGroup') }}</div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.tree-container {
  padding: 4px 0;
  font-size: 12px;
  user-select: none;
}

.tree-header {
  padding: 8px 12px;
  font-size: 11px;
  text-transform: uppercase;
  color: var(--c-subtext0);
  letter-spacing: 0.5px;
}

.tree-empty {
  padding: 24px 12px;
  color: var(--c-subtext0);
  text-align: center;
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
  background: var(--c-hover-bg);
}

.tree-node.selected {
  background: var(--c-selection-bg);
  color: var(--c-selection-text);
}

.tree-node.selected .node-meta,
.tree-node.selected .node-slave {
  color: var(--c-selection-text);
  opacity: 1;
}

.tree-child {
  padding-left: 28px;
}

.node-expand {
  font-size: 8px;
  width: 12px;
  text-align: center;
  color: var(--c-subtext0);
}

.node-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.node-status.connected { background: var(--c-green); }
.node-status.disconnected { background: var(--c-red); }
.node-status.error { background: var(--c-peach); }
.node-status.reconnecting {
  background: var(--c-yellow);
  animation: reconnecting-pulse 1s ease-in-out infinite;
}

@keyframes reconnecting-pulse {
  0%, 100% { opacity: 0.4; }
  50% { opacity: 1; }
}

.poll-indicator {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--c-surface1);
}

.poll-indicator.active {
  background: var(--c-green);
  box-shadow: 0 0 4px var(--c-green);
}

.node-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-slave {
  font-size: 10px;
  color: var(--c-subtext0);
}

.node-meta {
  font-size: 10px;
  color: var(--c-subtext0);
  font-family: monospace;
}

/* Context Menu */
.context-menu {
  position: fixed;
  background: var(--c-base);
  border: 1px solid var(--c-surface1);
  border-radius: 6px;
  padding: 4px 0;
  z-index: 999;
  min-width: 120px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
}

.ctx-item {
  padding: 6px 14px;
  cursor: pointer;
  font-size: 12px;
}

.ctx-item:hover {
  background: var(--c-control-bg);
}

.ctx-item.danger {
  color: var(--c-red);
}
</style>

<script setup lang="ts">
import { ref, provide, onMounted, onUnmounted } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import Toolbar from './components/Toolbar.vue'
import ConnectionTree from './components/ConnectionTree.vue'
import DataTable from './components/DataTable.vue'
import ValuePanel from './components/ValuePanel.vue'
import LogPanel from './components/LogPanel.vue'
import { AppDialog, UpdateDialog } from 'shared-frontend'
import type { ScanGroupInfo, RegisterValueDto } from './types'

// Shared state
const selectedConnectionId = ref<string | null>(null)
const selectedConnectionState = ref<string>('Disconnected')
const selectedScanGroup = ref<ScanGroupInfo | null>(null)
const selectedRegisters = ref<RegisterValueDto[]>([])
const logExpanded = ref(false)
const addrMode = ref<'hex' | 'dec'>('hex')

// Provide shared state to children
provide('selectedConnectionId', selectedConnectionId)
provide('selectedConnectionState', selectedConnectionState)
provide('selectedScanGroup', selectedScanGroup)
provide('selectedRegisters', selectedRegisters)
provide('addrMode', addrMode)

// Tree refresh trigger
const treeRefreshKey = ref(0)
provide('treeRefreshKey', treeRefreshKey)

function refreshTree() {
  treeRefreshKey.value++
}
provide('refreshTree', refreshTree)

// Data refresh trigger
const dataRefreshKey = ref(0)
provide('dataRefreshKey', dataRefreshKey)

function refreshData() {
  dataRefreshKey.value++
}
provide('refreshData', refreshData)

// Listen for backend connection state events → auto-refresh tree & update state
let unlistenConnState: (() => void) | null = null
let unlistenPollError: (() => void) | null = null

onMounted(async () => {
  unlistenConnState = await listen<{ id: string; state: string }>('master-connection-state', (event) => {
    const { id, state } = event.payload
    if (selectedConnectionId.value === id) {
      selectedConnectionState.value = state
    }
    refreshTree()
  })
  unlistenPollError = await listen<{ connection_id: string; error: string }>('master-poll-error', () => {
    refreshTree()
  })
  setTimeout(() => {
    checkUpdate(false).catch((e) => console.warn('auto update check failed', e))
  }, 2000)
})

onUnmounted(() => {
  unlistenConnState?.()
  unlistenPollError?.()
})

function handleConnectionSelect(id: string, state: string) {
  selectedConnectionId.value = id
  selectedConnectionState.value = state
  selectedScanGroup.value = null
  selectedRegisters.value = []
}

function handleScanGroupSelect(connectionId: string, group: ScanGroupInfo) {
  selectedConnectionId.value = connectionId
  selectedScanGroup.value = group
  selectedRegisters.value = []
}

function handleRegisterSelect(regs: RegisterValueDto[]) {
  const prev = selectedRegisters.value
  // Same selection (same addresses) → update values in-place to avoid disrupting active edits
  if (regs.length === prev.length && regs.every((r, i) => r.address === prev[i].address)) {
    for (let i = 0; i < regs.length; i++) {
      prev[i].raw_value = regs[i].raw_value
      prev[i].display_value = regs[i].display_value
    }
    return
  }
  selectedRegisters.value = regs
}

function toggleLog() {
  logExpanded.value = !logExpanded.value
}

// --- Auto update -------------------------------------------------------------
type UpdateMeta = { version: string; notes: string; pub_date?: string | null }
const updateMeta = ref<UpdateMeta | null>(null)
const updateVisible = ref(false)

async function checkUpdate(force = false): Promise<UpdateMeta | null> {
  const meta = await invoke<UpdateMeta | null>('check_for_update', { force })
  if (meta) {
    updateMeta.value = meta
    updateVisible.value = true
  }
  return meta
}
provide('checkUpdate', checkUpdate)

</script>

<template>
  <div :class="['app-layout', { 'log-expanded': logExpanded }]">
    <header class="toolbar-area">
      <Toolbar />
    </header>

    <aside class="tree-area">
      <ConnectionTree
        @connection-select="handleConnectionSelect"
        @scan-group-select="handleScanGroupSelect"
      />
    </aside>
    <main class="content-area">
      <DataTable
        @register-select="handleRegisterSelect"
      />
    </main>
    <aside class="panel-area">
      <ValuePanel />
    </aside>

    <footer class="log-area">
      <LogPanel :expanded="logExpanded" @toggle="toggleLog" />
    </footer>
    <AppDialog />
    <UpdateDialog
      :visible="updateVisible"
      :version="updateMeta?.version ?? ''"
      :notes="updateMeta?.notes ?? ''"
      @close="updateVisible = false"
    />
  </div>
</template>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html, body, #app {
  height: 100%;
  width: 100%;
  overflow: hidden;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
  background: #11111b;
  color: #cdd6f4;
}

.app-layout {
  display: grid;
  grid-template-columns: 260px 1fr 280px;
  grid-template-rows: auto minmax(0, 1fr) 32px;
  grid-template-areas:
    "toolbar toolbar toolbar"
    "tree content panel"
    "log log log";
  height: 100vh;
  width: 100vw;
}

.app-layout.log-expanded {
  grid-template-rows: auto minmax(0, 1fr) 200px;
}

.toolbar-area {
  grid-area: toolbar;
  background: #1e1e2e;
  border-bottom: 1px solid #313244;
}

.tree-area {
  grid-area: tree;
  background: #181825;
  border-right: 1px solid #313244;
  overflow-y: auto;
}

.content-area {
  grid-area: content;
  background: #11111b;
  overflow: hidden;
}

.panel-area {
  grid-area: panel;
  background: #181825;
  border-left: 1px solid #313244;
  overflow-y: auto;
}

.log-area {
  grid-area: log;
  background: #1e1e2e;
  border-top: 1px solid #313244;
  overflow: hidden;
}
</style>

<script setup lang="ts">
import { ref, provide, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import Toolbar from './components/Toolbar.vue'
import ConnectionTree from './components/ConnectionTree.vue'
import DataTable from './components/DataTable.vue'
import ValuePanel from './components/ValuePanel.vue'
import LogPanel from './components/LogPanel.vue'
import { AppDialog } from 'shared-frontend'
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

onMounted(() => {
  unlistenConnState = EventsOn('master-connection-state', (event) => {
    const { ConnectionID: id, State: state } = event
    if (selectedConnectionId.value === id) {
      selectedConnectionState.value = state
    }
    refreshTree()
  })
  unlistenPollError = EventsOn('master-poll-error', () => {
    refreshTree()
  })

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
  if (regs.length === prev.length && regs.every((r, i) => r.Address === prev[i].Address)) {
    for (let i = 0; i < regs.length; i++) {
      prev[i].RawValue = regs[i].RawValue
      prev[i].DisplayValue = regs[i].DisplayValue
    }
    return
  }
  selectedRegisters.value = regs
}

function toggleLog() {
  logExpanded.value = !logExpanded.value
}

const workspaceEpoch = ref(0)
function workspaceReplaced() {
  selectedConnectionId.value = null; selectedConnectionState.value = 'Disconnected'
  selectedScanGroup.value = null; selectedRegisters.value = []; workspaceEpoch.value++; refreshTree()
}
provide('workspaceReplaced', workspaceReplaced)
</script>

<template>
  <div :class="['app-layout', { 'log-expanded': logExpanded }]">
    <header class="toolbar-area">
      <Toolbar />
    </header>

    <aside class="tree-area">
      <ConnectionTree :key="workspaceEpoch"
        @connection-select="handleConnectionSelect"
        @scan-group-select="handleScanGroupSelect"
      />
    </aside>
    <main class="content-area">
      <DataTable :key="workspaceEpoch"
        @register-select="handleRegisterSelect"
      />
    </main>
    <aside class="panel-area">
      <ValuePanel :key="workspaceEpoch" />
    </aside>

    <footer class="log-area">
      <LogPanel :key="workspaceEpoch" :expanded="logExpanded" @toggle="toggleLog" />
    </footer>
    <AppDialog />

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
  background: var(--c-crust);
  color: var(--c-text);
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
  background: var(--c-base);
  border-bottom: 1px solid var(--c-surface0);
}

.tree-area {
  grid-area: tree;
  background: var(--c-mantle);
  border-right: 1px solid var(--c-surface0);
  overflow-y: auto;
}

.content-area {
  grid-area: content;
  background: var(--c-crust);
  overflow: hidden;
}

.panel-area {
  grid-area: panel;
  background: var(--c-mantle);
  border-left: 1px solid var(--c-surface0);
  overflow-y: auto;
}

.log-area {
  grid-area: log;
  background: var(--c-base);
  border-top: 1px solid var(--c-surface0);
  overflow: hidden;
}
</style>

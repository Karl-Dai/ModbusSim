<script setup lang="ts">
import { ref, provide, onMounted, onBeforeUnmount, computed, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import Toolbar from './components/Toolbar.vue'
import ConnectionTree from './components/ConnectionTree.vue'
import RegisterTable from './components/RegisterTable.vue'
import ValuePanel from './components/ValuePanel.vue'
import LogPanel from './components/LogPanel.vue'
import { AppDialog, UpdateDialog, Splitter, useI18n } from 'shared-frontend'

const { t } = useI18n()
const tableRef = ref<InstanceType<typeof RegisterTable> | null>(null)
provide('registerActions', {
  importCsv: () => tableRef.value?.importCsv(), exportCsv: () => tableRef.value?.exportCsv(),
  template: () => tableRef.value?.template(), simulation: () => tableRef.value?.simulation(),
})
const windowWidth = ref(window.innerWidth)
const windowHeight = ref(window.innerHeight)
const workspaceEpoch = ref(0)
function storedSize(key: string, fallback: number, min: number, max: number) {
  try { const n = Number(localStorage.getItem(key)); if (n >= min && n <= max) return n } catch { /* optional storage */ }
  return fallback
}
const treeWidth = ref(storedSize('modbus.slave.treeWidth', 240, 180, 480))
const panelWidth = ref(storedSize('modbus.slave.panelWidth', 280, 220, 600))
const logHeight = ref(storedSize('modbus.slave.logHeight', 220, 80, 10000))
const treeMax = computed(() => Math.min(480, Math.max(180, windowWidth.value - 528)))
const panelMax = computed(() => Math.min(600, Math.max(220, windowWidth.value - treeWidth.value - 308)))
const logMax = computed(() => Math.max(80, Math.floor(windowHeight.value * .65)))
function resizeWorkspace() {
  windowWidth.value = window.innerWidth
  windowHeight.value = window.innerHeight
  treeWidth.value = Math.min(treeWidth.value, treeMax.value)
  panelWidth.value = Math.min(panelWidth.value, panelMax.value)
  logHeight.value = Math.min(logHeight.value, logMax.value)
}
for (const [key, value] of [['treeWidth', treeWidth], ['panelWidth', panelWidth], ['logHeight', logHeight]] as const) {
  watch(value, n => { try { localStorage.setItem(`modbus.slave.${key}`, String(n)) } catch { /* optional storage */ } })
}
watch(treeWidth, () => { panelWidth.value = Math.min(panelWidth.value, panelMax.value) })
function resetWorkspaceView() {
  selectedConnectionId.value = null
  selectedConnectionState.value = 'Stopped'
  selectedSlaveId.value = null
  selectedRegisterType.value = null
  selectedRegister.value = []
  workspaceEpoch.value++
  refreshTree()
}
provide('resetWorkspaceView', resetWorkspaceView)
// Shared state
const selectedConnectionId = ref<string | null>(null)
const selectedConnectionState = ref<string>('Stopped')
const selectedSlaveId = ref<number | null>(null)
const selectedRegisterType = ref<string | null>(null)
const selectedRegister = ref<{ address: number; register_type: string; value: number }[]>([])
const logExpanded = ref(false)

// Provide shared state to children
provide('selectedConnectionId', selectedConnectionId)
provide('selectedConnectionState', selectedConnectionState)
provide('selectedSlaveId', selectedSlaveId)
provide('selectedRegisterType', selectedRegisterType)
provide('selectedRegister', selectedRegister)

// Tree refresh trigger
const treeRefreshKey = ref(0)
provide('treeRefreshKey', treeRefreshKey)

function refreshTree() {
  treeRefreshKey.value++
}

provide('refreshTree', refreshTree)

// Register refresh trigger (for ValuePanel → RegisterTable sync)
const registerRefreshKey = ref(0)
provide('registerRefreshKey', registerRefreshKey)

function refreshRegisters() {
  registerRefreshKey.value++
}

provide('refreshRegisters', refreshRegisters)

function handleConnectionSelect(id: string, state: string) {
  selectedConnectionId.value = id
  selectedConnectionState.value = state
  selectedSlaveId.value = null
  selectedRegisterType.value = null
  selectedRegister.value = []
}

function handleSlaveSelect(connectionId: string, slaveId: number) {
  selectedConnectionId.value = connectionId
  selectedSlaveId.value = slaveId
  selectedRegisterType.value = null
  selectedRegister.value = []
}

function handleGroupSelect(connectionId: string, slaveId: number, regType: string) {
  selectedConnectionId.value = connectionId
  selectedSlaveId.value = slaveId
  selectedRegisterType.value = regType
  selectedRegister.value = []
}

function handleRegisterSelect(regs: { address: number; register_type: string; value: number }[]) {
  selectedRegister.value = regs
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

let updateTimer: ReturnType<typeof setTimeout>
onMounted(() => {
  resizeWorkspace()
  window.addEventListener('resize', resizeWorkspace)
  updateTimer = setTimeout(() => {
    checkUpdate(false).catch((e) => console.warn('auto update check failed', e))
  }, 2000)
})

onBeforeUnmount(() => { window.removeEventListener('resize', resizeWorkspace); clearTimeout(updateTimer) })
</script>

<template>
  <div class="app-layout" :style="{
    gridTemplateColumns: `${treeWidth}px 4px minmax(240px, 1fr) 4px ${panelWidth}px`,
    gridTemplateRows: `auto minmax(0, 1fr) ${logExpanded ? '4px' : '0px'} ${logExpanded ? logHeight + 'px' : '32px'}`,
  }">
    <header class="toolbar-area">
      <Toolbar />
    </header>

    <aside class="tree-area">
      <ConnectionTree :key="workspaceEpoch"
        @connection-select="handleConnectionSelect"
        @slave-select="handleSlaveSelect"
        @group-select="handleGroupSelect"
      />
    </aside>
    <Splitter class="splitter-tree" axis="x" v-model="treeWidth" :min="180" :max="treeMax" :label="t('parity.resizeTree')" />
    <main class="content-area">
      <RegisterTable ref="tableRef" :key="workspaceEpoch"
        @register-select="handleRegisterSelect"
      />
    </main>
    <Splitter class="splitter-panel" axis="x" v-model="panelWidth" :min="220" :max="panelMax" reverse :label="t('parity.resizePanel')" />
    <aside class="panel-area">
      <ValuePanel :key="workspaceEpoch" />
    </aside>

    <Splitter v-show="logExpanded" class="splitter-log" axis="y" v-model="logHeight" :min="80" :max="logMax" reverse :label="t('parity.resizeLog')" />
    <footer class="log-area">
      <LogPanel :key="workspaceEpoch" :expanded="logExpanded" @toggle="toggleLog" />
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
  background: var(--c-crust);
  color: var(--c-text);
}

.app-layout {
  display: grid;

  grid-template-areas:
    "toolbar toolbar toolbar toolbar toolbar"
    "tree split-tree content split-panel panel"
    "split-log split-log split-log split-log split-log"
    "log log log log log";
  height: 100vh;
  width: 100vw;
}

.splitter-tree { grid-area: split-tree; }
.splitter-panel { grid-area: split-panel; }
.splitter-log { grid-area: split-log; }
.app-layout > * { min-width: 0; min-height: 0; }

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

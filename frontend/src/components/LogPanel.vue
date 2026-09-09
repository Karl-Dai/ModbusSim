<script setup lang="ts">
import { saveExport } from '../utils/saveExport'
import { ref, inject, onMounted, watch, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { LogPanelShell, useFcLabel, type LogPanelDataSource, type LogEntry } from 'shared-frontend'

interface Props { expanded: boolean }
defineProps<Props>()
defineEmits<{ (e: 'toggle'): void }>()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const treeRefreshKey = inject<Ref<number>>('treeRefreshKey')!

const connections = ref<{ id: string; label: string }[]>([])

async function loadConnections() {
  try {
    const slaves = await invoke<{ id: string; bind_address: string; port: number }[]>('list_slave_connections')
    connections.value = slaves.map(s => ({ id: s.id, label: `${s.bind_address}:${s.port}` }))
  } catch { /* ignore */ }
}

onMounted(loadConnections)
watch(treeRefreshKey, loadConnections)

const source: LogPanelDataSource = {
  saveFile: saveExport,
  fetchLogs: (id) => invoke<LogEntry[]>('get_communication_logs', { connectionId: id }),
  clearLogs: (id) => invoke<void>('clear_communication_logs', { connectionId: id }),
  exportCsv: (id) => invoke<string>('export_logs_csv', { connectionId: id }),
}

const { fcLabel } = useFcLabel()
</script>

<template>
  <LogPanelShell
    :expanded="expanded"
    :connections="connections"
    :source="source"
    export-prefix="modbus_slave_log"
    :pinned-connection-id="selectedConnectionId"
    :fc-formatter="fcLabel"
    @toggle="$emit('toggle')"
  />
</template>

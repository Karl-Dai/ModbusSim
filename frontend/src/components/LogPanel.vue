<script setup lang="ts">
import { saveExport } from '../utils/saveExport'
import { ref, inject, onMounted, watch, type Ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { main } from '../../wailsjs/go/models'
import * as App from '../../wailsjs/go/main/App'
import { LogPanelShell, useFcLabel, type LogPanelDataSource, type LogFields } from 'shared-frontend'

interface Props { expanded: boolean }
defineProps<Props>()
defineEmits<{ (e: 'toggle'): void }>()

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const treeRefreshKey = inject<Ref<number>>('treeRefreshKey')!

const connections = ref<{ id: string; label: string }[]>([])

async function loadConnections() {
  try {
    const slaves = await App.ListConnections()
    connections.value = slaves.map(s => ({ id: s.ID, label: `${s.BindAddress}:${s.Port}` }))
  } catch { /* ignore */ }
}

onMounted(loadConnections)
watch(treeRefreshKey, loadConnections)

const source: LogPanelDataSource<main.LogEntry> = {
  saveFile: saveExport,
  subscribe: onAppend => EventsOn('log-appended', (event: { ConnectionID: string }) => onAppend(event.ConnectionID)),
  fetchLogs: (id) => App.Logs(id),
  clearLogs: (id) => App.ClearLogs(id),
  exportCsv: (id) => App.ExportLogsCSV(id),
}

const fields: LogFields<main.LogEntry> = { timestamp: row => row.Timestamp, direction: row => row.Direction, functionCode: row => row.FunctionCode, detail: row => row.Detail }
const { fcLabel } = useFcLabel()
</script>

<template>
  <LogPanelShell
    :expanded="expanded"
    :connections="connections"
    :source="source"
    :fields="fields"
    export-prefix="modbus_slave_log"
    :pinned-connection-id="selectedConnectionId"
    :fc-formatter="fcLabel"
    @toggle="$emit('toggle')"
  />
</template>

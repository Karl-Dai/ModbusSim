<script setup lang="ts">
import { ref, inject, onMounted, watch, type Ref } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { masterstate } from '../../wailsjs/go/models'
import { LogPanelShell, useFcLabel, type LogPanelDataSource, type LogFields } from 'shared-frontend'
defineProps<{ expanded: boolean }>()
defineEmits<{ (e: 'toggle'): void }>()
const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const treeRefreshKey = inject<Ref<number>>('treeRefreshKey')!
const connections = ref<{ id: string; label: string }[]>([])
let generation = 0
async function loadConnections() {
 const current = ++generation
 try { const rows = await App.ListConnections(); if (current === generation) connections.value = rows.map(c => ({ id: c.ID, label: `${c.TargetAddress}:${c.Port}` })) } catch { /* reconnect may replace the workspace */ }
}
onMounted(loadConnections)
watch(treeRefreshKey, loadConnections)
const source: LogPanelDataSource<masterstate.LogEntry> = {
 fetchLogs: App.Logs, clearLogs: App.ClearLogs, exportCsv: App.ExportLogsCSV,
 subscribe: callback => EventsOn('log-appended', (event: { ConnectionID: string }) => callback(event.ConnectionID)),
 saveFile: async (content, filename) => { const path = await App.SaveFileDialog(filename, [{ Name: 'CSV', Extensions: ['csv'] }]); if (path) await App.SaveTextExport(path, content) },
}
const fields: LogFields<masterstate.LogEntry> = { timestamp: r => r.Timestamp, direction: r => r.Direction, functionCode: r => r.FunctionCode, detail: r => r.Detail }
const { fcLabel } = useFcLabel()
</script>

<template>
  <LogPanelShell
    :expanded="expanded"
    :connections="connections"
    :source="source"
    :fields="fields"
    export-prefix="modbus_master_log"
    :pinned-connection-id="selectedConnectionId"
    :fc-formatter="fcLabel"
    @toggle="$emit('toggle')"
  />
</template>

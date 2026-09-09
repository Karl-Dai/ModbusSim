<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'shared-frontend'
import type { ClientInfo } from '../types/clients'
defineProps<{ label: string; clients: ClientInfo[]; error: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const dialog = ref<HTMLDialogElement>()
onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => dialog.value?.close())
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="clients-dialog" aria-labelledby="clients-title"
      @cancel.prevent="emit('close')" @click="event => { if (event.target === dialog) emit('close') }">
      <section class="clients-content">
        <header><h2 id="clients-title">{{ t('clients.title') }}</h2><p>{{ label }}</p></header>
        <p v-if="error" role="alert" class="error">{{ t('clients.unavailable') }}: {{ error }}</p>
        <template v-else>
          <p role="status" aria-atomic="true">{{ t('clients.count', { n: clients.length }) }}</p>
          <p v-if="!clients.length" class="empty">{{ t('clients.empty') }}</p>
          <div v-else class="table-scroll">
            <table>
              <thead><tr><th>{{ t('clients.peer') }}</th><th>{{ t('clients.since') }}</th><th>{{ t('clients.state') }}</th></tr></thead>
              <tbody><tr v-for="client in clients" :key="client.id">
                <td class="peer">{{ client.peer_address }}</td>
                <td>{{ new Date(client.connected_at).toLocaleString() }}</td>
                <td class="connected">{{ t('clients.connected') }}</td>
              </tr></tbody>
            </table>
          </div>
        </template>
        <footer><button autofocus @click="emit('close')">{{ t('common.close') }}</button></footer>
      </section>
    </dialog>
  </Teleport>
</template>

<style scoped>
.clients-dialog { position: fixed; inset: 0; margin: auto; width: min(620px, 90vw); padding: 0; max-height: 85vh; border: 1px solid var(--c-surface2); border-radius: 8px; background: var(--c-base); color: var(--c-text); box-shadow: 0 18px 50px #0006; font-size: 13px; }
.clients-dialog::backdrop { background: #0008; }
.clients-content { padding: 20px; }
h2 { margin: 0 0 8px; font-size: 16px; }
header p { color: var(--c-subtext1); overflow-wrap: anywhere; }
.empty { padding: 24px 0; color: var(--c-subtext1); }
.table-scroll { overflow: auto; max-height: 50vh; }
table { width: 100%; border-collapse: collapse; text-align: left; }
th, td { padding: 10px 8px; border-bottom: 1px solid var(--c-surface1); }
th { color: var(--c-subtext1); font-weight: 500; }
.peer { font-family: monospace; overflow-wrap: anywhere; }
.connected { color: var(--c-green); white-space: nowrap; }
.error { color: var(--c-red); overflow-wrap: anywhere; }
footer { display: flex; justify-content: flex-end; margin-top: 20px; }
button { padding: 8px 18px; background: var(--c-surface0); border: 1px solid var(--c-surface2); border-radius: 4px; color: var(--c-text); cursor: pointer; }
button:hover { background: var(--c-surface1); }
button:focus-visible { outline: 2px solid var(--c-blue); outline-offset: 2px; }
</style>

<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useI18n, showAlert } from 'shared-frontend'

const { t } = useI18n()

interface Props { show: boolean; connectionId: string | null }
const props = defineProps<Props>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created'): void }>()

const slaveId = ref('2')
const slaveName = ref('')
const initMode = ref('zero')

watch(() => props.show, (visible) => {
  if (!visible) return
  slaveId.value = '2'
  slaveName.value = ''
  initMode.value = 'zero'
})

async function submit() {
  if (!props.connectionId) return
  const id = Number(slaveId.value)
  if (!id || id < 1 || id > 247) {
    await showAlert(t('errors.invalidSlaveId'))
    return
  }
  emit('close')
  try {
    await invoke('add_slave_device', {
      request: { connection_id: props.connectionId, slave_id: id, name: slaveName.value.trim(), init_mode: initMode.value }
    })
    emit('created')
  } catch (e) {
    await showAlert(String(e))
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" v-modal="() => emit('close')" class="modal-overlay" @click.self="emit('close')">
      <div class="modal-box">
        <div class="modal-title">{{ t('toolbar.newSlave') }}</div>
        <div class="modal-field">
          <label>{{ t('dialog.slaveId') }}</label>
          <input v-model="slaveId" type="number" min="1" max="247" @keyup.enter="submit" />
        </div>
        <div class="modal-field">
          <label>{{ t('dialog.slaveName') }}</label>
          <input v-model="slaveName" type="text" :placeholder="t('dialog.slaveNamePlaceholder')" @keyup.enter="submit" />
        </div>
        <div class="modal-field">
          <label>{{ t('dialog.initValue') }}</label>
          <div class="radio-group">
            <label class="radio-label"><input type="radio" v-model="initMode" value="zero" /> {{ t('dialog.initZero') }}</label>
            <label class="radio-label"><input type="radio" v-model="initMode" value="random" /> {{ t('dialog.initRandom') }}</label>
          </div>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button class="modal-btn confirm" @click="submit">{{ t('common.ok') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; padding: 20px; min-width: 300px; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 14px; font-weight: 600; color: var(--c-text); margin-bottom: 16px; }
.modal-field { margin-bottom: 14px; }
.modal-field label { display: block; font-size: 12px; color: var(--c-subtext0); margin-bottom: 6px; }
.modal-field input[type="number"], .modal-field input[type="text"] { width: 100%; padding: 6px 10px; background: var(--c-surface0); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 13px; outline: none; box-sizing: border-box; }
.modal-field input:focus { border-color: var(--c-blue); }
.radio-group { display: flex; gap: 16px; }
.radio-label { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--c-text); cursor: pointer; }
.radio-label input[type="radio"] { accent-color: var(--c-blue); }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
.modal-btn { padding: 6px 16px; border: none; border-radius: 4px; font-size: 12px; cursor: pointer; }
.modal-btn.cancel { background: var(--c-surface0); color: var(--c-subtext0); }
.modal-btn.cancel:hover { background: var(--c-surface1); }
.modal-btn.confirm { background: var(--c-blue); color: var(--c-base); font-weight: 600; }
.modal-btn.confirm:hover { background: var(--c-sapphire); }
</style>

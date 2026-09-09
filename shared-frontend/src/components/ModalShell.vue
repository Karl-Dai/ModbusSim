<script setup lang="ts">
import { vModal } from '../directives/modal'
import { useI18n } from '../i18n'
defineProps<{ title: string; subtitle?: string; wide?: boolean; busy?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
</script>
<template>
  <Teleport to="body">
    <div v-modal="() => { if (!busy) emit('close') }" class="common-modal-backdrop dialog-blur" :aria-label="title" @mousedown.self="!busy && emit('close')">
      <section class="common-modal" :class="{ wide }">
        <header><div><h2>{{ title }}</h2><p v-if="subtitle">{{ subtitle }}</p></div><button :disabled="busy" :aria-label="t('common.close')" @click="emit('close')">×</button></header>
        <div class="common-modal-body"><slot /></div>
        <footer><slot name="actions"><button class="primary" :disabled="busy" @click="emit('close')">{{ t('common.close') }}</button></slot></footer>
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.common-modal-backdrop { position: fixed; inset: 0; display: flex; align-items: center; justify-content: center; background: rgb(0 0 0 / 55%); z-index: 1500; }
.common-modal { width: min(560px, calc(100vw - 32px)); max-height: calc(100vh - 48px); display: flex; flex-direction: column; background: var(--c-base); color: var(--c-text); border: 1px solid var(--c-surface1); border-radius: 9px; box-shadow: 0 18px 50px #0006; font-size: 13px; }
.common-modal.wide { width: min(820px, calc(100vw - 32px)); }
header { display: flex; justify-content: space-between; gap: 12px; padding: 16px 20px; border-bottom: 1px solid var(--c-surface0); }
h2 { font-size: 16px; font-weight: 600; margin: 0; } p { margin-top: 6px; color: var(--c-subtext0); overflow-wrap: anywhere; }
header button { background: transparent; border: 0; font-size: 20px; color: var(--c-subtext1); cursor: pointer; }
.common-modal-body { padding: 20px; overflow: auto; min-height: 0; }
footer { padding: 12px 20px; border-top: 1px solid var(--c-surface0); display: flex; justify-content: flex-end; gap: 8px; }
footer :deep(button) { padding: 7px 16px; color: var(--c-text); background: var(--c-surface0); border: 1px solid var(--c-surface1); border-radius: 5px; cursor: pointer; }
footer :deep(.primary) { background: var(--c-blue); color: var(--c-base); }
.common-modal-body :deep(label) { display: block; margin-bottom: 6px; color: var(--c-subtext1); }
.common-modal-body :deep(input:not([type=checkbox])), .common-modal-body :deep(select), .common-modal-body :deep(textarea) { width: 100%; padding: 8px 10px; margin-bottom: 12px; color: var(--c-text); background: var(--c-mantle); border: 1px solid var(--c-surface1); border-radius: 5px; font: inherit; }
.common-modal-body :deep(.error) { color: var(--c-red); white-space: pre-wrap; overflow-wrap: anywhere; }
.common-modal-body :deep(.hint) { color: var(--c-subtext0); line-height: 1.6; margin: 8px 0; }
</style>

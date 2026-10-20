<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'

const props = defineProps<{
  modelValue: string
  placeholder?: string
  browseLabel?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'browse'): void
}>()

const editing = ref(false)
const draft = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

// Long absolute paths are clipped by the browser from the right, which hides
// the only part that identifies the file. Collapse the head instead and keep
// the last path segments (at least the file name) visible.
const displayPath = computed(() => tailPath(props.modelValue))

function tailPath(path: string, budget = 36): string {
  if (!path) return ''
  if (path.length <= budget) return path
  const tail = path.slice(-budget)
  const cut = tail.search(/[\\/]/)
  return `…${cut > 0 ? tail.slice(cut) : tail}`
}

async function startEdit() {
  draft.value = props.modelValue
  editing.value = true
  await nextTick()
  const el = inputEl.value
  if (!el) return
  el.focus()
  const end = el.value.length
  el.setSelectionRange(end, end)
  el.scrollLeft = el.scrollWidth
}

function commit() {
  if (!editing.value) return
  editing.value = false
  const next = draft.value.trim()
  if (next !== props.modelValue) emit('update:modelValue', next)
}

function cancel() {
  editing.value = false
}
</script>

<template>
  <div class="path-field">
    <input
      v-if="editing"
      ref="inputEl"
      v-model="draft"
      class="path-input"
      type="text"
      spellcheck="false"
      autocomplete="off"
      :placeholder="placeholder"
      :title="modelValue || undefined"
      @keydown.enter.prevent="commit"
      @keydown.escape.prevent="cancel"
      @blur="commit"
    >
    <button
      v-else
      type="button"
      class="path-display"
      :class="{ 'is-empty': !modelValue }"
      :title="modelValue || placeholder"
      :aria-label="modelValue || placeholder"
      @click="startEdit"
    >
      <span class="path-text">{{ modelValue ? displayPath : placeholder }}</span>
    </button>
    <button type="button" class="path-browse" :aria-label="browseLabel" @click="emit('browse')">...</button>
  </div>
</template>

<style scoped>
.path-field { display: flex; gap: 4px; min-width: 0; }

.path-display,
.path-input {
  box-sizing: border-box;
  flex: 1;
  min-width: 0;
  padding: 6px 10px;
  background: var(--c-control-bg);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  font: inherit;
  font-size: 13px;
  line-height: 16px;
}

.path-display { display: block; overflow: hidden; text-align: left; white-space: nowrap; cursor: text; }
.path-display:hover { border-color: var(--c-surface2); }
.path-display:focus-visible,
.path-input:focus { outline: none; border-color: var(--c-blue); }
.path-display.is-empty { color: var(--c-subtext0); }
.path-text { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.path-browse {
  box-sizing: border-box;
  align-self: stretch;
  flex: none;
  padding: 0 8px;
  background: var(--c-control-bg);
  border: 1px solid var(--c-surface1);
  border-radius: 4px;
  color: var(--c-text);
  font: inherit;
  font-size: 14px;
  cursor: pointer;
}
.path-browse:hover { background: var(--c-surface1); }
.path-browse:focus-visible { outline: none; border-color: var(--c-blue); }
</style>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { ToolbarAction } from '../types/toolbar'

const props = defineProps<{
  id: string
  label: string
  open: boolean
  disabled?: boolean
  tabindex?: number
  items: ToolbarAction[]
}>()
const emit = defineEmits<{
  toggle: []; close: []; focus: []; hover: []
  navigate: [direction: 'next' | 'previous' | 'first' | 'last']
}>()
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLDivElement | null>(null)
const position = ref({ left: '8px', top: '42px' })

function positionMenu() {
  if (!props.open || !trigger.value || !menu.value) return
  const anchor = trigger.value.getBoundingClientRect()
  const bounds = menu.value.getBoundingClientRect()
  position.value = {
    left: `${Math.max(8, Math.min(anchor.left, window.innerWidth - bounds.width - 8))}px`,
    top: `${Math.max(8, Math.min(anchor.bottom + 4, window.innerHeight - bounds.height - 8))}px`,
  }
}
function menuItems() {
  return Array.from(menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
}
function close(restoreFocus = false) {
  emit('close')
  if (restoreFocus) trigger.value?.focus()
}
let focusLast = false
function openFromKeyboard(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); close(); return }
  if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) {
    event.preventDefault()
    emit('navigate', event.key === 'Home' ? 'first' : event.key === 'End' ? 'last'
      : event.key === 'ArrowRight' ? 'next' : 'previous')
    return
  }
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  focusLast = event.key === 'ArrowUp'
  if (!props.open) emit('toggle')
  else {
    const items = menuItems()
    items[focusLast ? items.length - 1 : 0]?.focus()
    focusLast = false
  }
}
function menuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); close(true); return }
  if (event.key === 'Tab') { close(true); return }
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault()
    emit('navigate', event.key === 'ArrowRight' ? 'next' : 'previous')
    return
  }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const items = menuItems()
  if (!items.length) return
  const current = items.indexOf(document.activeElement as HTMLButtonElement)
  const index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
    : (current + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length
  items[index]?.focus()
}
function choose(item: typeof props.items[number]) {
  if (item.disabled || item.busy || props.disabled) return
  close(true)
  void item.action()
}
function outside(event: Event) {
  if (!props.open) return
  const target = event.target as Node
  if (!trigger.value?.contains(target) && !menu.value?.contains(target)) close()
}
watch(() => props.open, async open => {
  if (open) {
    await nextTick()
    if (!props.open) return
    positionMenu()
    const items = menuItems()
    const target = items[focusLast ? items.length - 1 : 0]
    if (target) target.focus()
    else menu.value?.focus()
    focusLast = false
  }
})
watch(() => props.disabled, disabled => { if (disabled && props.open) close() })
onMounted(() => {
  document.addEventListener('pointerdown', outside)
  document.addEventListener('focusin', outside)
  window.addEventListener('resize', positionMenu)
  window.addEventListener('scroll', positionMenu, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('focusin', outside)
  window.removeEventListener('resize', positionMenu)
  window.removeEventListener('scroll', positionMenu, true)
})
</script>

<template>
  <button ref="trigger" type="button" class="toolbar-btn menu-trigger" :id="`${id}-trigger`"
    role="menuitem" :tabindex="tabindex ?? 0" @focus="emit('focus')" @pointerenter="emit('hover')"
    :data-testid="`menu-${id}`" :disabled="disabled" aria-haspopup="menu" :aria-expanded="open"
    :aria-controls="`${id}-menu`" @click="emit('toggle')" @keydown="openFromKeyboard">
    {{ label }}<svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true"><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.5" /></svg>
  </button>
  <Teleport to="body">
    <div v-show="open" ref="menu" class="toolbar-menu" :id="`${id}-menu`" role="menu"
      :aria-labelledby="`${id}-trigger`" :style="position" tabindex="-1" @keydown="menuKeydown">
      <template v-for="item in items" :key="item.id">
        <div v-if="item.separatorBefore" class="menu-separator" role="separator" />
        <button type="button" role="menuitem" tabindex="-1" :class="item.tone && `menu-${item.tone}`"
          :data-testid="item.id" :aria-disabled="!!(disabled || item.disabled || item.busy)" :title="item.title"
          :aria-busy="item.busy" @click="choose(item)">
          <span>{{ item.label }}</span><kbd v-if="item.shortcut">{{ item.shortcut }}</kbd>
        </button>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
.menu-trigger { gap: 5px; }
.menu-trigger[aria-expanded=true] { background: var(--c-surface0); }
.menu-trigger:focus-visible { outline: 2px solid var(--c-blue); outline-offset: -2px; }
.toolbar-menu {
  position: fixed; z-index: 900; min-width: 220px; max-width: calc(100vw - 16px);
  max-height: calc(100vh - 16px); overflow-y: auto; padding: 4px;
  border: 1px solid var(--c-surface1); border-radius: 6px;
  background: var(--c-base); color: var(--c-text); box-shadow: 0 6px 18px rgb(0 0 0 / 28%);
}
.toolbar-menu button {
  display: flex; align-items: center; justify-content: space-between; gap: 24px;
  width: 100%; text-align: left; background: transparent; color: inherit;
  border: 0; border-radius: 3px; min-height: 32px; padding: 7px 10px; font: inherit; font-size: 12px; cursor: pointer;
}
.toolbar-menu button:hover:not([aria-disabled=true]), .toolbar-menu button:focus-visible { background: var(--c-surface0); }
.toolbar-menu button:focus-visible { outline: 2px solid var(--c-blue); outline-offset: -2px; }
.toolbar-menu button[aria-disabled=true] { opacity: .45; cursor: default; }
.toolbar-menu .menu-danger { color: var(--c-red); }
.toolbar-menu kbd { color: var(--c-subtext0); font: inherit; font-size: 11px; white-space: nowrap; }
.menu-separator { height: 1px; margin: 4px 6px; background: var(--c-surface0); }
</style>

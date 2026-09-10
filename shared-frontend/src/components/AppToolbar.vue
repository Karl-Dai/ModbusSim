<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '../i18n'
import type { ToolbarAction, ToolbarMenuDefinition } from '../types/toolbar'
import ToolbarMenu from './ToolbarMenu.vue'
import LangToggle from './LangToggle.vue'
import VersionBadge from './VersionBadge.vue'

const props = defineProps<{
  title: string
  menus: ToolbarMenuDefinition[]
  actions: ToolbarAction[]
  busy?: boolean
  projectPath?: string | null
  status?: string
}>()
const { t } = useI18n()
const menubar = ref<HTMLDivElement | null>(null)
const openMenu = ref<string | null>(null)
const activeMenu = ref(props.menus[0]?.id)
const projectName = computed(() => props.projectPath?.split(/[/\\]/).pop() || t('toolbar.untitledProject'))
const statusLabel = computed(() => props.status || (props.busy ? t('toolbar.working') : ''))
const icons: Record<NonNullable<ToolbarAction['icon']>, string> = {
  add: 'M8 3v10M3 8h10',
  save: 'M3 2.5h8l2 2V13H3zM5 2.5V6h5V2.5M5 13V9h6v4',
  play: 'm5 3 7 5-7 5z',
  stop: 'M4 4h8v8H4z',
  scan: 'M6.5 2.5a4 4 0 1 0 0 8 4 4 0 0 0 0-8M10 10l3.5 3.5',
  write: 'm10 2 4 4-7.5 7.5-4.5.5.5-4.5zM8.5 3.5l4 4',
  device: 'M3 2.5h10v11H3zM5.5 5.5h5M5.5 8h5M5.5 10.5h2',
}

function closeMenu(id: string) {
  if (openMenu.value === id) openMenu.value = null
}
function toggleMenu(id: string) {
  activeMenu.value = id
  openMenu.value = openMenu.value === id ? null : id
}
function switchMenu(id: string, direction: 'next' | 'previous' | 'first' | 'last') {
  const index = props.menus.findIndex(menu => menu.id === id)
  const next = direction === 'first' ? 0 : direction === 'last' ? props.menus.length - 1
    : (index + (direction === 'next' ? 1 : -1) + props.menus.length) % props.menus.length
  const target = props.menus[next]
  if (!target || props.busy) return
  const wasOpen = openMenu.value !== null
  activeMenu.value = target.id
  menubar.value?.querySelectorAll<HTMLButtonElement>('.menu-trigger')[next]?.focus()
  if (wasOpen) openMenu.value = target.id
}
function hoverMenu(id: string) {
  if (openMenu.value && openMenu.value !== id && !props.busy) {
    activeMenu.value = id
    openMenu.value = id
  }
}
function choose(action: ToolbarAction) {
  if (props.busy || action.disabled || action.busy) return
  openMenu.value = null
  void action.action()
}
watch(() => props.busy, busy => { if (busy) openMenu.value = null })
</script>

<template>
  <div class="app-toolbar" :aria-busy="busy">
    <div class="toolbar-menu-row">
      <div ref="menubar" class="toolbar-menubar" role="menubar" :aria-label="t('toolbar.mainMenu')">
        <ToolbarMenu v-for="menu in menus" :key="menu.id" v-bind="menu"
          :open="openMenu === menu.id" :disabled="busy" :tabindex="activeMenu === menu.id ? 0 : -1"
          @toggle="toggleMenu(menu.id)"
          @close="closeMenu(menu.id)" @focus="activeMenu = menu.id" @hover="hoverMenu(menu.id)"
          @navigate="switchMenu(menu.id, $event)" />
      </div>
      <div class="toolbar-project" :title="projectPath || projectName">
        <span class="toolbar-app-name">{{ title }}</span>
        <span class="toolbar-project-name">{{ projectName }}</span>
      </div>
      <div class="toolbar-aside"><LangToggle /><VersionBadge /></div>
    </div>
    <div class="toolbar-action-row" role="group" :aria-label="t('toolbar.quickActions')">
      <div class="toolbar-quick-actions">
        <template v-for="action in actions" :key="action.id">
          <span v-if="action.separatorBefore" class="toolbar-divider" aria-hidden="true" />
          <button type="button" class="toolbar-btn" :class="action.tone && `btn-${action.tone}`"
            :data-testid="`quick-${action.id}`" :disabled="busy || action.disabled || action.busy"
            :aria-busy="action.busy" :title="action.title || action.label" @click="choose(action)">
            <svg v-if="action.icon" viewBox="0 0 16 16" width="15" height="15" fill="none"
              stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path :d="icons[action.icon]" />
            </svg>
            {{ action.label }}
          </button>
        </template>
      </div>
      <span class="toolbar-status" role="status" :title="statusLabel">{{ statusLabel }}</span>
    </div>
  </div>
</template>

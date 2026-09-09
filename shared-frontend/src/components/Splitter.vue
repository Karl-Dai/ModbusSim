<script setup lang="ts">
import { onBeforeUnmount } from 'vue'

const props = defineProps<{
  modelValue: number
  axis: 'x' | 'y'
  min: number
  max: number
  reverse?: boolean
  label?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: number): void
}>()

let startValue = 0
let startPosition = 0

function onMouseDown(event: MouseEvent) {
  onMouseUp()
  event.preventDefault()
  ;(event.currentTarget as HTMLElement).focus()
  startValue = props.modelValue
  startPosition = props.axis === 'x' ? event.clientX : event.clientY
  document.addEventListener('mousemove', onMouseMove)
  document.addEventListener('mouseup', onMouseUp)
  document.body.style.userSelect = 'none'
  document.body.style.cursor = props.axis === 'x' ? 'col-resize' : 'row-resize'
}

function onMouseMove(event: MouseEvent) {
  const currentPosition = props.axis === 'x' ? event.clientX : event.clientY
  let delta = currentPosition - startPosition
  if (props.reverse) delta = -delta
  emit('update:modelValue', Math.min(props.max, Math.max(props.min, startValue + delta)))
}

function onMouseUp() {
  document.removeEventListener('mousemove', onMouseMove)
  document.removeEventListener('mouseup', onMouseUp)
  document.body.style.userSelect = ''
  document.body.style.cursor = ''
}

function onKeydown(event: KeyboardEvent) {
  const backward = props.axis === 'x' ? 'ArrowLeft' : 'ArrowUp'
  const forward = props.axis === 'x' ? 'ArrowRight' : 'ArrowDown'
  if (![backward, forward, 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const delta = (event.key === backward ? -1 : 1) * (props.reverse ? -1 : 1) * (event.shiftKey ? 40 : 10)
  const value = event.key === 'Home' ? props.min : event.key === 'End' ? props.max : props.modelValue + delta
  emit('update:modelValue', Math.min(props.max, Math.max(props.min, value)))
}
onBeforeUnmount(onMouseUp)
</script>

<template>
  <div
    :class="['splitter', `axis-${axis}`]"
    role="separator"
    tabindex="0"
    :aria-label="label"
    :aria-valuenow="Math.round(modelValue)"
    :aria-valuemin="min"
    :aria-valuemax="max"
    @keydown="onKeydown"
    :aria-orientation="axis === 'x' ? 'vertical' : 'horizontal'"
    @mousedown="onMouseDown"
  />
</template>

<style scoped>
.splitter {
  position: relative;
  z-index: 5;
  background: transparent;
}

.axis-x {
  width: 4px;
  height: 100%;
  cursor: col-resize;
}

.axis-y {
  width: 100%;
  height: 4px;
  cursor: row-resize;
}

.splitter::before {
  content: '';
  position: absolute;
  background: var(--c-surface0);
  transition: background 0.12s, transform 0.12s;
}

.axis-x::before {
  top: 0;
  bottom: 0;
  left: 1px;
  width: 1px;
}

.axis-y::before {
  right: 0;
  left: 0;
  top: 1px;
  height: 1px;
}

.splitter:hover::before,
.splitter:active::before {
  background: var(--c-blue);
}

.axis-x:hover::before,
.axis-x:active::before {
  left: 1px;
  width: 2px;
}

.axis-y:hover::before,
.axis-y:active::before {
  top: 1px;
  height: 2px;
}
</style>

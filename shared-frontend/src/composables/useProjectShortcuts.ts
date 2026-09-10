import { onBeforeUnmount, onMounted } from 'vue'

export function useProjectShortcuts(actions: {
  open: () => unknown
  save: () => unknown
  saveAs: () => unknown
}) {
  const modifier = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl+'

  function keyboard(event: KeyboardEvent) {
    if (event.defaultPrevented || event.repeat || event.isComposing || event.altKey
      || !(event.metaKey || event.ctrlKey) || document.querySelector('[aria-modal="true"]')) return
    const key = event.key.toLowerCase()
    if (key === 's') {
      event.preventDefault()
      void (event.shiftKey ? actions.saveAs() : actions.save())
    } else if (key === 'o' && !event.shiftKey) {
      event.preventDefault()
      void actions.open()
    }
  }

  onMounted(() => window.addEventListener('keydown', keyboard))
  onBeforeUnmount(() => window.removeEventListener('keydown', keyboard))

  return { open: `${modifier}O`, save: `${modifier}S`, saveAs: `${modifier}Shift+S` }
}

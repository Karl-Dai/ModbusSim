import type { ObjectDirective } from 'vue'
const stack: HTMLElement[] = []
const cleanup = new WeakMap<HTMLElement, () => void>()
/** Shared focus lifecycle for the simulator's existing teleported dialogs. */
export const vModal: ObjectDirective<HTMLElement, () => void> = {
  mounted(el, binding) {
    const previous = document.activeElement as HTMLElement | null
    stack.push(el)
    el.setAttribute('role', 'dialog')
    el.setAttribute('aria-modal', 'true')
    el.tabIndex = -1
    const focusables = () => Array.from(el.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex="0"]')).filter(node => node.getClientRects().length)
    const keydown = (event: KeyboardEvent) => {
      if (stack.at(-1) !== el) return
      if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); binding.value(); return }
      if (event.key !== 'Tab') return
      const nodes = focusables(), first = nodes[0], last = nodes.at(-1)
      if (!first) { event.preventDefault(); el.focus(); return }
      if (event.shiftKey && (document.activeElement === first || !el.contains(document.activeElement))) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && (document.activeElement === last || !el.contains(document.activeElement))) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', keydown, true)
    queueMicrotask(() => { if (el.isConnected && !el.contains(document.activeElement)) (focusables()[0] ?? el).focus() })
    cleanup.set(el, () => {
      document.removeEventListener('keydown', keydown, true)
      const index = stack.indexOf(el); if (index >= 0) stack.splice(index, 1)
      if (previous?.isConnected) previous.focus()
    })
  },
  unmounted(el) { cleanup.get(el)?.(); cleanup.delete(el) },
}

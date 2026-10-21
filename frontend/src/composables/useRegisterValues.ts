import { ref, onUnmounted, watch, type Ref } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
type UnlistenFn = () => void
import type { RegisterDef } from './useRegisterFormat'

const CHANGE_HIGHLIGHT_MS = 1500

interface RegisterChangePayload {
  SlaveID: number
  RegisterType: string
  Address: number
  Value: number
}

interface RegisterValueEvent {
  ConnectionID: string
  Changes: RegisterChangePayload[]
}

/**
 * Loads a slave's register definitions and values, then keeps them live by
 * listening to backend `register-value-changed` events (emitted both by the
 * bound WriteRegister method and by external master writes routed through
 * the slave protocol layer). Bulk refreshes still happen on demand via
 * `refreshValues`.
 */
export function useRegisterValues(
  selectedConnectionId: Ref<string | null>,
  selectedSlaveId: Ref<number | null>,
  selectedRegisterType: Ref<string | null>,
) {
  const registers = ref<RegisterDef[]>([])
  const registerValues = ref<Record<string, number>>({})
  const isLoading = ref(false)
  const error = ref<string | null>(null)
  const changedKeys = ref<Set<string>>(new Set())

  const changeTimers = new Map<string, number>()
  let loadSeq = 0
  // Keys that received a push-event during an in-flight load. The load's
  // snapshot must not clobber these (the event is fresher than the snapshot).
  let loadDirtyKeys: Set<string> | null = null
  let unlisten: UnlistenFn | null = null

  function markChanged(key: string) {
    changedKeys.value.add(key)
    const prev = changeTimers.get(key)
    if (prev) clearTimeout(prev)
    changeTimers.set(key, window.setTimeout(() => {
      changedKeys.value.delete(key)
      changeTimers.delete(key)
    }, CHANGE_HIGHLIGHT_MS))
  }

  function clearChangeTimers() {
    for (const t of changeTimers.values()) clearTimeout(t)
    changeTimers.clear()
    changedKeys.value.clear()
  }

  async function loadRegisters() {
    if (!selectedConnectionId.value || selectedSlaveId.value === null) {
      registers.value = []
      registerValues.value = {}
      return
    }
    const seq = ++loadSeq
    const connId = selectedConnectionId.value
    const slaveId = selectedSlaveId.value
    isLoading.value = true
    error.value = null
    loadDirtyKeys = new Set()
    try {
      const defs = await App.ListRegisters(connId, slaveId, selectedRegisterType.value ?? '')
      if (seq !== loadSeq) return
      registers.value = defs

      const types = Array.from(new Set(defs.map(d => d.RegisterType)))
      const values: Record<string, number> = {}
      await Promise.all(types.map(async (rt) => {
        const rows = await App.ReadRegistersBulk(connId, slaveId, rt)
        for (const r of rows) values[`${rt}-${r.Address}`] = r.Value
      }))
      if (seq !== loadSeq) return
      const dirty = loadDirtyKeys
      const merged: Record<string, number> = { ...values }
      if (dirty) {
        for (const k of dirty) {
          if (k in registerValues.value) merged[k] = registerValues.value[k]
        }
      }
      registerValues.value = merged
    } catch (e) {
      if (seq === loadSeq) error.value = String(e)
    } finally {
      if (seq === loadSeq) {
        isLoading.value = false
        loadDirtyKeys = null
      }
    }
  }

  /** Bulk re-read; used after operations the backend doesn't emit events for
   *  (random mutation, batch import). Caller bumps a refresh key. */
  async function refreshValues() {
    if (!selectedConnectionId.value || selectedSlaveId.value === null) return
    if (registers.value.length === 0) return
    const connId = selectedConnectionId.value
    const slaveId = selectedSlaveId.value
    const seq = loadSeq
    const types = Array.from(new Set(registers.value.map(r => r.RegisterType)))
    try {
      const results = await Promise.all(types.map(rt =>
        App.ReadRegistersBulk(connId, slaveId, rt).then(rows => ({ rt, rows }))
      ))
      if (seq !== loadSeq || connId !== selectedConnectionId.value || slaveId !== selectedSlaveId.value) return
      for (const { rt, rows } of results) {
        for (const r of rows) {
          const k = `${rt}-${r.Address}`
          if (registerValues.value[k] !== r.Value) {
            registerValues.value[k] = r.Value
            markChanged(k)
          }
        }
      }
    } catch { /* skip */ }
  }

  function applyEvent(payload: RegisterValueEvent) {
    if (payload.ConnectionID !== selectedConnectionId.value) return
    const slaveId = selectedSlaveId.value
    for (const change of payload.Changes) {
      if (change.SlaveID !== slaveId) continue
      const key = `${change.RegisterType}-${change.Address}`
      if (loadDirtyKeys) loadDirtyKeys.add(key)
      if (registerValues.value[key] !== change.Value) {
        registerValues.value[key] = change.Value
        markChanged(key)
      }
    }
  }

  async function startListening() {
    if (unlisten) return
    unlisten = EventsOn('register-value-changed', (event: RegisterValueEvent) => applyEvent(event))
  }

  function stopListening() {
    if (unlisten) {
      unlisten()
      unlisten = null
    }
  }

  startListening()

  watch([selectedConnectionId, selectedSlaveId, selectedRegisterType], clearChangeTimers)

  function getValue(rt: string, address: number): number {
    return registerValues.value[`${rt}-${address}`] ?? 0
  }

  onUnmounted(() => {
    stopListening()
    clearChangeTimers()
  })

  return {
    registers,
    registerValues,
    isLoading,
    error,
    changedKeys,
    loadRegisters,
    refreshValues,
    clearChangeTimers,
    getValue,
  }
}

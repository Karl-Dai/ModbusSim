import { ref, onUnmounted, watch, type Ref } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { listen, type UnlistenFn } from '@tauri-apps/api/event'
import type { RegisterDef } from './useRegisterFormat'

const CHANGE_HIGHLIGHT_MS = 1500

interface RowValue { address: number; value: number }

interface RegisterChangePayload {
  slave_id: number
  register_type: string
  address: number
  value: number
}

interface RegisterValueEvent {
  connection_id: string
  changes: RegisterChangePayload[]
}

/**
 * Loads a slave's register definitions and values, then keeps them live by
 * listening to backend `register-value-changed` events (emitted both by the
 * Tauri write_register command and by external master writes routed through
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
      const defs = await invoke<RegisterDef[]>('list_registers', {
        connectionId: connId,
        slaveId,
        registerType: selectedRegisterType.value,
      })
      if (seq !== loadSeq) return
      registers.value = defs.map(def => ({ ...def, data_type: def.data_type.replace(/^u_int/, 'uint') }))

      const types = Array.from(new Set(defs.map(d => d.register_type)))
      const values: Record<string, number> = {}
      await Promise.all(types.map(async (rt) => {
        const rows = await invoke<RowValue[]>('read_registers_bulk', {
          connectionId: connId, slaveId, registerType: rt,
        })
        for (const r of rows) values[`${rt}-${r.address}`] = r.value
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
    const types = Array.from(new Set(registers.value.map(r => r.register_type)))
    try {
      const results = await Promise.all(types.map(rt =>
        invoke<RowValue[]>('read_registers_bulk', {
          connectionId: connId,
          slaveId,
          registerType: rt,
        }).then(rows => ({ rt, rows }))
      ))
      if (seq !== loadSeq || connId !== selectedConnectionId.value || slaveId !== selectedSlaveId.value) return
      for (const { rt, rows } of results) {
        for (const r of rows) {
          const k = `${rt}-${r.address}`
          if (registerValues.value[k] !== r.value) {
            registerValues.value[k] = r.value
            markChanged(k)
          }
        }
      }
    } catch { /* skip */ }
  }

  function applyEvent(payload: RegisterValueEvent) {
    if (payload.connection_id !== selectedConnectionId.value) return
    const slaveId = selectedSlaveId.value
    for (const change of payload.changes) {
      if (change.slave_id !== slaveId) continue
      const key = `${change.register_type}-${change.address}`
      if (loadDirtyKeys) loadDirtyKeys.add(key)
      if (registerValues.value[key] !== change.value) {
        registerValues.value[key] = change.value
        markChanged(key)
      }
    }
  }

  async function startListening() {
    if (unlisten) return
    unlisten = await listen<RegisterValueEvent>('register-value-changed', (e) => applyEvent(e.payload))
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

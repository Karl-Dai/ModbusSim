<script setup lang="ts">
import { inject, computed, ref, watch, nextTick, type Ref, type Directive } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { swapBytes16, use16BitFormat, use32BitFormat, use64BitFormat, formatAddress, useI18n, useFcLabel, showAlert } from 'shared-frontend'

const { t } = useI18n()
const { registerTypeLabel } = useFcLabel()

const vFocus: Directive<HTMLInputElement> = {
  mounted(el) {
    el.focus()
    el.select()
  }
}

interface SelectedReg {
  address: number
  register_type: string
  value: number
}

const selectedConnectionId = inject<Ref<string | null>>('selectedConnectionId')!
const selectedSlaveId = inject<Ref<number | null>>('selectedSlaveId')!
const selectedRegister = inject<Ref<SelectedReg[]>>('selectedRegister')!
const addrMode = inject<Ref<'hex' | 'dec'>>('addrMode', ref('hex') as any)
const refreshRegisters = inject<() => void>('refreshRegisters')!

const hasSelection = computed(() => selectedRegister.value.length > 0)

const sortedRegs = computed(() => {
  return [...selectedRegister.value].sort((a, b) => a.address - b.address)
})

const firstReg = computed(() => sortedRegs.value[0] ?? null)

const isBoolType = computed(() => {
  if (!firstReg.value) return false
  return firstReg.value.register_type === 'coil' || firstReg.value.register_type === 'discrete_input'
})

const allSameType = computed(() => {
  if (sortedRegs.value.length <= 1) return true
  const t = sortedRegs.value[0].register_type
  return sortedRegs.value.every(r => r.register_type === t)
})

const selCount = computed(() => sortedRegs.value.length)

// Editing state
const editingField = ref<string | null>(null)
const editValue = ref('')
const editReady = ref(false)

function fmtAddress(addr: number): string {
  return formatAddress(addr, addrMode.value)
}

const panelTitle = computed(() => {
  if (!firstReg.value) return ''
  const prefix = registerTypeLabel(firstReg.value.register_type)
  if (selCount.value === 1) {
    return `${prefix} @ ${fmtAddress(firstReg.value.address)}`
  }
  const last = sortedRegs.value[sortedRegs.value.length - 1]
  return `${prefix} @ ${fmtAddress(firstReg.value.address)}~${fmtAddress(last.address)}`
})

// 16-bit interpretations (shared composable)
const rawValue = computed(() => firstReg.value ? firstReg.value.value & 0xFFFF : 0)
const { signed16, unsigned16, hex16, binary16 } = use16BitFormat(rawValue)

// 32-bit
const show32bit = computed(() => {
  return selCount.value >= 2 && allSameType.value && !isBoolType.value
})

const reg32Hi = computed(() => show32bit.value ? sortedRegs.value[0].value & 0xFFFF : 0)
const reg32Lo = computed(() => show32bit.value ? sortedRegs.value[1].value & 0xFFFF : 0)
const { longABCD, longCDAB, longBADC, longDCBA, floatABCD, floatCDAB, floatBADC, floatDCBA } =
  use32BitFormat(reg32Hi, reg32Lo, show32bit)

// 64-bit
const show64bit = computed(() => {
  return selCount.value >= 4 && allSameType.value && !isBoolType.value
})

const reg64Values = computed(() => {
  if (!show64bit.value) return [0, 0, 0, 0]
  return sortedRegs.value.slice(0, 4).map(r => r.value & 0xFFFF)
})
const { doubleValue, doubleReversed, doubleByteSwap, doubleLittleEndian } =
  use64BitFormat(reg64Values, show64bit)

// --- Editing ---

function startEdit(field: string, currentValue: string) {
  editReady.value = false
  editingField.value = field
  editValue.value = currentValue
  nextTick(() => {
    editReady.value = true
  })
}

function cancelEdit() {
  editingField.value = null
  editReady.value = false
}

watch(selectedRegister, () => {
  cancelEdit()
})

function reverseParseField(field: string, input: string): { address: number; register_type: string; value: number }[] | null {
  const regs = sortedRegs.value
  if (regs.length === 0) return null

  // 16-bit
  if (field === 'signed16') {
    const n = Number(input)
    if (isNaN(n)) return null
    const v = n < 0 ? (n + 0x10000) & 0xFFFF : n & 0xFFFF
    return [{ address: regs[0].address, register_type: regs[0].register_type, value: v }]
  }
  if (field === 'unsigned16') {
    const n = Number(input)
    if (isNaN(n)) return null
    return [{ address: regs[0].address, register_type: regs[0].register_type, value: n & 0xFFFF }]
  }
  if (field === 'hex16') {
    const hex = input.replace(/^0x/i, '')
    const n = parseInt(hex, 16)
    if (isNaN(n)) return null
    return [{ address: regs[0].address, register_type: regs[0].register_type, value: n & 0xFFFF }]
  }

  // 32-bit Long
  if (field === 'longABCD' || field === 'longCDAB' || field === 'longBADC' || field === 'longDCBA') {
    if (regs.length < 2) return null
    const n = Number(input)
    if (isNaN(n)) return null
    const u = n >>> 0
    const hi = (u >>> 16) & 0xFFFF
    const lo = u & 0xFFFF
    const map: Record<string, [number, number]> = {
      longABCD: [hi, lo],
      longCDAB: [lo, hi],
      longBADC: [swapBytes16(hi), swapBytes16(lo)],
      longDCBA: [swapBytes16(lo), swapBytes16(hi)],
    }
    const [r0, r1] = map[field]
    return [
      { address: regs[0].address, register_type: regs[0].register_type, value: r0 },
      { address: regs[1].address, register_type: regs[1].register_type, value: r1 },
    ]
  }
  // 32-bit Float
  if (field === 'floatABCD' || field === 'floatCDAB' || field === 'floatBADC' || field === 'floatDCBA') {
    if (regs.length < 2) return null
    const n = parseFloat(input)
    if (isNaN(n)) return null
    const buf = new ArrayBuffer(4)
    const view = new DataView(buf)
    view.setFloat32(0, n)
    const w0 = view.getUint16(0)
    const w1 = view.getUint16(2)
    const map: Record<string, [number, number]> = {
      floatABCD: [w0, w1],
      floatCDAB: [w1, w0],
      floatBADC: [swapBytes16(w0), swapBytes16(w1)],
      floatDCBA: [swapBytes16(w1), swapBytes16(w0)],
    }
    const [r0, r1] = map[field]
    return [
      { address: regs[0].address, register_type: regs[0].register_type, value: r0 },
      { address: regs[1].address, register_type: regs[1].register_type, value: r1 },
    ]
  }

  // 64-bit
  if (field === 'double' || field === 'doubleReversed' || field === 'doubleByteSwap' || field === 'doubleLittleEndian') {
    if (regs.length < 4) return null
    const n = parseFloat(input)
    if (isNaN(n)) return null
    const buf = new ArrayBuffer(8)
    const view = new DataView(buf)
    view.setFloat64(0, n)
    const w = [view.getUint16(0), view.getUint16(2), view.getUint16(4), view.getUint16(6)]
    let vals: number[]
    if (field === 'double') {
      vals = [w[0], w[1], w[2], w[3]]
    } else if (field === 'doubleReversed') {
      vals = [w[3], w[2], w[1], w[0]]
    } else if (field === 'doubleByteSwap') {
      vals = [swapBytes16(w[0]), swapBytes16(w[1]), swapBytes16(w[2]), swapBytes16(w[3])]
    } else {
      vals = [swapBytes16(w[3]), swapBytes16(w[2]), swapBytes16(w[1]), swapBytes16(w[0])]
    }
    return vals.map((v, i) => ({ address: regs[i].address, register_type: regs[i].register_type, value: v }))
  }

  return null
}

async function writeRegisters(writes: { address: number; register_type: string; value: number }[]) {
  if (!selectedConnectionId.value || selectedSlaveId.value === null) return
  try {
    for (const w of writes) {
      await invoke('write_register', {
        request: {
          connection_id: selectedConnectionId.value,
          slave_id: selectedSlaveId.value,
          register_type: w.register_type,
          address: w.address,
          value: w.value,
        }
      })
    }
  } catch (e) {
    await showAlert(String(e))
  }
  refreshRegisters()
}

function onBlur() {
  if (!editReady.value) return
  commitEdit()
}

async function commitEdit() {
  if (!editingField.value) return
  const field = editingField.value
  const input = editValue.value
  editingField.value = null
  editReady.value = false
  const writes = reverseParseField(field, input)
  if (!writes) return
  await writeRegisters(writes)
}

async function toggleBool(reg: SelectedReg) {
  await writeRegisters([{
    address: reg.address,
    register_type: reg.register_type,
    value: reg.value !== 0 ? 0 : 1,
  }])
}

async function handleEditKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    await commitEdit()
  } else if (e.key === 'Escape') {
    e.preventDefault()
    cancelEdit()
  }
}
</script>

<template>
  <div class="value-panel">
    <div class="panel-header">{{ t('valuePanel.title') }}</div>

    <div v-if="!hasSelection" class="empty-state">
      {{ t('valuePanel.selectHint') }}
    </div>

    <template v-else>
      <div class="panel-title">{{ panelTitle }}</div>
      <div v-if="selCount > 1 && !allSameType" class="panel-hint">
        {{ t('valuePanel.mixedTypeHint') }}
      </div>

      <!-- Bool type -->
      <template v-if="isBoolType">
        <div class="value-section">
          <div v-for="reg in sortedRegs" :key="`${reg.register_type}-${reg.address}`" class="value-row">
            <span class="value-label">{{ fmtAddress(reg.address) }}</span>
            <span class="value-data editable" @click="toggleBool(reg)">{{ reg.value !== 0 ? 'true (1)' : 'false (0)' }}</span>
          </div>
        </div>
      </template>

      <!-- Numeric register(s) -->
      <template v-else>
        <div class="value-section">
          <div class="section-title">16-bit</div>
          <div class="value-row">
            <span class="value-label">Signed</span>
            <input v-if="editingField === 'signed16'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('signed16', String(signed16))">{{ signed16 }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Unsigned</span>
            <input v-if="editingField === 'unsigned16'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('unsigned16', String(unsigned16))">{{ unsigned16 }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Hex</span>
            <input v-if="editingField === 'hex16'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('hex16', hex16)">{{ hex16 }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Binary</span>
            <span class="value-data mono">{{ binary16 }}</span>
          </div>
        </div>

        <div v-if="show32bit" class="value-section">
          <div class="section-title">32-bit ({{ sortedRegs.slice(0, 2).map(r => fmtAddress(r.address)).join(' + ') }})</div>
          <div class="value-row">
            <span class="value-label">Long AB CD</span>
            <input v-if="editingField === 'longABCD'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('longABCD', longABCD)">{{ longABCD }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Long CD AB</span>
            <input v-if="editingField === 'longCDAB'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('longCDAB', longCDAB)">{{ longCDAB }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Float AB CD</span>
            <input v-if="editingField === 'floatABCD'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('floatABCD', floatABCD)">{{ floatABCD }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Float CD AB</span>
            <input v-if="editingField === 'floatCDAB'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('floatCDAB', floatCDAB)">{{ floatCDAB }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Long BA DC</span>
            <input v-if="editingField === 'longBADC'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('longBADC', longBADC)">{{ longBADC }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Long DC BA</span>
            <input v-if="editingField === 'longDCBA'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('longDCBA', longDCBA)">{{ longDCBA }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Float BA DC</span>
            <input v-if="editingField === 'floatBADC'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('floatBADC', floatBADC)">{{ floatBADC }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Float DC BA</span>
            <input v-if="editingField === 'floatDCBA'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('floatDCBA', floatDCBA)">{{ floatDCBA }}</span>
          </div>
        </div>

        <div v-if="show64bit" class="value-section">
          <div class="section-title">64-bit ({{ sortedRegs.slice(0, 4).map(r => fmtAddress(r.address)).join(' + ') }})</div>
          <div class="value-row">
            <span class="value-label">Double AB CD EF GH</span>
            <input v-if="editingField === 'double'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('double', doubleValue)">{{ doubleValue }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Double GH EF CD AB</span>
            <input v-if="editingField === 'doubleReversed'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('doubleReversed', doubleReversed)">{{ doubleReversed }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Double BA DC FE HG</span>
            <input v-if="editingField === 'doubleByteSwap'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('doubleByteSwap', doubleByteSwap)">{{ doubleByteSwap }}</span>
          </div>
          <div class="value-row">
            <span class="value-label">Double HG FE DC BA</span>
            <input v-if="editingField === 'doubleLittleEndian'" v-focus v-model="editValue" class="panel-edit-input" @blur="onBlur" @keydown="handleEditKeydown" />
            <span v-else class="value-data mono editable" @click="startEdit('doubleLittleEndian', doubleLittleEndian)">{{ doubleLittleEndian }}</span>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.value-panel {
  padding: 0;
  font-size: 13px;
}

.panel-header {
  padding: 8px 12px;
  font-size: 11px;
  text-transform: uppercase;
  color: var(--c-overlay0);
  letter-spacing: 0.5px;
}

.empty-state {
  padding: 24px 12px;
  color: var(--c-overlay0);
  text-align: center;
  font-size: 12px;
}

.panel-title {
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 600;
  color: var(--c-blue);
  border-bottom: 1px solid var(--c-surface0);
  margin-bottom: 4px;
}

.panel-hint {
  padding: 6px 12px;
  font-size: 11px;
  color: var(--c-peach);
}

.value-section {
  padding: 4px 0;
  border-bottom: 1px solid var(--c-surface0);
}

.section-title {
  padding: 4px 12px;
  font-size: 11px;
  color: var(--c-overlay0);
  text-transform: uppercase;
}

.value-row {
  display: flex;
  justify-content: space-between;
  padding: 3px 12px;
}

.value-label {
  color: var(--c-overlay0);
  font-size: 12px;
}

.value-data {
  color: var(--c-text);
  font-size: 12px;
  text-align: right;
}

.value-data.mono {
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.value-data.editable {
  cursor: pointer;
  border-radius: 3px;
  padding: 0 4px;
  user-select: none;
}

.value-data.editable:hover {
  background: var(--c-surface0);
}

.panel-edit-input {
  width: 120px;
  padding: 1px 6px;
  background: var(--c-base);
  border: 1px solid var(--c-blue);
  border-radius: 3px;
  color: var(--c-text);
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 12px;
  text-align: right;
  outline: none;
}
</style>

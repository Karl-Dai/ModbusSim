import type { project, main, appstate } from '../../wailsjs/go/models'
import { swapBytes16, toFloat32, float32ToU16Pair, type ByteOrder } from 'shared-frontend'

export type ValueFormat =
  | 'auto'
  | 'unsigned'
  | 'signed'
  | 'hex'
  | 'binary'
  | 'float32_abcd'
  | 'float32_cdab'
  | 'float32_badc'
  | 'float32_dcba'

export type MutationMode = 'flip' | 'increment' | 'decrement' | 'random'

export type MutationConfig = project.MutationConfig
export type DataSource = project.DataSource
export type DataSourceConfig = project.DataSourceConfig
export type RegisterDef = main.Point
export type PointMutationInfo = appstate.PointMutationInfo

export const ENDIAN_TO_BYTEORDER: Record<string, ByteOrder> = {
  big: 'ABCD',
  little: 'CDAB',
  mid_big: 'BADC',
  mid_little: 'DCBA',
}

export function is32BitType(dataType: string): boolean {
  return dataType === 'uint32' || dataType === 'int32' || dataType === 'float32'
}

export function isFloatFormat(fmt: ValueFormat): boolean {
  return fmt.startsWith('float32_')
}

/** Format a single u16 with the chosen display format (auto handled by caller). */
export function formatU16(raw: number, fmt: ValueFormat): string {
  const v = raw & 0xFFFF
  switch (fmt) {
    case 'signed': return (v >= 0x8000 ? v - 0x10000 : v).toString()
    case 'hex':    return '0x' + v.toString(16).toUpperCase().padStart(4, '0')
    case 'binary': {
      const b = v.toString(2).padStart(16, '0')
      return `${b.slice(0, 4)} ${b.slice(4, 8)} ${b.slice(8, 12)} ${b.slice(12, 16)}`
    }
    default: return v.toString()
  }
}

function applyEndianDecode(reg0: number, reg1: number, endian: string): [number, number, number, number] {
  const r0h = (reg0 >> 8) & 0xFF, r0l = reg0 & 0xFF
  const r1h = (reg1 >> 8) & 0xFF, r1l = reg1 & 0xFF
  switch (endian) {
    case 'big':        return [r0h, r0l, r1h, r1l]
    case 'little':     return [r1h, r1l, r0h, r0l]
    case 'mid_big':    return [r0l, r0h, r1l, r1h]
    case 'mid_little': return [r1l, r1h, r0l, r0h]
    default:           return [r0h, r0l, r1h, r1l]
  }
}

/** "Auto" mode: format using the register's declared DataType + endian, with companion u16 if needed. */
export function formatTypedValue(reg: RegisterDef, hi: number, lo: number): string {
  switch (reg.DataType) {
    case 'bool':   return hi !== 0 ? 'ON' : 'OFF'
    case 'uint16': return (hi & 0xFFFF).toString()
    case 'int16':  { const v = hi & 0xFFFF; return (v >= 0x8000 ? v - 0x10000 : v).toString() }
    case 'uint32':
    case 'int32':
    case 'float32': {
      const bytes = applyEndianDecode(hi & 0xFFFF, lo & 0xFFFF, reg.Endian)
      const canonHi = (bytes[0] << 8) | bytes[1]
      const canonLo = (bytes[2] << 8) | bytes[3]
      if (reg.DataType === 'float32') return toFloat32(canonHi, canonLo)
      const buf = new ArrayBuffer(4)
      const view = new DataView(buf)
      view.setUint16(0, canonHi)
      view.setUint16(2, canonLo)
      if (reg.DataType === 'uint32') return view.getUint32(0).toString()
      return view.getInt32(0).toString()
    }
    default: return (hi & 0xFFFF).toString()
  }
}

/** "Float AB CD" / etc display modes — interpret two consecutive u16 words as IEEE-754 float. */
export function formatFloatPair(fmt: ValueFormat, hi: number, lo: number): string {
  const a = hi & 0xFFFF, b = lo & 0xFFFF
  switch (fmt) {
    case 'float32_abcd': return toFloat32(a, b)
    case 'float32_cdab': return toFloat32(b, a)
    case 'float32_badc': return toFloat32(swapBytes16(a), swapBytes16(b))
    case 'float32_dcba': return toFloat32(swapBytes16(b), swapBytes16(a))
    default: return toFloat32(a, b)
  }
}

/** Encode a typed numeric value back into two u16 words for write_register. */
export function encodeTypedValue(value: number, dataType: string, endian: string): [number, number] {
  if (dataType === 'float32') return float32ToU16Pair(value, ENDIAN_TO_BYTEORDER[endian] || 'ABCD')
  const buf = new ArrayBuffer(4)
  const view = new DataView(buf)
  if (dataType === 'int32') view.setInt32(0, value)
  else view.setUint32(0, value >>> 0)
  const w0 = view.getUint16(0), w1 = view.getUint16(2)
  const order = ENDIAN_TO_BYTEORDER[endian] || 'ABCD'
  switch (order) {
    case 'ABCD': return [w0, w1]
    case 'CDAB': return [w1, w0]
    case 'BADC': return [swapBytes16(w0), swapBytes16(w1)]
    case 'DCBA': return [swapBytes16(w1), swapBytes16(w0)]
  }
}

export function normalizeMutationMode(value: string): MutationMode {
  return value === 'increment' || value === 'decrement' || value === 'random' ? value : 'flip'
}

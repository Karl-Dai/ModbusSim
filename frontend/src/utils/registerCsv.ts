import type { RegisterDef } from '../composables/useRegisterFormat'
const columns = ['RegisterType', 'Address', 'DataType', 'Endian', 'Name', 'Comment', 'Mutation', 'DataSource'] as const
export function encodeRegisterCsv(rows: RegisterDef[]): string {
  const quote = (value: unknown) => `"${String(value ?? '').replaceAll('"', '""')}"`
  return '\uFEFF' + [columns.join(','), ...rows.map(row => columns.map(key => quote(key === 'Mutation' || key === 'DataSource' ? row[key] ? JSON.stringify(row[key]) : '' : row[key])).join(','))].join('\r\n')
}

// Legacy CSV is translated once at import, just like a v1 project. Live DTOs
// and new exports use native names throughout.
function decodeBehaviour(text: string) {
  const legacy: Record<string, string> = {
    enabled: 'Enabled', mode: 'Mode', period_ms: 'PeriodMS', step: 'Step', min: 'Min', max: 'Max',
    source: 'Source', update_interval_ms: 'UpdateIntervalMS', type: 'Type', value: 'Value',
    amplitude: 'Amplitude', frequency: 'Frequency', offset: 'Offset', phase: 'Phase',
    start: 'Start', wrap: 'Wrap', values: 'Values', loop_playback: 'LoopPlayback',
  }
  return JSON.parse(text, (_key, value) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return value
    const result: Record<string, unknown> = {}
    for (const [key, item] of Object.entries(value)) {
      const name = legacy[key] ?? key
      if (name in result) throw new Error(`Duplicate behaviour field: ${name}`)
      result[name] = item
    }
    return result
  })
}

/** RFC4180 quoting, including commas, doubled quotes and multiline fields. */
export function decodeRegisterCsv(input: string): RegisterDef[] {
  const rows: string[][] = []; let row: string[] = [], field = '', quoted = false, endedQuote = false
  const text = input.replace(/^\uFEFF/, '')
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') { field += '"'; i++ }
      else if (c === '"') { quoted = false; endedQuote = true }
      else field += c
    } else if (c === '"') {
      if (field || endedQuote) throw new Error('Invalid CSV quote')
      quoted = true
    } else if (c === ',' || c === '\n' || c === '\r') {
      row.push(field); field = ''; endedQuote = false
      if (c !== ',') { if (row.some(value => value.length)) rows.push(row); row = []; if (c === '\r' && text[i + 1] === '\n') i++ }
    } else { if (endedQuote) throw new Error('Unexpected character after CSV quote'); field += c }
  }
  if (quoted) throw new Error('Unterminated CSV quote')
  row.push(field); if (row.some(value => value.length)) rows.push(row)
  const aliases: Record<string, string> = {"register_type": "RegisterType", "address": "Address", "data_type": "DataType", "endian": "Endian", "name": "Name", "comment": "Comment", "mutation": "Mutation", "data_source": "DataSource"}
  const header = rows.shift()?.map(value => aliases[value.trim()] ?? value.trim()) ?? []
  for (const required of columns.slice(0, 4)) if (!header.includes(required)) throw new Error(`Missing CSV column: ${required}`)
  if (new Set(header).size !== header.length) throw new Error('Duplicate CSV column')
  const seen = new Set<string>()
  return rows.map((values, index) => {
    if (values.length !== header.length) throw new Error(`CSV row ${index + 2}: wrong column count`)
    const fields = Object.fromEntries(header.map((key, column) => [key, values[column]]))
    fields.DataType = fields.DataType.replace(/^u_int/, 'uint')
    const address = Number(fields.Address)
    const width = ['holding_register', 'input_register'].includes(fields.RegisterType) && ['uint32', 'int32', 'float32'].includes(fields.DataType) ? 2 : 1
    if (!fields.Address.trim() || !Number.isInteger(address) || address < 0 || address + width > 65536) throw new Error(`CSV row ${index + 2}: invalid address`)
    if (!['coil', 'discrete_input', 'input_register', 'holding_register'].includes(fields.RegisterType)) throw new Error(`CSV row ${index + 2}: invalid RegisterType`)
    if (!['bool', 'uint16', 'int16', 'uint32', 'int32', 'float32'].includes(fields.DataType)) throw new Error(`CSV row ${index + 2}: invalid DataType`)
    if (!['big', 'little', 'mid_big', 'mid_little'].includes(fields.Endian)) throw new Error(`CSV row ${index + 2}: invalid Endian`)
    for (let offset = 0; offset < width; offset++) {
      const key = `${fields.RegisterType}:${address + offset}`
      if (seen.has(key)) throw new Error(`CSV row ${index + 2}: overlapping address ${key}`)
      seen.add(key)
    }
    return { RegisterType: fields.RegisterType, Address: address, DataType: fields.DataType, Endian: fields.Endian,
      Name: fields.Name ?? '', Comment: fields.Comment ?? '',
      Mutation: fields.Mutation ? decodeBehaviour(fields.Mutation) : null, DataSource: fields.DataSource ? decodeBehaviour(fields.DataSource) : null }
  })
}
export const registerCsvTemplate = () => encodeRegisterCsv([{ RegisterType: 'holding_register', Address: 0, DataType: 'uint16', Endian: 'big', Name: 'Register 0', Comment: '' }])

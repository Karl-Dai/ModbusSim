import type { RegisterDef } from '../composables/useRegisterFormat'
const columns = ['register_type', 'address', 'data_type', 'endian', 'name', 'comment', 'mutation', 'data_source'] as const
export function encodeRegisterCsv(rows: RegisterDef[]): string {
  rows = rows.map(row => ({ ...row, data_type: row.data_type.replace(/^u_int/, 'uint') }))
  const quote = (value: unknown) => `"${String(value ?? '').replaceAll('"', '""')}"`
  return '\uFEFF' + [columns.join(','), ...rows.map(row => columns.map(key => quote(key === 'mutation' || key === 'data_source' ? row[key] ? JSON.stringify(row[key]) : '' : row[key])).join(','))].join('\r\n')
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
  const header = rows.shift()?.map(value => value.trim()) ?? []
  for (const required of columns.slice(0, 4)) if (!header.includes(required)) throw new Error(`Missing CSV column: ${required}`)
  if (new Set(header).size !== header.length) throw new Error('Duplicate CSV column')
  const seen = new Set<string>()
  return rows.map((values, index) => {
    if (values.length !== header.length) throw new Error(`CSV row ${index + 2}: wrong column count`)
    const fields = Object.fromEntries(header.map((key, column) => [key, values[column]]))
    fields.data_type = fields.data_type.replace(/^u_int/, 'uint')
    const address = Number(fields.address)
    const width = ['holding_register', 'input_register'].includes(fields.register_type) && ['uint32', 'int32', 'float32'].includes(fields.data_type) ? 2 : 1
    if (!fields.address.trim() || !Number.isInteger(address) || address < 0 || address + width > 65536) throw new Error(`CSV row ${index + 2}: invalid address`)
    if (!['coil', 'discrete_input', 'input_register', 'holding_register'].includes(fields.register_type)) throw new Error(`CSV row ${index + 2}: invalid register_type`)
    if (!['bool', 'uint16', 'int16', 'uint32', 'int32', 'float32'].includes(fields.data_type)) throw new Error(`CSV row ${index + 2}: invalid data_type`)
    if (!['big', 'little', 'mid_big', 'mid_little'].includes(fields.endian)) throw new Error(`CSV row ${index + 2}: invalid endian`)
    for (let offset = 0; offset < width; offset++) {
      const key = `${fields.register_type}:${address + offset}`
      if (seen.has(key)) throw new Error(`CSV row ${index + 2}: overlapping address ${key}`)
      seen.add(key)
    }
    return { register_type: fields.register_type, address, data_type: fields.data_type, endian: fields.endian,
      name: fields.name ?? '', comment: fields.comment ?? '',
      mutation: fields.mutation ? JSON.parse(fields.mutation) : null, data_source: fields.data_source ? JSON.parse(fields.data_source) : null }
  })
}
export const registerCsvTemplate = () => encodeRegisterCsv([{ register_type: 'holding_register', address: 0, data_type: 'uint16', endian: 'big', name: 'Register 0', comment: '' }])

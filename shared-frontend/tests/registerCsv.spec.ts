import { describe, expect, it } from 'vitest'
import { decodeRegisterCsv, encodeRegisterCsv } from '../../frontend/src/utils/registerCsv'
describe('register CSV', () => {
  const reg = { RegisterType: 'holding_register', Address: 0, DataType: 'uint16', Endian: 'big', Name: '风机,"A"', Comment: 'line 1\nline 2', Mutation: null, DataSource: null }
  it('round-trips Unicode, quotes, commas, newlines and simulation configuration', () => {
    expect(decodeRegisterCsv(encodeRegisterCsv([reg]))).toEqual([reg])
  })
  it('rejects an invalid document before an import can be submitted', () => {
    expect(() => decodeRegisterCsv('register_type,address,data_type,endian\nholding_register,65536,uint16,big')).toThrow('invalid address')
    expect(() => decodeRegisterCsv(encodeRegisterCsv([reg, reg]))).toThrow('overlapping address')
    expect(() => decodeRegisterCsv('"unterminated')).toThrow('Unterminated')
  })
  it('checks 32-bit overlap and rejects malformed quoting', () => {
    expect(() => decodeRegisterCsv(encodeRegisterCsv([{ ...reg, DataType: 'float32' }, { ...reg, Address: 1 }]))).toThrow('overlapping')
    expect(() => decodeRegisterCsv('register_type,address,data_type,endian\n"holding_register"x,0,uint16,big')).toThrow('after CSV quote')
  })
  it('imports legacy nested simulation fields once and preserves native configurations on export', () => {
    const configured = { ...reg, Mutation: { Enabled: true, Mode: 'increment', PeriodMS: 500, Step: 1, Min: 0, Max: 100 }, DataSource: null }
    expect(decodeRegisterCsv(encodeRegisterCsv([configured]))).toEqual([configured])
    const legacyMutation = JSON.stringify({ enabled: true, mode: 'increment', period_ms: 500, step: 1, min: 0, max: 100 }).replaceAll('"', '""')
    const legacy = 'register_type,address,data_type,endian,name,comment,mutation,data_source\r\n' + `holding_register,0,uint16,big,,,"${legacyMutation}",`
    expect(decodeRegisterCsv(legacy)[0].Mutation).toEqual(configured.Mutation)
  })

})

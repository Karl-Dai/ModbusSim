import { describe, expect, it } from 'vitest'
import { decodeRegisterCsv, encodeRegisterCsv } from '../../frontend/src/utils/registerCsv'
describe('register CSV', () => {
  const reg = { register_type: 'holding_register', address: 0, data_type: 'uint16', endian: 'big', name: '风机,"A"', comment: 'line 1\nline 2', mutation: null, data_source: null }
  it('round-trips Unicode, quotes, commas, newlines and simulation configuration', () => {
    expect(decodeRegisterCsv(encodeRegisterCsv([reg]))).toEqual([reg])
  })
  it('rejects an invalid document before an import can be submitted', () => {
    expect(() => decodeRegisterCsv('register_type,address,data_type,endian\nholding_register,65536,uint16,big')).toThrow('invalid address')
    expect(() => decodeRegisterCsv(encodeRegisterCsv([reg, reg]))).toThrow('overlapping address')
    expect(() => decodeRegisterCsv('"unterminated')).toThrow('Unterminated')
  })
  it('checks 32-bit overlap and rejects malformed quoting', () => {
    expect(() => decodeRegisterCsv(encodeRegisterCsv([{ ...reg, data_type: 'float32' }, { ...reg, address: 1 }]))).toThrow('overlapping')
    expect(() => decodeRegisterCsv('register_type,address,data_type,endian\n"holding_register"x,0,uint16,big')).toThrow('after CSV quote')
  })
})

import type { masterstate } from '../wailsjs/go/models'
export type MasterConnectionInfo = masterstate.ConnectionInfo
export type ScanGroupInfo = masterstate.ScanGroup
export type RegisterValueDto = masterstate.RegisterValue
export type ReadResultDto = masterstate.ReadResult
export type LogEntry = masterstate.LogEntry
export interface PollDataPayload { ConnectionID: string; ScanGroupID: string; Result: ReadResultDto }
export interface PollErrorPayload { ConnectionID: string; ScanGroupID: string; Error: string }
export interface SlaveIdScanEvent { ConnectionID: string; TaskID: string; CurrentID: number; Total: number; FoundIDs: number[]; Done: boolean; Cancelled: boolean }
export interface FoundRegisterDto { Address: number; Value: number }
export interface RegisterScanEvent { ConnectionID: string; TaskID: string; CurrentAddress: number; EndAddress: number; FoundCount: number; FoundRegisters: FoundRegisterDto[]; Done: boolean; Cancelled: boolean }

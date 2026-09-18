// Thin typed client for the Go backend's JSON API.

export interface ConnectionInfo {
  id: string
  bind_address: string
  port: number
  state: string
  device_count: number
}

export interface RegisterDef {
  address: number
  register_type: string
  data_type: string
  endian: string
  name: string
  comment: string
  mutation?: MutationConfig | null
  data_source?: unknown | null
}

export interface MutationConfig {
  enabled: boolean
  mode: string
  period_ms: number
  step: number
  min: number
  max: number
}

export interface LogEntry {
  timestamp: string
  direction: string
  function_code: string
  detail: string
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((data as { error?: string }).error ?? res.statusText)
  }
  return data as T
}

export const api = {
  listConnections: () => req<ConnectionInfo[]>('GET', '/api/connections'),
  createConnection: (body: {
    slave_id: number
    name: string
    transport: { type: string; host: string; port: number }
  }) => req<ConnectionInfo>('POST', '/api/connections', body),
  startConnection: (id: string) => req<{ state: string }>('POST', `/api/connections/${id}/start`),
  stopConnection: (id: string) => req<{ state: string }>('POST', `/api/connections/${id}/stop`),
  deleteConnection: (id: string) => req<{ ok: string }>('DELETE', `/api/connections/${id}`),

  listRegisters: (id: string, slaveId: number) =>
    req<RegisterDef[]>('GET', `/api/connections/${id}/devices/${slaveId}/registers`),
  readRegisters: (id: string, slaveId: number, registerType: string, address: number, count: number) =>
    req<{ values: number[] }>('POST', `/api/connections/${id}/devices/${slaveId}/read`, {
      slave_id: slaveId,
      register_type: registerType,
      address,
      count,
    }),
  writeRegister: (id: string, slaveId: number, registerType: string, address: number, value: number) =>
    req<{ ok: string }>('POST', `/api/connections/${id}/devices/${slaveId}/write`, {
      slave_id: slaveId,
      register_type: registerType,
      address,
      value,
    }),
  addRegister: (body: {
    connection_id: string
    slave_id: number
    address: number
    register_type: string
    data_type: string
  }) => req<{ ok: string }>('POST', `/api/connections/${body.connection_id}/devices/${body.slave_id}/registers`, body),

  getLogs: (id: string) => req<LogEntry[]>('GET', `/api/connections/${id}/logs`),
  clearLogs: (id: string) => req<{ ok: string }>('POST', `/api/connections/${id}/logs/clear`),

  setMutation: (body: {
    connection_id: string
    slave_id: number
    register_type: string
    address: number
    config: MutationConfig
  }) => req<{ ok: string }>('POST', '/api/points/mutation', body),
  setMutationRunning: (running: boolean) =>
    req<{ running: boolean }>('POST', '/api/mutation/running', { running }),
}

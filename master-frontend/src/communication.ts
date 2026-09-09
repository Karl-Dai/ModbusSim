export function defaultCommunicationOptions() {
  return {
    requests: { interval_ms: 0, max_read_registers: 125, max_read_bits: 2000 },
    reconnect: { enabled: true, initial_delay_ms: 1000, max_delay_ms: 30000, backoff_factor: 2, max_attempts: 0 },
  }
}

export type CommunicationOptions = ReturnType<typeof defaultCommunicationOptions>

export function validCommunicationOptions(options: CommunicationOptions): boolean {
  const integerIn = (value: number, min: number, max: number) => Number.isInteger(value) && value >= min && value <= max
  const { requests, reconnect } = options
  return integerIn(requests.interval_ms, 0, 60000)
    && integerIn(requests.max_read_registers, 1, 125)
    && integerIn(requests.max_read_bits, 1, 2000)
    && integerIn(reconnect.initial_delay_ms, 1, 60000)
    && integerIn(reconnect.max_delay_ms, reconnect.initial_delay_ms, 600000)
    && Number.isFinite(reconnect.backoff_factor) && reconnect.backoff_factor >= 1 && reconnect.backoff_factor <= 10
    && integerIn(reconnect.max_attempts, 0, 10000)
}

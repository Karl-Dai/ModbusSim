export function defaultCommunicationOptions() {
  return {
    requests: { IntervalMS: 0, MaxReadRegisters: 125, MaxReadBits: 2000 },
    reconnect: { Enabled: true, InitialDelayMS: 1000, MaxDelayMS: 30000, BackoffFactor: 2, MaxAttempts: 0 },
  }
}

export type CommunicationOptions = ReturnType<typeof defaultCommunicationOptions>

export function validCommunicationOptions(options: CommunicationOptions): boolean {
  const integerIn = (value: number, min: number, max: number) => Number.isInteger(value) && value >= min && value <= max
  const { requests, reconnect } = options
  return integerIn(requests.IntervalMS, 0, 60000)
    && integerIn(requests.MaxReadRegisters, 1, 125)
    && integerIn(requests.MaxReadBits, 1, 2000)
    && integerIn(reconnect.InitialDelayMS, 1, 60000)
    && integerIn(reconnect.MaxDelayMS, reconnect.InitialDelayMS, 600000)
    && Number.isFinite(reconnect.BackoffFactor) && reconnect.BackoffFactor >= 1 && reconnect.BackoffFactor <= 10
    && integerIn(reconnect.MaxAttempts, 0, 10000)
}

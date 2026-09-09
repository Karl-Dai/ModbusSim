<script setup lang="ts">
import { useI18n } from 'shared-frontend'
import type { CommunicationOptions } from '../communication'

const options = defineModel<CommunicationOptions>({ required: true })
const { t } = useI18n()
</script>

<template>
  <details class="communication-settings">
    <summary>{{ t('dialog.communicationSettings') }}</summary>
    <div class="settings-body">
      <label>
        {{ t('dialog.requestInterval') }}
        <input v-model.number="options.requests.interval_ms" type="number" min="0" max="60000" aria-describedby="request-interval-hint" />
      </label>
      <p id="request-interval-hint">{{ t('dialog.requestIntervalHint') }}</p>
      <label>
        {{ t('dialog.maxReadRegisters') }}
        <input v-model.number="options.requests.max_read_registers" type="number" min="1" max="125" aria-describedby="read-registers-hint" />
      </label>
      <p id="read-registers-hint">{{ t('dialog.maxReadRegistersHint') }}</p>
      <label>
        {{ t('dialog.maxReadBits') }}
        <input v-model.number="options.requests.max_read_bits" type="number" min="1" max="2000" aria-describedby="read-bits-hint" />
      </label>
      <p id="read-bits-hint">{{ t('dialog.maxReadBitsHint') }}</p>
      <label class="checkbox-label">
        <input v-model="options.reconnect.enabled" type="checkbox" /> {{ t('dialog.autoReconnect') }}
      </label>
      <p>{{ t('dialog.autoReconnectHint') }}</p>
      <template v-if="options.reconnect.enabled">
        <label>
          {{ t('dialog.reconnectInitialDelay') }}
          <input v-model.number="options.reconnect.initial_delay_ms" type="number" min="1" max="60000" />
        </label>
        <label>
          {{ t('dialog.reconnectMaxDelay') }}
          <input v-model.number="options.reconnect.max_delay_ms" type="number" :min="options.reconnect.initial_delay_ms" max="600000" />
        </label>
        <label>
          {{ t('dialog.reconnectBackoff') }}
          <input v-model.number="options.reconnect.backoff_factor" type="number" min="1" max="10" step="0.1" aria-describedby="reconnect-backoff-hint" />
        </label>
        <p id="reconnect-backoff-hint">{{ t('dialog.reconnectBackoffHint') }}</p>
        <label>
          {{ t('dialog.reconnectAttempts') }}
          <input v-model.number="options.reconnect.max_attempts" type="number" min="0" max="10000" aria-describedby="reconnect-attempts-hint" />
        </label>
        <p id="reconnect-attempts-hint">{{ t('dialog.reconnectAttemptsHint') }}</p>
      </template>
    </div>
  </details>
</template>

<style scoped>
.communication-settings { border-top: 1px solid #45475a; padding-top: 12px; }
summary { color: #cdd6f4; font-size: 13px; cursor: pointer; }
.settings-body { display: flex; flex-direction: column; gap: 12px; margin-top: 12px; }
label { display: flex; flex-direction: column; gap: 4px; color: #a6adc8; font-size: 12px; }
input[type="number"] { min-width: 0; padding: 6px 10px; background: #313244; border: 1px solid #45475a; border-radius: 4px; color: #cdd6f4; font-size: 13px; }
input:focus-visible, summary:focus-visible { outline: 2px solid #89b4fa; outline-offset: 2px; }
.checkbox-label { flex-direction: row; align-items: center; }
p { margin: -6px 0 0; color: #a6adc8; font-size: 12px; line-height: 1.5; }
</style>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { open } from '@tauri-apps/plugin-dialog'
import { useI18n, showAlert, vModal } from 'shared-frontend'
import CommunicationSettings from './CommunicationSettings.vue'
import { defaultCommunicationOptions, validCommunicationOptions, type CommunicationOptions } from '../communication'

const { t } = useI18n()

interface Props { show: boolean; connectionId?: string }
interface ConnectionSettings {
  transport: { type: string; port: string | number; baud_rate?: number; data_bits?: number; stop_bits?: number; parity?: string }
  config: {
    target_address: string; port: number; slave_id: number; timeout_ms: number
    requests: CommunicationOptions['requests']
    tls: { enabled: boolean; ca_file: string; cert_file: string; key_file: string; pkcs12_file: string; pkcs12_password: string; accept_invalid_certs: boolean }
    socks5: { enabled: boolean; host: string; port: number; username: string; password: string }
  }
  reconnect_policy: Omit<CommunicationOptions['reconnect'], 'max_attempts'> & { max_attempts: number | null }
}
const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'created'): void
  (e: 'saved'): void
}>()

const busy = ref(false)
const loadFailed = ref(false)
const error = ref('')
function close() { if (!busy.value) emit('close') }

const form = ref({
  transport: 'tcp',
  target_address: '127.0.0.1',
  port: 502,
  slave_id: 1,
  timeout_ms: 3000,
})
const communication = ref(defaultCommunicationOptions())
const serialPort = ref('')
const baudRate = ref(9600)
const dataBits = ref(8)
const stopBits = ref(1)
const parityMode = ref('none')
const serialPorts = ref<{ name: string; description: string; manufacturer: string }[]>([])

const useTls = ref(false)
const tlsCaFile = ref('')
const tlsCertFile = ref('')
const tlsKeyFile = ref('')
const tlsPkcs12File = ref('')
const tlsPkcs12Password = ref('')
const tlsAcceptInvalidCerts = ref(false)
const useSocks5 = ref(false)
const socks5Host = ref('127.0.0.1')
const socks5Port = ref(1080)
const socks5Username = ref('')
const socks5Password = ref('')

watch(() => [props.show, props.connectionId] as const, async ([visible, connectionId], _old, onCleanup) => {
  let cancelled = false
  onCleanup(() => { cancelled = true })
  if (!visible) return
  error.value = ''
  loadFailed.value = false
  form.value = { transport: 'tcp', target_address: '127.0.0.1', port: 502, slave_id: 1, timeout_ms: 3000 }
  communication.value = defaultCommunicationOptions()
  serialPort.value = ''
  baudRate.value = 9600
  dataBits.value = 8
  stopBits.value = 1
  parityMode.value = 'none'
  useTls.value = false
  tlsCaFile.value = ''
  tlsCertFile.value = ''
  tlsKeyFile.value = ''
  tlsPkcs12File.value = ''
  tlsPkcs12Password.value = ''
  tlsAcceptInvalidCerts.value = false
  useSocks5.value = false
  socks5Host.value = '127.0.0.1'
  socks5Port.value = 1080
  socks5Username.value = ''
  socks5Password.value = ''
  if (!connectionId) return
  busy.value = true
  try {
    const settings = await invoke<ConnectionSettings>('get_master_connection_settings', { connectionId })
    if (cancelled) return
    const { config, transport, reconnect_policy } = settings
    form.value = {
      transport: transport.type === 'tcp_tls' ? 'tcp' : transport.type,
      target_address: config.target_address, port: config.port,
      slave_id: config.slave_id, timeout_ms: config.timeout_ms,
    }
    communication.value = { requests: config.requests, reconnect: { ...reconnect_policy, max_attempts: reconnect_policy.max_attempts ?? 0 } }
    if (transport.type === 'rtu' || transport.type === 'ascii') {
      serialPort.value = String(transport.port)
      baudRate.value = transport.baud_rate!
      dataBits.value = transport.data_bits!
      stopBits.value = transport.stop_bits!
      parityMode.value = transport.parity!
    }
    useTls.value = transport.type === 'tcp_tls'
    tlsCaFile.value = config.tls.ca_file
    tlsCertFile.value = config.tls.cert_file
    tlsKeyFile.value = config.tls.key_file
    tlsPkcs12File.value = config.tls.pkcs12_file
    tlsPkcs12Password.value = config.tls.pkcs12_password
    tlsAcceptInvalidCerts.value = config.tls.accept_invalid_certs
    useSocks5.value = config.socks5.enabled
    socks5Host.value = config.socks5.host
    socks5Port.value = config.socks5.port
    socks5Username.value = config.socks5.username
    socks5Password.value = config.socks5.password
  } catch (e) {
    if (!cancelled) { error.value = String(e); loadFailed.value = true }
  } finally {
    if (!cancelled) busy.value = false
  }
}, { immediate: true })

watch(() => form.value.transport, (val) => {
  if (val === 'rtu' || val === 'ascii') refreshSerialPorts()
})

async function refreshSerialPorts() {
  try {
    serialPorts.value = await invoke('list_serial_ports')
  } catch (e) { await showAlert(String(e)) }
}

async function pickFile(target: 'cert' | 'key' | 'ca' | 'pkcs12') {
  try {
    const path = await open({
      filters: target === 'pkcs12'
        ? [{ name: 'PKCS#12', extensions: ['p12', 'pfx'] }]
        : [{ name: 'PEM Certificate', extensions: ['pem', 'crt', 'key'] }],
    })
    if (!path) return
    const p = path as string
    if (target === 'cert') tlsCertFile.value = p
    else if (target === 'key') tlsKeyFile.value = p
    else if (target === 'ca') tlsCaFile.value = p
    else if (target === 'pkcs12') tlsPkcs12File.value = p
  } catch (e) { await showAlert(String(e)) }
}

async function submit() {
  if (busy.value || loadFailed.value) return
  error.value = ''
  if (!validCommunicationOptions(communication.value)) {
    await showAlert(t('errors.invalidCommunicationSettings'))
    return
  }
  const needsSerial = form.value.transport === 'rtu' || form.value.transport === 'ascii'
  const usesNetwork = form.value.transport === 'tcp' || form.value.transport === 'rtu_over_tcp'
  if (needsSerial && !serialPort.value) {
    await showAlert(t('errors.serialPortRequired'))
    return
  }
  if (usesNetwork && useSocks5.value && !socks5Host.value.trim()) {
    await showAlert(t('errors.socks5HostRequired'))
    return
  }
  if (usesNetwork && useSocks5.value && (!socks5Port.value || socks5Port.value > 65535)) {
    await showAlert(t('errors.invalidPort'))
    return
  }
  if (usesNetwork && useSocks5.value && (!!socks5Username.value !== !!socks5Password.value)) {
    await showAlert(t('errors.socks5CredentialsPair'))
    return
  }

  let transport: Record<string, unknown>
  if (form.value.transport === 'tcp') {
    transport = { type: 'tcp', host: form.value.target_address, port: form.value.port }
    if (useTls.value) transport = { type: 'tcp_tls', host: form.value.target_address, port: form.value.port }
  } else if (form.value.transport === 'rtu' || form.value.transport === 'ascii') {
    transport = {
      type: form.value.transport,
      serial_port: serialPort.value,
      baud_rate: baudRate.value,
      data_bits: dataBits.value,
      stop_bits: stopBits.value,
      parity: parityMode.value,
    }
  } else {
    transport = { type: 'rtu_over_tcp', host: form.value.target_address, port: form.value.port }
  }

  try {
    busy.value = true
    await invoke(props.connectionId ? 'update_master_connection' : 'create_master_connection', {
      ...(props.connectionId ? { connectionId: props.connectionId } : {}),
      request: {
        transport,
        slave_id: form.value.slave_id,
        timeout_ms: form.value.timeout_ms,
        requests: communication.value.requests,
        reconnect_policy: {
          ...communication.value.reconnect,
          max_attempts: communication.value.reconnect.max_attempts || null,
        },
        use_tls: form.value.transport === 'tcp' && useTls.value,
        ca_file: tlsCaFile.value || undefined,
        cert_file: tlsCertFile.value || undefined,
        key_file: tlsKeyFile.value || undefined,
        pkcs12_file: tlsPkcs12File.value || undefined,
        pkcs12_password: tlsPkcs12Password.value || undefined,
        accept_invalid_certs: tlsAcceptInvalidCerts.value || undefined,
        ...(usesNetwork ? {
          socks5: {
            enabled: useSocks5.value,
            host: socks5Host.value.trim(),
            port: socks5Port.value,
            username: socks5Username.value,
            password: socks5Password.value,
          },
        } : {}),
      }
    })
    emit('close')
    if (props.connectionId) emit('saved')
    else emit('created')
  } catch (e) { error.value = String(e) }
  finally { busy.value = false }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="modal-backdrop" @click.self="close">
      <div v-modal="close" class="modal-box" aria-labelledby="connection-dialog-title" :aria-busy="busy">
        <div id="connection-dialog-title" class="modal-title">{{ t(connectionId ? 'parity.connectionSettings' : 'toolbar.newConnection') }}</div>
        <p v-if="connectionId" class="form-hint">{{ t('dialog.editConnectionHint') }}</p>
        <form @submit.prevent="submit">
        <fieldset class="modal-body" :disabled="busy || loadFailed">
          <label class="form-label">
            {{ t('dialog.transport') }}
            <select v-model="form.transport" class="form-input">
              <option value="tcp">TCP</option>
              <option value="rtu">{{ t('dialog.rtuSerial') }}</option>
              <option value="ascii">{{ t('dialog.asciiSerial') }}</option>
              <option value="rtu_over_tcp">RTU over TCP</option>
            </select>
          </label>
          <template v-if="form.transport === 'tcp' || form.transport === 'rtu_over_tcp'">
            <label class="form-label">
              {{ t('dialog.targetAddress') }}
              <input v-model="form.target_address" class="form-input" type="text" placeholder="127.0.0.1" />
            </label>
            <label class="form-label">
              {{ t('dialog.port') }}
              <input v-model.number="form.port" class="form-input" type="number" min="1" max="65535" />
            </label>
            <label class="form-label checkbox-label">
              <input type="checkbox" v-model="useSocks5" /> {{ t('dialog.enableSocks5') }}
            </label>
            <template v-if="useSocks5">
              <label class="form-label">
                {{ t('dialog.socks5Host') }}
                <input v-model="socks5Host" class="form-input" type="text" placeholder="127.0.0.1" />
              </label>
              <label class="form-label">
                {{ t('dialog.socks5Port') }}
                <input v-model.number="socks5Port" class="form-input" type="number" min="1" max="65535" />
              </label>
              <label class="form-label">
                {{ t('dialog.socks5Username') }}
                <input v-model="socks5Username" class="form-input" type="text" :placeholder="t('dialog.optional')" autocomplete="username" />
              </label>
              <label class="form-label">
                {{ t('dialog.socks5Password') }}
                <input v-model="socks5Password" class="form-input" type="password" :placeholder="t('dialog.optional')" autocomplete="current-password" />
              </label>
              <div class="form-hint">{{ t('dialog.socks5AuthWarning') }}</div>
            </template>
          </template>
          <template v-if="form.transport === 'tcp'">
            <label class="form-label checkbox-label">
              <input type="checkbox" v-model="useTls" /> {{ t('dialog.enableTls') }}
            </label>
            <template v-if="useTls">
              <label class="form-label">
                {{ t('dialog.caFile') }}
                <div class="file-row">
                  <input v-model="tlsCaFile" class="form-input" type="text" :placeholder="t('dialog.caFilePlaceholder')" />
                  <button type="button" class="tool-btn" @click="pickFile('ca')">...</button>
                </div>
              </label>
              <label class="form-label">
                {{ t('dialog.clientCert') }}
                <div class="file-row">
                  <input v-model="tlsCertFile" class="form-input" type="text" :placeholder="t('dialog.clientCertPlaceholder')" />
                  <button type="button" class="tool-btn" @click="pickFile('cert')">...</button>
                </div>
              </label>
              <label class="form-label">
                {{ t('dialog.clientKey') }}
                <div class="file-row">
                  <input v-model="tlsKeyFile" class="form-input" type="text" :placeholder="t('dialog.clientCertPlaceholder')" />
                  <button type="button" class="tool-btn" @click="pickFile('key')">...</button>
                </div>
              </label>
              <label class="form-label">
                {{ t('dialog.pkcs12File') }}
                <div class="file-row">
                  <input v-model="tlsPkcs12File" class="form-input" type="text" :placeholder="t('dialog.pkcs12FilePlaceholder')" />
                  <button type="button" class="tool-btn" @click="pickFile('pkcs12')">...</button>
                </div>
              </label>
              <label class="form-label" v-if="tlsPkcs12File">
                {{ t('dialog.pkcs12Password') }}
                <input v-model="tlsPkcs12Password" class="form-input" type="password" :placeholder="t('dialog.passwordPlaceholder')" />
              </label>
              <label class="form-label">
                <input type="checkbox" v-model="tlsAcceptInvalidCerts" /> {{ t('dialog.acceptInvalidCerts') }}
              </label>
            </template>
          </template>
          <template v-if="form.transport === 'rtu' || form.transport === 'ascii'">
            <label class="form-label">
              {{ t('dialog.serialPort') }}
              <div class="file-row">
                <select v-model="serialPort" class="form-input">
                  <option v-if="serialPort && !serialPorts.some(p => p.name === serialPort)" :value="serialPort">{{ serialPort }}</option>
                  <option v-for="p in serialPorts" :key="p.name" :value="p.name">
                    {{ p.name }}{{ p.description ? ` (${p.description})` : '' }}
                  </option>
                </select>
                <button type="button" class="tool-btn" @click="refreshSerialPorts" :title="t('dialog.refreshSerialPorts')">&#x21bb;</button>
              </div>
            </label>
            <label class="form-label">
              {{ t('dialog.baudRate') }}
              <select v-model.number="baudRate" class="form-input">
                <option :value="9600">9600</option>
                <option :value="19200">19200</option>
                <option :value="38400">38400</option>
                <option :value="57600">57600</option>
                <option :value="115200">115200</option>
              </select>
            </label>
            <label class="form-label">
              {{ t('dialog.dataBits') }}
              <select v-model.number="dataBits" class="form-input">
                <option :value="7">7</option>
                <option :value="8">8</option>
              </select>
            </label>
            <label class="form-label">
              {{ t('dialog.stopBits') }}
              <select v-model.number="stopBits" class="form-input">
                <option :value="1">1</option>
                <option :value="2">2</option>
              </select>
            </label>
            <label class="form-label">
              {{ t('dialog.parity') }}
              <select v-model="parityMode" class="form-input">
                <option value="none">{{ t('dialog.parityNone') }}</option>
                <option value="odd">{{ t('dialog.parityOdd') }}</option>
                <option value="even">{{ t('dialog.parityEven') }}</option>
              </select>
            </label>
          </template>
          <label class="form-label">
            {{ t('dialog.slaveId') }}
            <input v-model.number="form.slave_id" class="form-input" type="number" min="1" max="247" />
          </label>
          <label class="form-label">
            {{ t('dialog.communicationTimeout') }}
            <input v-model.number="form.timeout_ms" class="form-input" type="number" min="100" max="30000" aria-describedby="communication-timeout-hint" />
          </label>
          <div id="communication-timeout-hint" class="form-hint timeout-hint">{{ t('dialog.communicationTimeoutHint') }}</div>
          <CommunicationSettings v-model="communication" />
        </fieldset>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="busy || loadFailed">{{ t(connectionId ? 'common.save' : 'common.create') }}</button>
        </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { box-sizing: border-box; background: #1e1e2e; border: 1px solid #45475a; border-radius: 8px; padding: 20px; width: 400px; max-width: calc(100vw - 32px); max-height: calc(100vh - 32px); overflow-y: auto; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 15px; font-weight: 600; color: #cdd6f4; margin-bottom: 16px; }
.modal-body { border: 0; padding: 0; margin: 0; min-width: 0; display: flex; flex-direction: column; gap: 12px; }
.form-error { color: var(--c-red); white-space: pre-wrap; overflow-wrap: anywhere; }
button:disabled { opacity: 0.4; cursor: default; }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.form-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--c-subtext0); }
.checkbox-label { flex-direction: row; align-items: center; }
.form-hint { margin-top: -6px; color: #a6adc8; font-size: 11px; line-height: 1.4; }
.timeout-hint { max-width: 300px; font-size: 12px; line-height: 1.5; }
.form-input { padding: 6px 10px; background: #313244; border: 1px solid #45475a; border-radius: 4px; color: #cdd6f4; font-size: 13px; }
.form-input:focus { outline: none; border-color: #89b4fa; }
.file-row { display: flex; gap: 4px; }
.file-row > input, .file-row > select { flex: 1; min-width: 0; }
.tool-btn { padding: 4px 8px; background: #313244; border: 1px solid #45475a; border-radius: 4px; color: #cdd6f4; cursor: pointer; font-size: 14px; }
.tool-btn:hover { background: #45475a; }
.btn { padding: 7px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-primary { background: #89b4fa; color: #1e1e2e; }
.btn-primary:hover { background: #74c7ec; }
.btn-secondary { background: #45475a; color: #cdd6f4; }
.btn-secondary:hover { background: #585b70; }
</style>

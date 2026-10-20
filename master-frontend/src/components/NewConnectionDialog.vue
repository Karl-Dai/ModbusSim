<script setup lang="ts">
import { ref, watch } from 'vue'
import * as App from '../../wailsjs/go/main/App'
import type { masterstate, project } from '../../wailsjs/go/models'

import { PathField, showAlert, useI18n, vModal } from 'shared-frontend'
import CommunicationSettings from './CommunicationSettings.vue'
import { defaultCommunicationOptions, validCommunicationOptions } from '../communication'

const { t } = useI18n()

interface Props { show: boolean; connectionId?: string }
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
    const settings = await App.GetSettings(connectionId)
    if (cancelled) return
    const transport = settings.Transport
    form.value = { transport: transport.Type === 'tcp_tls' ? 'tcp' : transport.Type,
      target_address: transport.Host ?? '', port: transport.Port ?? 502,
      slave_id: settings.SlaveID, timeout_ms: settings.TimeoutMS }
    communication.value = { requests: settings.Requests, reconnect: { ...settings.Reconnect, MaxAttempts: settings.Reconnect.MaxAttempts ?? 0 } }
    if (transport.Serial) { const sc = transport.Serial; serialPort.value = sc.Port; baudRate.value = sc.BaudRate; dataBits.value = sc.DataBits; stopBits.value = sc.StopBits; parityMode.value = sc.Parity }
    useTls.value = transport.Type === 'tcp_tls'
    const tls = transport.TLS
    tlsCaFile.value = tls?.CAFile ?? ''; tlsCertFile.value = tls?.CertFile ?? ''; tlsKeyFile.value = tls?.KeyFile ?? ''
    tlsPkcs12File.value = tls?.PKCS12File ?? ''; tlsPkcs12Password.value = tls?.PKCS12Password ?? ''; tlsAcceptInvalidCerts.value = tls?.AcceptInvalidCerts ?? false
    const proxy = settings.Socks5
    useSocks5.value = !!proxy; socks5Host.value = proxy?.Host ?? '127.0.0.1'; socks5Port.value = proxy?.Port ?? 1080
    socks5Username.value = proxy?.Username ?? ''; socks5Password.value = proxy?.Password ?? ''

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
    serialPorts.value = (await App.ListSerialPorts()).map(name => ({ name, description: '', manufacturer: '' }))
  } catch (e) { await showAlert(String(e)) }
}

async function pickFile(target: 'cert' | 'key' | 'ca' | 'pkcs12') {
  try {
    const path = await App.OpenFileDialog(target === 'pkcs12'
      ? [{ Name: 'PKCS#12', Extensions: ['p12', 'pfx'] }]
      : [{ Name: 'PEM Certificate', Extensions: ['pem', 'crt', 'key'] }])
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

  const transport: project.Transport = needsSerial
    ? { Type: form.value.transport, Serial: { Port: serialPort.value, BaudRate: baudRate.value, DataBits: dataBits.value, StopBits: stopBits.value, Parity: parityMode.value } }
    : { Type: form.value.transport === 'tcp' && useTls.value ? 'tcp_tls' : form.value.transport, Host: form.value.target_address.trim(), Port: form.value.port }
  if (transport.Type === 'tcp_tls') transport.TLS = { Enabled: true, CAFile: tlsCaFile.value, CertFile: tlsCertFile.value, KeyFile: tlsKeyFile.value, PKCS12File: tlsPkcs12File.value, PKCS12Password: tlsPkcs12Password.value, AcceptInvalidCerts: tlsAcceptInvalidCerts.value }
  const settings: masterstate.Settings = { Transport: transport, SlaveID: form.value.slave_id, TimeoutMS: form.value.timeout_ms,
    Requests: communication.value.requests, Reconnect: { ...communication.value.reconnect, MaxAttempts: communication.value.reconnect.MaxAttempts || undefined },
    Socks5: usesNetwork && useSocks5.value ? { Host: socks5Host.value.trim(), Port: socks5Port.value, Username: socks5Username.value, Password: socks5Password.value } : undefined }
  try {
    busy.value = true
    if (props.connectionId) await App.UpdateConnection(props.connectionId, settings)
    else await App.CreateConnection(settings)
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
              <label class="form-label checkbox-label">
                <input type="checkbox" v-model="tlsAcceptInvalidCerts" aria-describedby="tls-compatibility-hint" /> {{ t('dialog.acceptInvalidCerts') }}
              </label>
              <div id="tls-compatibility-hint" class="form-hint">{{ t('dialog.tlsCompatibilityHint') }}</div>
              <div class="form-label">
                {{ t('dialog.caFile') }}
                <PathField v-model="tlsCaFile" :placeholder="t('dialog.caFilePlaceholder')" @browse="pickFile('ca')" />
              </div>
              <div class="form-label">
                {{ t('dialog.clientCert') }}
                <PathField v-model="tlsCertFile" :placeholder="t('dialog.clientCertPlaceholder')" @browse="pickFile('cert')" />
              </div>
              <div class="form-label">
                {{ t('dialog.clientKey') }}
                <PathField v-model="tlsKeyFile" :placeholder="t('dialog.clientCertPlaceholder')" @browse="pickFile('key')" />
              </div>
              <div class="form-label">
                {{ t('dialog.pkcs12File') }}
                <PathField v-model="tlsPkcs12File" :placeholder="t('dialog.pkcs12FilePlaceholder')" @browse="pickFile('pkcs12')" />
              </div>
              <label class="form-label" v-if="tlsPkcs12File">
                {{ t('dialog.pkcs12Password') }}
                <input v-model="tlsPkcs12Password" class="form-input" type="password" :placeholder="t('dialog.passwordPlaceholder')" />
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
.modal-box { box-sizing: border-box; background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; padding: 20px; width: 400px; max-width: calc(100vw - 32px); max-height: calc(100vh - 32px); overflow-y: auto; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 15px; font-weight: 600; color: var(--c-text); margin-bottom: 16px; }
.modal-body { border: 0; padding: 0; margin: 0; min-width: 0; display: flex; flex-direction: column; gap: 12px; }
.form-error { color: var(--c-red); white-space: pre-wrap; overflow-wrap: anywhere; }
button:disabled { opacity: 0.4; cursor: default; }
.modal-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
.form-label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--c-subtext0); }
.checkbox-label { flex-direction: row; align-items: center; }
.form-hint { margin-top: -6px; color: var(--c-subtext0); font-size: 11px; line-height: 1.4; }
.timeout-hint { max-width: 300px; font-size: 12px; line-height: 1.5; }
.form-input { padding: 6px 10px; background: var(--c-control-bg); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 13px; }
.form-input:focus { outline: none; border-color: var(--c-blue); }
.file-row { display: flex; gap: 4px; }
.file-row > input, .file-row > select { flex: 1; min-width: 0; }
.tool-btn { padding: 4px 8px; background: var(--c-control-bg); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); cursor: pointer; font-size: 14px; }
.tool-btn:hover { background: var(--c-surface1); }
.btn { padding: 7px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-primary { background: var(--c-blue); color: var(--c-on-accent); }
.btn-primary:hover { background: var(--c-accent-hover); }
.btn-secondary { background: var(--c-neutral-bg); color: var(--c-text); }
.btn-secondary:hover { background: var(--c-hover-bg); }
</style>

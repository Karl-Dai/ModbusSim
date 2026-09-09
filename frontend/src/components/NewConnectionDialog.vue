<script setup lang="ts">
import { vModal } from 'shared-frontend'
import { ref, watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { open } from '@tauri-apps/plugin-dialog'
import { useI18n, showAlert } from 'shared-frontend'

const { t } = useI18n()

interface Props { show: boolean }
const props = defineProps<Props>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created'): void }>()

const port = ref('5020')
const slaveId = ref('1')
const slaveName = ref('')
const initMode = ref('zero')
const transport = ref('tcp')
const serialPort = ref('')
const baudRate = ref(9600)
const dataBits = ref(8)
const stopBits = ref(1)
const parityMode = ref('none')
const serialPorts = ref<{ name: string; description: string; manufacturer: string }[]>([])

const useTls = ref(false)
const tlsCertFile = ref('')
const tlsKeyFile = ref('')
const tlsCaFile = ref('')
const tlsRequireClientCert = ref(false)
const tlsPkcs12File = ref('')
const tlsPkcs12Password = ref('')

watch(() => props.show, (visible) => {
  if (!visible) return
  port.value = '5020'
  slaveId.value = '1'
  slaveName.value = ''
  initMode.value = 'zero'
  transport.value = 'tcp'
  serialPort.value = ''
  baudRate.value = 9600
  dataBits.value = 8
  stopBits.value = 1
  parityMode.value = 'none'
  useTls.value = false
  tlsCertFile.value = ''
  tlsKeyFile.value = ''
  tlsCaFile.value = ''
  tlsRequireClientCert.value = false
  tlsPkcs12File.value = ''
  tlsPkcs12Password.value = ''
})

watch(transport, (val) => {
  if (val === 'rtu' || val === 'ascii') refreshSerialPorts()
})

async function refreshSerialPorts() {
  try {
    serialPorts.value = await invoke('list_serial_ports')
  } catch (e) {
    await showAlert(String(e))
  }
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
  } catch (e) {
    await showAlert(String(e))
  }
}

async function submit() {
  const portNum = Number(port.value)
  const initialSlaveId = Number(slaveId.value)
  const needsPort = transport.value === 'tcp' || transport.value === 'rtu_over_tcp'
  const needsSerial = transport.value === 'rtu' || transport.value === 'ascii'

  if (needsPort && (!portNum || portNum < 1 || portNum > 65535)) {
    await showAlert(t('errors.invalidPort'))
    return
  }
  if (needsSerial && !serialPort.value) {
    await showAlert(t('errors.serialPortRequired'))
    return
  }
  if (!initialSlaveId || initialSlaveId < 1 || initialSlaveId > 247) {
    await showAlert(t('errors.invalidSlaveId'))
    return
  }

  let transportPayload: Record<string, unknown>
  if (transport.value === 'tcp') {
    transportPayload = useTls.value ? { type: 'tcp_tls', port: portNum } : { type: 'tcp', port: portNum }
  } else if (transport.value === 'rtu' || transport.value === 'ascii') {
    transportPayload = {
      type: transport.value,
      serial_port: serialPort.value,
      baud_rate: baudRate.value,
      data_bits: dataBits.value,
      stop_bits: stopBits.value,
      parity: parityMode.value,
    }
  } else {
    transportPayload = { type: 'rtu_over_tcp', host: '0.0.0.0', port: portNum }
  }

  try {
    await invoke('create_slave_connection', {
      request: {
        transport: transportPayload,
        init_mode: initMode.value,
        slave_id: initialSlaveId,
        name: slaveName.value.trim(),
        ...(useTls.value ? {
          use_tls: true,
          cert_file: tlsCertFile.value || undefined,
          key_file: tlsKeyFile.value || undefined,
          ca_file: tlsCaFile.value || undefined,
          require_client_cert: tlsRequireClientCert.value || undefined,
          pkcs12_file: tlsPkcs12File.value || undefined,
          pkcs12_password: tlsPkcs12Password.value || undefined,
        } : {}),
      }
    })
    emit('created')
    emit('close')
  } catch (e) {
    await showAlert(String(e))
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" v-modal="() => emit('close')" class="modal-overlay" @click.self="emit('close')">
      <div class="modal-box">
        <div class="modal-title">{{ t('toolbar.newConnection') }}</div>
        <div class="modal-field">
          <label>{{ t('dialog.transport') }}</label>
          <select v-model="transport" class="form-select">
            <option value="tcp">TCP</option>
            <option value="rtu">{{ t('dialog.rtuSerial') }}</option>
            <option value="ascii">{{ t('dialog.asciiSerial') }}</option>
            <option value="rtu_over_tcp">RTU over TCP</option>
          </select>
        </div>
        <template v-if="transport === 'tcp' || transport === 'rtu_over_tcp'">
          <div class="modal-field">
            <label>{{ t('dialog.portNumber') }}</label>
            <input v-model="port" type="number" min="1" max="65535" @keyup.enter="submit" />
          </div>
        </template>
        <template v-if="transport === 'tcp'">
          <div class="modal-field">
            <label>
              <input type="checkbox" v-model="useTls" /> {{ t('dialog.enableTls') }}
            </label>
          </div>
          <template v-if="useTls">
            <div class="modal-field">
              <label>{{ t('dialog.serverCert') }}</label>
              <div class="file-row">
                <input v-model="tlsCertFile" type="text" :placeholder="t('dialog.caFilePlaceholder')" />
                <button class="tool-btn" @click="pickFile('cert')">...</button>
              </div>
            </div>
            <div class="modal-field">
              <label>{{ t('dialog.serverKey') }}</label>
              <div class="file-row">
                <input v-model="tlsKeyFile" type="text" :placeholder="t('dialog.caFilePlaceholder')" />
                <button class="tool-btn" @click="pickFile('key')">...</button>
              </div>
            </div>
            <div class="modal-field">
              <label>{{ t('dialog.pkcs12File') }}</label>
              <div class="file-row">
                <input v-model="tlsPkcs12File" type="text" :placeholder="t('dialog.pkcs12FilePlaceholder')" />
                <button class="tool-btn" @click="pickFile('pkcs12')">...</button>
              </div>
            </div>
            <div class="modal-field" v-if="tlsPkcs12File">
              <label>{{ t('dialog.pkcs12Password') }}</label>
              <input v-model="tlsPkcs12Password" type="password" :placeholder="t('dialog.passwordPlaceholder')" />
            </div>
            <div class="modal-field">
              <label>
                <input type="checkbox" v-model="tlsRequireClientCert" /> {{ t('dialog.requireClientCert') }}
              </label>
            </div>
            <div class="modal-field" v-if="tlsRequireClientCert">
              <label>{{ t('dialog.caFileClient') }}</label>
              <div class="file-row">
                <input v-model="tlsCaFile" type="text" :placeholder="t('dialog.caFilePlaceholder')" />
                <button class="tool-btn" @click="pickFile('ca')">...</button>
              </div>
            </div>
          </template>
        </template>
        <template v-if="transport === 'rtu' || transport === 'ascii'">
          <div class="modal-field">
            <label>{{ t('dialog.serialPort') }}</label>
            <div class="file-row">
              <select v-model="serialPort" class="form-select">
                <option v-for="p in serialPorts" :key="p.name" :value="p.name">
                  {{ p.name }}{{ p.description ? ` (${p.description})` : '' }}
                </option>
              </select>
              <button class="tool-btn" @click="refreshSerialPorts" :title="t('dialog.refreshSerialPorts')">&#x21bb;</button>
            </div>
          </div>
          <div class="modal-field">
            <label>{{ t('dialog.baudRate') }}</label>
            <select v-model.number="baudRate" class="form-select">
              <option :value="9600">9600</option>
              <option :value="19200">19200</option>
              <option :value="38400">38400</option>
              <option :value="57600">57600</option>
              <option :value="115200">115200</option>
            </select>
          </div>
          <div class="modal-field">
            <label>{{ t('dialog.dataBits') }}</label>
            <select v-model.number="dataBits" class="form-select">
              <option :value="7">7</option>
              <option :value="8">8</option>
            </select>
          </div>
          <div class="modal-field">
            <label>{{ t('dialog.stopBits') }}</label>
            <select v-model.number="stopBits" class="form-select">
              <option :value="1">1</option>
              <option :value="2">2</option>
            </select>
          </div>
          <div class="modal-field">
            <label>{{ t('dialog.parity') }}</label>
            <select v-model="parityMode" class="form-select">
              <option value="none">{{ t('dialog.parityNone') }}</option>
              <option value="odd">{{ t('dialog.parityOdd') }}</option>
              <option value="even">{{ t('dialog.parityEven') }}</option>
            </select>
          </div>
        </template>
        <div class="modal-field">
          <label>{{ t('dialog.slaveId') }}</label>
          <input v-model="slaveId" type="number" min="1" max="247" />
        </div>
        <div class="modal-field">
          <label>{{ t('dialog.slaveName') }}</label>
          <input v-model="slaveName" type="text" :placeholder="t('dialog.slaveNamePlaceholder')" />
        </div>
        <div class="modal-field">
          <label>{{ t('dialog.initValue') }}</label>
          <div class="radio-group">
            <label class="radio-label">
              <input type="radio" v-model="initMode" value="zero" /> {{ t('dialog.initZero') }}
            </label>
            <label class="radio-label">
              <input type="radio" v-model="initMode" value="random" /> {{ t('dialog.initRandom') }}
            </label>
          </div>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button class="modal-btn confirm" @click="submit">{{ t('common.ok') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal-box { background: var(--c-base); border: 1px solid var(--c-surface1); border-radius: 8px; padding: 20px; min-width: 300px; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
.modal-title { font-size: 14px; font-weight: 600; color: var(--c-text); margin-bottom: 16px; }
.modal-field { margin-bottom: 14px; }
.modal-field label { display: block; font-size: 12px; color: var(--c-subtext0); margin-bottom: 6px; }
.modal-field input[type="number"], .modal-field input[type="text"], .modal-field input[type="password"] {
  width: 100%; padding: 6px 10px; background: var(--c-surface0); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 13px; outline: none;
}
.modal-field input:focus { border-color: var(--c-blue); }
.form-select { width: 100%; padding: 6px 10px; background: var(--c-surface0); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); font-size: 13px; outline: none; }
.form-select:focus { border-color: var(--c-blue); }
.file-row { display: flex; gap: 4px; }
.file-row > input, .file-row > select { flex: 1; }
.tool-btn { padding: 4px 8px; background: var(--c-surface0); border: 1px solid var(--c-surface1); border-radius: 4px; color: var(--c-text); cursor: pointer; font-size: 14px; }
.tool-btn:hover { background: var(--c-surface1); }
.radio-group { display: flex; gap: 16px; }
.radio-label { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--c-text); cursor: pointer; }
.radio-label input[type="radio"] { accent-color: var(--c-blue); }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
.modal-btn { padding: 6px 16px; border: none; border-radius: 4px; font-size: 12px; cursor: pointer; }
.modal-btn.cancel { background: var(--c-surface0); color: var(--c-subtext0); }
.modal-btn.cancel:hover { background: var(--c-surface1); }
.modal-btn.confirm { background: var(--c-blue); color: var(--c-base); font-weight: 600; }
.modal-btn.confirm:hover { background: var(--c-sapphire); }
</style>

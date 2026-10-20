import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, h, ref, type Ref } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import PathField from '../src/components/PathField.vue'

const LONG_PATH = '/Users/daichangyu/Library/Mobile Documents/com~apple~CloudDocs/certs/server.pem'

// Mirrors how the dialogs bind the field with v-model.
let wrapper: VueWrapper | undefined
let value: Ref<string>

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

function mountField(initial: string) {
  value = ref(initial)
  const Host = defineComponent({
    setup: () => () => h(PathField, {
      modelValue: value.value,
      placeholder: '选择证书',
      'onUpdate:modelValue': (next: string) => { value.value = next },
    }),
  })
  wrapper = mount(Host)
  return wrapper
}

describe('PathField', () => {
  it('shows the end of a long path and keeps the full path in the tooltip', () => {
    mountField(LONG_PATH)
    const display = wrapper!.get('.path-display')
    expect(display.text()).toBe('…/certs/server.pem')
    expect(display.attributes('title')).toBe(LONG_PATH)
  })

  it('keeps short paths intact and shows the placeholder when empty', async () => {
    mountField('/etc/ssl/cert.pem')
    expect(wrapper!.get('.path-display').text()).toBe('/etc/ssl/cert.pem')
    value.value = ''
    await wrapper!.vm.$nextTick()
    const display = wrapper!.get('.path-display')
    expect(display.text()).toBe('选择证书')
    expect(display.classes()).toContain('is-empty')
  })

  it('opens the full path for editing and commits the trimmed value on Enter', async () => {
    mountField(LONG_PATH)
    await wrapper!.get('.path-display').trigger('click')
    const input = wrapper!.get<HTMLInputElement>('.path-input')
    expect(input.element.value).toBe(LONG_PATH)
    await input.setValue('  /etc/ssl/client.pem  ')
    await input.trigger('keydown', { key: 'Enter' })
    expect(value.value).toBe('/etc/ssl/client.pem')
    expect(wrapper!.get('.path-display').text()).toBe('/etc/ssl/client.pem')
  })

  it('commits on blur and discards the draft on Escape', async () => {
    mountField('/etc/ssl/a.pem')
    await wrapper!.get('.path-display').trigger('click')
    await wrapper!.get('.path-input').setValue('/etc/ssl/b.pem')
    await wrapper!.get('.path-input').trigger('blur')
    expect(value.value).toBe('/etc/ssl/b.pem')

    await wrapper!.get('.path-display').trigger('click')
    await wrapper!.get('.path-input').setValue('/etc/ssl/c.pem')
    await wrapper!.get('.path-input').trigger('keydown', { key: 'Escape' })
    expect(value.value).toBe('/etc/ssl/b.pem')
    expect(wrapper!.get('.path-display').text()).toBe('/etc/ssl/b.pem')
  })

  it('emits browse when the browse button is pressed', async () => {
    mountField('')
    await wrapper!.get('.path-browse').trigger('click')
    expect(wrapper!.findComponent(PathField).emitted('browse')).toHaveLength(1)
  })
})

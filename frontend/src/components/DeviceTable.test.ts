import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { Device } from '@/types'
import DeviceTable from './DeviceTable.vue'

const device: Device = {
  id: 'd1', name: '猫盒-01', hostname: 'lc-01', osVersion: 'LazyCat OS', collectorVersion: '1.0.0',
  status: 'active', lastSeenAt: new Date().toISOString(), online: true, stale: false, health: 'healthy', latest: {},
}

describe('DeviceTable', () => {
  it('uses a real button as the keyboard entry point', async () => {
    const onSelect = vi.fn()
    const wrapper = mount(DeviceTable, { props: { items: [device], clickable: true, onSelect } })
    const row = wrapper.get('.device-inventory-item')
    const button = wrapper.get('button.row-link')

    expect(row.attributes('tabindex')).toBeUndefined()
    expect(button.text()).toBe(device.name)
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    expect(wrapper.find('.device-mark').exists()).toBe(false)

    await button.trigger('click')
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith(device.id)
  })

  it('keeps the full row pointer target without duplicate callbacks', async () => {
    const onSelect = vi.fn()
    const wrapper = mount(DeviceTable, { props: { items: [device], clickable: true, onSelect } })
    await wrapper.get('.device-inventory-item').trigger('click')
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith(device.id)
  })
})

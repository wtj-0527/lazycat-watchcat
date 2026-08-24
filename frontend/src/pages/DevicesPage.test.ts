import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Device, Overview } from '@/types'
import DevicesPage from './DevicesPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

const device: Device = {
  id: 'd1', name: '猫盒-01', hostname: 'lc-01', osVersion: 'LazyCat OS', collectorVersion: '1.8.0',
  status: 'active', lastSeenAt: new Date().toISOString(), online: true, stale: false, health: 'healthy', latest: {},
}
const overview: Overview = { stats: { devices: 1 }, devices: [device], alerts: [], updatedAt: device.lastSeenAt }

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callback(0)
    return 1
  })
  apiMock.mockImplementation(async (path: string) => path === '/api/v1/overview' ? overview : device)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('DevicesPage detail tabs', () => {
  it('uses one unified view and filter toolbar without duplicate guidance', async () => {
    apiMock.mockImplementation(async (path: string) => path === '/api/v1/overview'
      ? { ...overview, savedViews: [{ id: 'mine', name: '我的关注', query: { status: 'attention' } }] }
      : device)

    const wrapper = mount(DevicesPage)
    await flushPromises()

    expect(wrapper.text()).not.toContain('用筛选和保存视图快速缩小范围')
    expect(wrapper.find('.saved-views').exists()).toBe(false)
    expect(wrapper.findAll('.filter-bar')).toHaveLength(1)
    expect(wrapper.get('[aria-label="选择视图"]').text()).toContain('我的关注')

    await wrapper.get('[aria-label="选择视图"]').setValue('attention')
    expect(wrapper.get('[aria-label="健康状态"]').element).toHaveProperty('value', 'attention')
    expect(wrapper.text()).toContain('没有符合当前筛选条件的设备')

    await wrapper.get('[aria-label="健康状态"]').setValue('healthy')
    expect(wrapper.get('[aria-label="选择视图"]').element).toHaveProperty('value', 'custom')
    wrapper.unmount()
  })

  it('exposes tab semantics and keyboard navigation after opening a device', async () => {
    const wrapper = mount(DevicesPage, { attachTo: document.body })
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs).toHaveLength(6)
    for (const item of tabs) {
      expect(document.getElementById(item.attributes('aria-controls')!)).not.toBeNull()
    }
    expect(wrapper.get('#device-tab-overview').attributes('aria-selected')).toBe('true')

    await wrapper.get('#device-tab-overview').trigger('keydown', { key: 'ArrowLeft' })
    expect(wrapper.get('#device-tab-events').attributes('aria-selected')).toBe('true')
    expect(document.activeElement?.id).toBe('device-tab-events')
    expect(wrapper.get('[role="tabpanel"]').attributes('aria-labelledby')).toBe('device-tab-events')

    await wrapper.get('#device-tab-events').trigger('keydown', { key: 'Home' })
    expect(wrapper.get('#device-tab-overview').attributes('tabindex')).toBe('0')
    wrapper.unmount()
  })

  it('retries the original device id after the initial detail request fails', async () => {
    let detailRequests = 0
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/overview') return overview
      if (path === '/api/v1/devices/d1') {
        detailRequests++
        if (detailRequests === 1) throw new Error('temporary detail failure')
      }
      if (path.includes('/metrics')) return { items: [] }
      if (path.includes('/events')) return { items: [] }
      if (path === '/api/v1/operations') return { capabilities: [] }
      return device
    })

    const wrapper = mount(DevicesPage)
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('数据加载失败')
    await wrapper.get('.error-state button').trigger('click')
    await flushPromises()

    expect(detailRequests).toBe(2)
    expect(wrapper.text()).toContain('猫盒-01')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })
})

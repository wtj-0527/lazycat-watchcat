import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Device, Overview } from '@/types'
import DevicesPage from './DevicesPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
const confirmMock = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/api', () => ({ api: apiMock }))
vi.mock('@/dialog', () => ({ appConfirm: confirmMock, appPrompt: vi.fn(async () => null) }))

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

  it('shows charts instead of raw metric tables and supports a custom trend range', async () => {
    const collectedAt = '2026-08-25T10:00:00Z'
    const detailed = {
      ...device,
      latest: {
        'system.cpu.usage': [{ name: 'system.cpu.usage', value: 18, unit: '%', labels: {}, collectedAt }],
        'system.memory.usage': [{ name: 'system.memory.usage', value: 42, unit: '%', labels: {}, collectedAt }],
        'system.load.1m': [{ name: 'system.load.1m', value: 1.5, unit: '', labels: {}, collectedAt }],
        'system.temperature': [{ name: 'system.temperature', value: 50, unit: 'celsius', labels: { sensor: 'cpu' }, collectedAt }],
      },
    }
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/overview') return overview
      if (path === '/api/v1/devices/d1') return detailed
      if (path.includes('/events')) return { items: [{ id: 'e1', type: 'alert.opened', title: '告警触发', detail: {}, createdAt: collectedAt }] }
      if (path === '/api/v1/operations') return { capabilities: [] }
      if (path.includes('/metrics')) return { items: [{ name: 'system.cpu.usage', value: 18, unit: '%', labels: {}, collectedAt }] }
      return detailed
    })

    const wrapper = mount(DevicesPage)
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()

    expect(apiMock.mock.calls.some(([path]) => String(path).includes('hours=24'))).toBe(true)
    const overviewText = wrapper.get('.device-overview-grid').text()
    expect(overviewText.indexOf('资源趋势')).toBeLessThan(overviewText.indexOf('活动风险'))

    await wrapper.get('#device-tab-system').trigger('click')
    expect(wrapper.find('.raw-metrics').exists()).toBe(false)
    expect(wrapper.findAll('.metric-chart-panel').length).toBeGreaterThan(0)

    await wrapper.get('#device-tab-overview').trigger('click')
    await wrapper.findAll('.range-tabs button').find((button) => button.text() === '自定义')!.trigger('click')
    const inputs = wrapper.findAll('.device-trend-custom-range input')
    await inputs[0].setValue('2026-08-23T00:00')
    await inputs[1].setValue('2026-08-24T00:00')
    await wrapper.get('.device-trend-custom-range button').trigger('click')
    await flushPromises()

    expect(apiMock.mock.calls.some(([path]) => String(path).includes('&from=') && String(path).includes('&to='))).toBe(true)
    wrapper.unmount()
  })

  it('uses a bubble distribution, status donut and resource matrix for container metrics', async () => {
    const collectedAt = '2026-08-26T10:00:00Z'
    const labels = { app: 'cloud.lazycat.app.photos', container: 'abc123', name: 'photos-main', state: 'running' }
    const detailed = {
      ...device,
      latest: {
        'container.running': [{ name: 'container.running', value: 1, unit: 'bool', labels, collectedAt }],
        'container.cpu.usage': [{ name: 'container.cpu.usage', value: 28, unit: '%', labels, collectedAt }],
        'container.memory.usage': [{ name: 'container.memory.usage', value: 1024 ** 3, unit: 'bytes', labels, collectedAt }],
        'container.memory.limit': [{ name: 'container.memory.limit', value: 2 * 1024 ** 3, unit: 'bytes', labels, collectedAt }],
        'container.network.receive.bytes_total': [{ name: 'container.network.receive.bytes_total', value: 2048, unit: 'bytes', labels, collectedAt }],
        'container.block.write.bytes_total': [{ name: 'container.block.write.bytes_total', value: 4096, unit: 'bytes', labels, collectedAt }],
      },
    }
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/overview') return overview
      if (path === '/api/v1/devices/d1') return detailed
      if (path === '/api/v1/applications') return { items: [{ id: 'cloud.lazycat.app.photos', title: '懒猫相册' }] }
      if (path.includes('/events') || path.includes('/metrics')) return { items: [] }
      if (path === '/api/v1/operations') return { capabilities: [] }
      return detailed
    })

    const wrapper = mount(DevicesPage)
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()
    await wrapper.get('#device-tab-apps').trigger('click')

    expect(wrapper.find('.resource-bubble-chart').exists()).toBe(true)
    expect(wrapper.find('.application-status-panel .donut-chart').exists()).toBe(true)
    expect(wrapper.get('.application-resource-matrix').text()).toContain('懒猫相册')
    expect(wrapper.get('.application-resource-matrix').text()).toContain('photos-main')
    expect(wrapper.find('.device-metric-chart-grid').exists()).toBe(false)
    wrapper.unmount()
  })

  it('deletes a remote device after confirmation and hides deletion for the local device', async () => {
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/overview') return overview
      if (path === '/api/v1/devices/d1' && options?.method === 'DELETE') return undefined
      if (path === '/api/v1/devices/d1') return device
      if (path.includes('/metrics') || path.includes('/events')) return { items: [] }
      if (path === '/api/v1/operations') return { capabilities: [] }
      return device
    })
    const wrapper = mount(DevicesPage)
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()
    expect(wrapper.get('.danger-button').text()).toContain('双向彻底移除')
    await wrapper.get('.danger-button').trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalledOnce()
    expect(apiMock).toHaveBeenCalledWith('/api/v1/devices/d1', { method: 'DELETE' })
    expect(wrapper.text()).toContain('设备清单')
    wrapper.unmount()

    apiMock.mockImplementation(async (path: string) => path === '/api/v1/overview' ? overview : { ...device, local: true })
    const localWrapper = mount(DevicesPage)
    await flushPromises()
    await localWrapper.get('button.row-link').trigger('click')
    await flushPromises()
    expect(localWrapper.find('.danger-button').exists()).toBe(false)
    localWrapper.unmount()
  })
})

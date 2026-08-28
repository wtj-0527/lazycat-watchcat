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
    expect(wrapper.find('.device-list-section').exists()).toBe(true)
    expect(wrapper.get('.device-list-section').classes()).not.toContain('card')
    expect(wrapper.text()).not.toContain('设备清单')

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
    expect(tabs).toHaveLength(7)
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
      if (path.includes('/events')) return { items: [{ id: 'e1', type: 'alert', title: '告警触发', detail: { severity: 'critical' }, createdAt: collectedAt }] }
      if (path === '/api/v1/operations') return { capabilities: [] }
      if (path.includes('/metrics')) {
        const name = new URL(`https://watchcat.test${path}`).searchParams.get('name') || ''
        const counter = name.includes('operations') ? 120 : name.includes('bytes_total') ? 10 * 1024 ** 2 : 18
        const unit = name.includes('operations') ? 'count' : name.includes('bytes_total') ? 'bytes' : '%'
        return { items: [
          { name, value: counter, unit, labels: { device: 'sda', interface: 'eth0' }, collectedAt: '2026-08-25T09:59:30Z' },
          { name, value: counter + (name.includes('operations') ? 60 : name.includes('bytes_total') ? 30 * 1024 ** 2 : 0), unit, labels: { device: 'sda', interface: 'eth0' }, collectedAt },
        ] }
      }
      return detailed
    })

    const wrapper = mount(DevicesPage)
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()

    expect(apiMock.mock.calls.some(([path]) => String(path).includes('hours=24'))).toBe(true)
    const overviewText = wrapper.get('.device-overview-grid').text()
    expect(overviewText.indexOf('磁盘 I/O 趋势')).toBeLessThan(overviewText.indexOf('活动风险'))
    expect(wrapper.get('.resource-trend-card').text()).toContain('磁盘吞吐')
    expect(wrapper.get('.resource-trend-card').text()).toContain('磁盘 IOPS')
    expect(wrapper.get('.resource-trend-card').text()).not.toContain('网络吞吐')
    expect(wrapper.find('.resource-trend-usage').exists()).toBe(false)
    expect(wrapper.findAll('.resource-throughput-grid .line-chart')).toHaveLength(2)
    expect(wrapper.findAll('.resource-throughput-legend')).toHaveLength(2)
    expect(wrapper.findAll('.resource-throughput-grid .chart-legend')).toHaveLength(0)
    expect(apiMock.mock.calls.some(([path]) => String(path).includes('name=disk.io.read.bytes_total'))).toBe(true)
    expect(apiMock.mock.calls.some(([path]) => String(path).includes('name=disk.io.write.operations_total'))).toBe(true)

    await wrapper.get('#device-tab-system').trigger('click')
    expect(wrapper.find('.raw-metrics').exists()).toBe(false)
    expect(wrapper.get('.device-detail-insights').classes()).not.toContain('card')
    expect(wrapper.find('.device-detail-insights > .section-title').exists()).toBe(false)
    expect(wrapper.findAll('.detail-chart-card').length).toBeGreaterThan(0)
    expect(wrapper.get('.detail-kpi-grid').text()).toContain('CPU')

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

  it('uses four independently scrollable resource cards without a duplicate matrix', async () => {
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

    expect(wrapper.get('.device-app-insights').classes()).not.toContain('card')
    expect(wrapper.find('.device-app-insights > .section-title').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('应用与容器指标')
    expect(wrapper.text()).not.toContain('资源热点')
    expect(wrapper.text()).not.toContain('四项指标分别排序')
    expect(wrapper.find('.resource-ranking-board').exists()).toBe(true)
    expect(wrapper.findAll('.resource-ranking-column')).toHaveLength(4)
    expect(wrapper.findAll('.resource-ranking-list')).toHaveLength(4)
    expect(wrapper.findAll('.resource-ranking-list')[0].attributes('aria-label')).toContain('可上下滚动')
    expect(wrapper.get('.resource-summary-strip').text()).toContain('全部运行中')
    expect(wrapper.get('.resource-ranking-grid').text()).toContain('懒猫相册')
    expect(wrapper.get('.resource-ranking-grid').text()).toContain('photos-main')
    expect(wrapper.find('.application-resource-matrix').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('全部实例资源矩阵')
    expect(wrapper.find('.device-metric-chart-grid').exists()).toBe(false)
    wrapper.unmount()
  })

  it('deduplicates SMART sources and maps evidence to the correct severity', async () => {
    const collectedAt = '2026-08-26T10:00:00Z'
    const baseLabels = { device: 'sda', model: 'Example HDD', serial: 'SERIAL-1', media: 'hdd', transport: 'sata' }
    const detailed = {
      ...device,
      latest: {
        'disk.capacity': [{ name: 'disk.capacity', value: 1024 ** 4, unit: 'bytes', labels: baseLabels, collectedAt }],
        'disk.temperature': [{ name: 'disk.temperature', value: 35, unit: 'celsius', labels: baseLabels, collectedAt }],
        'disk.ata.reallocated_sectors': [
          { name: 'disk.ata.reallocated_sectors', value: 162, unit: 'count', labels: { ...baseLabels }, collectedAt },
          { name: 'disk.ata.reallocated_sectors', value: 162, unit: 'count', labels: { ...baseLabels, source: 'lazycat-docker-helper' }, collectedAt },
        ],
      },
    }
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/overview') return overview
      if (path === '/api/v1/devices/d1') return detailed
      if (path.includes('/events') || path.includes('/metrics')) return { items: [] }
      if (path === '/api/v1/operations') return { capabilities: [] }
      return detailed
    })

    const wrapper = mount(DevicesPage)
    await flushPromises()
    await wrapper.get('button.row-link').trigger('click')
    await flushPromises()
    await wrapper.get('#device-tab-storage').trigger('click')

    const card = wrapper.get('.physical-disk-card')
    expect(card.classes()).toContain('warning')
    expect(card.text()).toContain('警告')
    expect(card.text()).toContain('SMART 证据')
    expect(card.text()).toContain('重映射扇区 162')
    expect(card.text()).not.toContain('324')
    expect(card.text()).not.toContain('SMART 错误')
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
    expect(wrapper.find('.device-list-section').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('设备清单')
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

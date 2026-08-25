import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ApplicationItem } from '@/types'
import AppsPage from './AppsPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

const resources = {
  containers: 0,
  cpuPercent: 0,
  memoryUsage: 0,
  memoryLimit: 0,
  networkReceive: 0,
  networkTransmit: 0,
  blockRead: 0,
  blockWrite: 0,
}

function application(overrides: Partial<ApplicationItem>): ApplicationItem {
  return {
    id: 'app',
    title: '应用',
    instances: 1,
    healthy: 0,
    unhealthy: 0,
    paused: 0,
    versions: { '1.0.0': 1 },
    statusCounts: {},
    devices: [],
    resources,
    ...overrides,
  }
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('AppsPage', () => {
  it('excludes starting-only applications from the Healthy filter', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path.includes('/metrics')) return {
        appId: 'healthy', hours: 24, bucketSeconds: 300, updatedAt: new Date().toISOString(),
        series: { cpuPercent: [], memoryUsage: [], networkReceiveRate: [], networkTransmitRate: [], blockReadRate: [], blockWriteRate: [] },
      }
      return {
        items: [
          application({ id: 'starting', title: '启动中的应用', statusCounts: { starting: 1 } }),
          application({ id: 'healthy', title: '运行正常的应用', healthy: 1, statusCounts: { running: 1 } }),
        ],
        source: 'lazycat',
        stale: false,
        updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    expect(wrapper.findAll('.app-resource-item')).toHaveLength(2)

    await wrapper.get('[aria-label="应用状态"]').setValue('healthy')

    const rows = wrapper.findAll('.app-resource-item')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('运行正常的应用')
    expect(rows[0].text()).not.toContain('启动中的应用')
    wrapper.unmount()
  })

  it('loads CPU, memory, network and disk history for the selected application', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path.includes('/metrics')) return {
        appId: 'busy', hours: 24, bucketSeconds: 300, updatedAt: new Date().toISOString(),
        series: {
          cpuPercent: [{ value: 12, collectedAt: '2026-08-24T10:00:00Z' }],
          memoryUsage: [{ value: 104857600, collectedAt: '2026-08-24T10:00:00Z' }],
          networkReceiveRate: [{ value: 2048, collectedAt: '2026-08-24T10:00:00Z' }],
          networkTransmitRate: [{ value: 1024, collectedAt: '2026-08-24T10:00:00Z' }],
          blockReadRate: [{ value: 4096, collectedAt: '2026-08-24T10:00:00Z' }],
          blockWriteRate: [{ value: 512, collectedAt: '2026-08-24T10:00:00Z' }],
        },
      }
      return {
        items: [application({ id: 'busy', title: '繁忙应用', healthy: 1, resources: { ...resources, containers: 2, cpuPercent: 12, memoryUsage: 104857600 } })],
        source: 'lazycat', stale: false, updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()

    expect(apiMock).toHaveBeenCalledWith('/api/v1/applications/busy/metrics?hours=24')
    expect(wrapper.text()).toContain('资源历史')
    expect(wrapper.text()).toContain('网络吞吐')
    expect(wrapper.text()).toContain('磁盘 IO')
    expect(wrapper.findAll('.line-chart')).toHaveLength(4)
    wrapper.unmount()
  })

  it('applies a custom historical time range', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path.includes('/metrics')) return {
        appId: 'busy', from: '2026-08-23T08:00:00Z', to: '2026-08-24T08:00:00Z',
        bucketSeconds: 300, updatedAt: new Date().toISOString(),
        series: { cpuPercent: [], memoryUsage: [], networkReceiveRate: [], networkTransmitRate: [], blockReadRate: [], blockWriteRate: [] },
      }
      return {
        items: [application({ id: 'busy', title: '繁忙应用', healthy: 1 })],
        source: 'lazycat', stale: false, updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    await wrapper.get('.history-range button:last-child').trigger('click')
    const inputs = wrapper.findAll('.custom-history-range input')
    await inputs[0].setValue('2026-08-23T08:00')
    await inputs[1].setValue('2026-08-24T08:00')
    await wrapper.get('.custom-history-range .primary-button').trigger('click')
    await flushPromises()

    const customRequest = apiMock.mock.calls.map(([path]) => String(path)).find((path) => path.includes('from='))
    expect(customRequest).toContain('/api/v1/applications/busy/metrics?from=')
    expect(customRequest).toContain('&to=')
    expect(wrapper.text()).toContain('2026/8/23')
    wrapper.unmount()
  })

  it('sorts applications by the selected metric and opens the all-app comparison', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path.includes('/metrics/compare')) {
        const metric = new URL(`https://example.test${path}`).searchParams.get('metric') || 'cpu'
        return {
        metric, from: '2026-08-23T08:00:00Z', to: '2026-08-24T08:00:00Z', bucketSeconds: 300,
        items: [
          { appId: 'small', value: 1024, unit: 'bytes', points: [] },
          { appId: 'large', value: 4096, unit: 'bytes', points: [] },
        ],
        updatedAt: new Date().toISOString(),
      }
      }
      if (path.includes('/metrics')) return {
        appId: 'large', from: '2026-08-23T08:00:00Z', to: '2026-08-24T08:00:00Z', bucketSeconds: 300,
        summary: { networkReceiveRateBytes: 0, networkTransmitRateBytes: 0, networkTotalBytes: 0, blockReadRateBytes: 0, blockWriteRateBytes: 0, blockTotalBytes: 0 },
        series: { cpuPercent: [], memoryUsage: [], networkReceiveRate: [], networkTransmitRate: [], blockReadRate: [], blockWriteRate: [] },
      }
      return {
        items: [
          application({ id: 'small', title: '小内存应用', healthy: 1, resources: { ...resources, cpuPercent: 50, memoryUsage: 1024 } }),
          application({ id: 'large', title: '大内存应用', healthy: 1, resources: { ...resources, cpuPercent: 1, memoryUsage: 4096 } }),
        ],
        source: 'lazycat', stale: false, updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    await wrapper.get('[aria-label="排序指标"]').setValue('memory')
    expect(wrapper.findAll('.app-resource-item')[0].text()).toContain('大内存应用')

    await wrapper.findAll('.view-toggle button')[1].trigger('click')
    await flushPromises()
    for (const metric of ['cpu', 'memory', 'network', 'disk']) {
      expect(apiMock.mock.calls.some(([path]) => String(path).includes(`/metrics/compare?metric=${metric}`))).toBe(true)
    }
    expect(wrapper.text()).toContain('所有应用对比')
    expect(wrapper.findAll('.all-app-metric-panel')).toHaveLength(4)
    expect(wrapper.find('.app-comparison-table').exists()).toBe(false)
    expect(wrapper.text()).toContain('所有应用 CPU')
    expect(wrapper.text()).toContain('所有应用内存')
    expect(wrapper.text()).toContain('所有应用网络流量')
    expect(wrapper.text()).toContain('所有应用磁盘 I/O')
    expect(wrapper.findAll('.all-app-metric-panel .bar-chart-row')).toHaveLength(8)
    wrapper.unmount()
  })
})

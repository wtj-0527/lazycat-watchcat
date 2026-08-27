import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ApplicationDevice, ApplicationItem } from '@/types'
import AppsPage from './AppsPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))
vi.mock('@/dialog', () => ({ appConfirm: vi.fn(async () => true) }))

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

function device(overrides: Partial<ApplicationDevice>): ApplicationDevice {
  return {
    deviceId: 'device-1',
    deviceName: '设备一',
    deployId: 'deploy-1',
    healthy: true,
    status: 'running',
    installStatus: 'installed',
    version: '1.0.0',
    domain: '',
    builtin: false,
    userId: 'user-1',
    userName: '用户一',
    collectedAt: '2026-08-25T10:00:00Z',
    resources: { ...resources },
    ...overrides,
  }
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

  it('switches a single application between runtime instances', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path.includes('/metrics')) return {
        appId: 'multi', deviceId: path.includes('deviceId=device-2') ? 'device-2' : '',
        from: '2026-08-24T10:00:00Z', to: '2026-08-25T10:00:00Z', bucketSeconds: 300,
        updatedAt: '2026-08-25T10:00:00Z',
        summary: { networkReceiveRateBytes: 0, networkTransmitRateBytes: 0, networkTotalBytes: 0, blockReadRateBytes: 0, blockWriteRateBytes: 0, blockTotalBytes: 0 },
        series: { cpuPercent: [], memoryUsage: [], networkReceiveRate: [], networkTransmitRate: [], blockReadRate: [], blockWriteRate: [] },
      }
      return {
        items: [application({
          id: 'multi', title: '多实例应用', healthy: 2, instances: 2,
          resources: { ...resources, containers: 3, cpuPercent: 30, memoryUsage: 300 },
          devices: [
            device({ deviceId: 'device-1', deviceName: '设备一', deployId: 'deploy-1', resources: { ...resources, containers: 1, cpuPercent: 10, memoryUsage: 100 } }),
            device({ deviceId: 'device-2', deviceName: '设备二', deployId: 'deploy-2', resources: { ...resources, containers: 2, cpuPercent: 20, memoryUsage: 200 } }),
          ],
        })],
        source: 'lazycat', stale: false, updatedAt: '2026-08-25T10:00:00Z',
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    await wrapper.get('[aria-label="应用实例"]').trigger('click')
    expect(wrapper.findAll('.smart-select-options > button')).toHaveLength(3)
    const target = wrapper.findAll('.smart-select-options > button').find((item) => item.text().includes('deploy-2'))
    expect(target).toBeTruthy()
    await target!.trigger('click')
    await flushPromises()

    expect(apiMock.mock.calls.some(([path]) => String(path).includes('/api/v1/applications/multi/metrics?hours=24&deviceId=device-2'))).toBe(true)
    expect(wrapper.find('.app-resource-kpis').text()).toContain('20.0%')
    expect(wrapper.find('.app-resource-kpis').text()).toContain('2 个容器')
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
        scope: 'instance',
        items: [
          { appId: 'small', deviceId: 'device-1', value: 1024, unit: 'bytes', points: [{ value: 1024, collectedAt: '2026-08-23T08:00:00Z' }, { value: 1536, collectedAt: '2026-08-23T09:00:00Z' }] },
          { appId: 'small', deviceId: 'device-2', value: 2048, unit: 'bytes', points: [{ value: 2048, collectedAt: '2026-08-23T08:00:00Z' }, { value: 3072, collectedAt: '2026-08-23T09:00:00Z' }] },
          { appId: 'large', deviceId: 'device-1', value: 4096, unit: 'bytes', points: [{ value: 4096, collectedAt: '2026-08-23T08:00:00Z' }, { value: 5120, collectedAt: '2026-08-23T09:00:00Z' }] },
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
          application({ id: 'small', title: '小内存应用', healthy: 2, instances: 2, resources: { ...resources, cpuPercent: 50, memoryUsage: 1024 }, devices: [
            device({ deviceId: 'device-1', deviceName: '设备一', deployId: 'small-1' }),
            device({ deviceId: 'device-2', deviceName: '设备二', deployId: 'small-2' }),
          ] }),
          application({ id: 'large', title: '大内存应用', healthy: 1, resources: { ...resources, cpuPercent: 1, memoryUsage: 4096 }, devices: [
            device({ deviceId: 'device-1', deviceName: '设备一', deployId: 'large-1' }),
          ] }),
        ],
        source: 'lazycat', stale: false, updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    await wrapper.findAll('[role="columnheader"]').find((item) => item.text().startsWith('内存'))!.trigger('click')
    expect(wrapper.findAll('.app-resource-item')[0].text()).toContain('大内存应用')
    expect(wrapper.find('[aria-label="用户实例"]').exists()).toBe(false)
    expect(wrapper.get('[aria-label="应用设备"]').text()).toContain('全部设备')

    await wrapper.get('[aria-label="应用设备"]').setValue('device-2')
    expect(wrapper.findAll('.app-resource-item')).toHaveLength(1)
    expect(wrapper.find('.app-resource-item').text()).toContain('小内存应用')

    await wrapper.findAll('.view-toggle button')[1].trigger('click')
    await flushPromises()
    for (const metric of ['cpu', 'memory', 'network', 'disk']) {
      expect(apiMock.mock.calls.some(([path]) => String(path).includes(`/metrics/compare?metric=${metric}&scope=instance`))).toBe(true)
    }
    expect(wrapper.text()).not.toContain('所有应用对比')
    expect(wrapper.find('.app-comparison-card').exists()).toBe(false)
    expect(wrapper.get('[aria-label="对比应用实例"]').text()).toContain('全部实例')
    expect(wrapper.findAll('.all-app-metric-panel')).toHaveLength(4)
    expect(wrapper.find('.app-comparison-table').exists()).toBe(false)
    expect(wrapper.text()).toContain('所有应用 CPU')
    expect(wrapper.text()).toContain('所有应用内存')
    expect(wrapper.text()).toContain('所有应用网络流量')
    expect(wrapper.text()).toContain('所有应用磁盘 I/O')
    expect(wrapper.findAll('.all-app-metric-panel .line-chart')).toHaveLength(4)
    expect(wrapper.findAll('.all-app-metric-panel .chart-line')).toHaveLength(4)
    expect(wrapper.findAll('.all-app-metric-panel .chart-legend')).toHaveLength(0)
    expect(wrapper.text()).toContain('1 个应用实例')

    await wrapper.get('.all-app-metric-panel .chart-line-hit').trigger('click')
    expect(wrapper.get('[aria-label="对比应用实例"]').text()).toContain('小内存应用')
    expect(wrapper.findAll('.all-app-metric-panel .chart-line')).toHaveLength(4)
    wrapper.unmount()
  })

  it('does not attribute device-level resources to a stopped user instance', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path.includes('/metrics')) return {
        appId: 'shared', from: '2026-08-23T08:00:00Z', to: '2026-08-24T08:00:00Z',
        bucketSeconds: 300, updatedAt: new Date().toISOString(),
        summary: { networkReceiveRateBytes: 0, networkTransmitRateBytes: 0, networkTotalBytes: 0, blockReadRateBytes: 0, blockWriteRateBytes: 0, blockTotalBytes: 0 },
        series: { cpuPercent: [], memoryUsage: [], networkReceiveRate: [], networkTransmitRate: [], blockReadRate: [], blockWriteRate: [] },
      }
      return {
        items: [application({
          id: 'shared', title: '共享应用', healthy: 1, paused: 1, instances: 2,
          resources: { ...resources, containers: 2, cpuPercent: 30, memoryUsage: 300 },
          devices: [
            device({ deployId: 'running', userId: 'running-user', userName: '运行用户', resources: { ...resources, containers: 2, cpuPercent: 30, memoryUsage: 300 } }),
            device({ deployId: 'paused', userId: 'paused-user', userName: '未运行用户', status: 'paused', healthy: false, resources: { ...resources, containers: 2, cpuPercent: 30, memoryUsage: 300 } }),
          ],
        })],
        users: [{ id: 'running-user', name: '运行用户' }, { id: 'paused-user', name: '未运行用户' }],
        source: 'lazycat', stale: false, updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    await wrapper.get('[aria-label="应用实例"]').trigger('click')
    const options = wrapper.findAll('.smart-select-options > button')
    expect(options.at(-1)?.text()).toContain('paused')
    const target = options.find((item) => item.text().includes('paused'))
    await target!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.app-resource-kpis').text()).toContain('0.0%')
    expect(wrapper.find('.app-resource-kpis').text()).toContain('0 B')
    expect(wrapper.text()).toContain('该实例当前未运行')
    expect(wrapper.text()).toContain('已暂停')
    expect(wrapper.find('.app-instance-table').exists()).toBe(false)
    wrapper.unmount()
  })

  it('starts a paused instance and configures autostart through the instance controls', async () => {
    let status = 'paused'
    let autostart: boolean | null = null
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path.includes('/instances/') && options?.method === 'POST') {
        const body = JSON.parse(String(options.body))
        if (body.action === 'start') status = 'running'
        if (body.action === 'set_autostart') autostart = body.autostart
        return { status: 'succeeded', instanceStatus: status, autostart }
      }
      if (path.includes('/metrics')) return {
        appId: 'managed', from: '2026-08-26T00:00:00Z', to: '2026-08-27T00:00:00Z',
        bucketSeconds: 300, updatedAt: new Date().toISOString(),
        summary: { networkReceiveRateBytes: 0, networkTransmitRateBytes: 0, networkTotalBytes: 0, blockReadRateBytes: 0, blockWriteRateBytes: 0, blockTotalBytes: 0 },
        series: { cpuPercent: [], memoryUsage: [], networkReceiveRate: [], networkTransmitRate: [], blockReadRate: [], blockWriteRate: [] },
      }
      return {
        items: [application({
          id: 'managed', title: '可管理应用', instances: 1, healthy: status === 'running' ? 1 : 0, paused: status === 'paused' ? 1 : 0,
          devices: [device({ deployId: 'managed-1', status, healthy: status === 'running', controllable: true, autostart })],
        })],
        users: [{ id: 'user-1', name: '用户一' }],
        source: 'lazycat', stale: false, updatedAt: new Date().toISOString(),
      }
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    await wrapper.get('[aria-label="应用实例"]').trigger('click')
    await wrapper.findAll('.smart-select-options > button').find((item) => item.text().includes('managed-1'))!.trigger('click')
    await flushPromises()
    expect(wrapper.get('.instance-control-actions .primary-button').text()).toContain('启动实例')

    await wrapper.get('.instance-control-actions .primary-button').trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.some(([path, options]) =>
      String(path).includes('/applications/managed/instances/managed-1/actions') &&
      JSON.parse(String((options as RequestInit).body)).action === 'start')).toBe(true)

    await wrapper.get('.autostart-button').trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.some(([path, options]) =>
      String(path).includes('/applications/managed/instances/managed-1/actions') &&
      JSON.parse(String((options as RequestInit).body)).action === 'set_autostart')).toBe(true)
    wrapper.unmount()
  })
})

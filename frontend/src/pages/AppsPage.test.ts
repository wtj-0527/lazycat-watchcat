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
})

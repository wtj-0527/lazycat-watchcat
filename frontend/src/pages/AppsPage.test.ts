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
    apiMock.mockResolvedValue({
      items: [
        application({ id: 'starting', title: '启动中的应用', statusCounts: { starting: 1 } }),
        application({ id: 'healthy', title: '运行正常的应用', healthy: 1, statusCounts: { running: 1 } }),
      ],
      source: 'lazycat',
      stale: false,
      updatedAt: new Date().toISOString(),
    })

    const wrapper = mount(AppsPage)
    await flushPromises()
    expect(wrapper.findAll('.app-matrix tbody tr')).toHaveLength(2)

    await wrapper.get('select').setValue('healthy')

    const rows = wrapper.findAll('.app-matrix tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('运行正常的应用')
    expect(rows[0].text()).not.toContain('启动中的应用')
    wrapper.unmount()
  })
})

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import InspectionsPage from './InspectionsPage.vue'
import type { Inspection } from '@/types'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('InspectionsPage', () => {
  it('normalizes a null inspection list to an empty history', async () => {
    apiMock.mockResolvedValue({ items: null })

    const wrapper = mount(InspectionsPage)
    await flushPromises()

    expect(wrapper.text()).toContain('尚无巡检记录')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })

  it('derives report health from persisted result counts', async () => {
    const inspection: Inspection = {
      id: 'inspection-warning-result',
      triggerType: 'manual',
      startedAt: new Date().toISOString(),
      status: 'completed',
      deviceCount: 1,
      healthyCount: 0,
      warningCount: 1,
      criticalCount: 0,
      evidenceSha256: 'a'.repeat(64),
      report: { checks: { online: 1, devices: 1, healthy: 0 } },
    }
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/inspections') return { items: [inspection] }
      if (path === `/api/v1/inspections/${inspection.id}`) return inspection
      throw new Error(`unexpected API call ${path}`)
    })

    const wrapper = mount(InspectionsPage)
    await flushPromises()

    expect(wrapper.get('.inspection-title .pill').text()).toBe('警告')
    expect(wrapper.get('.inspection-title .pill').classes()).toContain('warning')
    expect(wrapper.get('.report-history tbody tr').attributes('tabindex')).toBeUndefined()
    expect(wrapper.get('.report-history .row-link').text()).toBe('#inspecti')
    wrapper.unmount()
  })

  it('shows missing connectivity evidence as Unknown', async () => {
    const inspection: Inspection = {
      id: 'inspection-missing-checks',
      triggerType: 'manual',
      startedAt: new Date().toISOString(),
      status: 'completed',
      deviceCount: 1,
      healthyCount: 1,
      warningCount: 0,
      criticalCount: 0,
      evidenceSha256: 'b'.repeat(64),
      report: { checks: {} },
    }
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/inspections') return { items: [inspection] }
      return inspection
    })

    const wrapper = mount(InspectionsPage)
    await flushPromises()

    const connectivity = wrapper.findAll('.check-row')[0]
    expect(connectivity.get('.pill').text()).toBe('未知')
    expect(connectivity.get('.pill').classes()).toContain('unknown')
    wrapper.unmount()
  })

  it('follows a newer report while no historical report is selected', async () => {
    vi.useFakeTimers()
    const older: Inspection = {
      id: 'older-report',
      triggerType: 'scheduled',
      startedAt: new Date().toISOString(),
      status: 'completed',
      deviceCount: 1,
      healthyCount: 1,
      warningCount: 0,
      criticalCount: 0,
      evidenceSha256: 'c'.repeat(64),
      report: { checks: { online: 1, devices: 1, healthy: 1 } },
    }
    const newer: Inspection = {
      ...older,
      id: 'newer-report',
      healthyCount: 0,
      warningCount: 1,
      evidenceSha256: 'd'.repeat(64),
      report: { checks: { online: 1, devices: 1, healthy: 0 } },
    }
    let listRequests = 0
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/inspections') {
        listRequests++
        return { items: [listRequests === 1 ? older : newer] }
      }
      if (path.endsWith(older.id)) return older
      if (path.endsWith(newer.id)) return newer
      throw new Error(`unexpected API call ${path}`)
    })

    const wrapper = mount(InspectionsPage)
    await flushPromises()
    expect(wrapper.get('.inspection-title').text()).toContain('#older-re')

    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()

    expect(wrapper.get('.inspection-title').text()).toContain('#newer-re')
    expect(wrapper.get('.inspection-title .pill').text()).toBe('警告')
    wrapper.unmount()
  })
})

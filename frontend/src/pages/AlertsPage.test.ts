import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AlertsPage from './AlertsPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

const alert = {
  fingerprint: 'device-1:cpu',
  deviceId: 'device-1',
  deviceName: '测试设备',
  severity: 'warning',
  resource: 'CPU',
  message: 'CPU 使用率持续偏高',
  value: 91,
  unit: '%',
  status: 'firing',
  observedAt: new Date().toISOString(),
}

afterEach(() => {
  vi.clearAllMocks()
})

describe('AlertsPage', () => {
  it('normalizes a null alert list to an empty result', async () => {
    apiMock.mockResolvedValue({ items: null })

    const wrapper = mount(AlertsPage)
    await flushPromises()

    expect(wrapper.text()).toContain('当前筛选下没有告警')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })

  it('keeps severity counts and filters limited to active alerts', async () => {
    apiMock.mockResolvedValue({
      items: [
        alert,
        {
          ...alert,
          fingerprint: 'device-1:resolved-warning',
          message: '已恢复的 Warning 告警',
          status: 'resolved',
        },
      ],
    })

    const wrapper = mount(AlertsPage)
    await flushPromises()

    const warningTab = wrapper.findAll('.alert-filter-tabs button')
      .find((button) => button.text().includes('Warning'))
    expect(warningTab?.get('b').text()).toBe('1')
    await warningTab!.trigger('click')

    expect(wrapper.get('.triage-list').text()).toContain(alert.message)
    expect(wrapper.get('.triage-list').text()).not.toContain('已恢复的 Warning 告警')
    wrapper.unmount()
  })

  it('shows success only after the acknowledged state is read back', async () => {
    let listRequest = 0
    apiMock.mockImplementation((path: string, options?: RequestInit) => {
      if (path === '/api/v1/alerts?includeResolved=true') {
        listRequest++
        return Promise.resolve({ items: [{ ...alert, status: listRequest === 1 ? 'firing' : 'acknowledged' }] })
      }
      if (path === '/api/v1/alerts/device-1%3Acpu/acknowledge' && options?.method === 'POST') {
        return Promise.resolve({ fingerprint: alert.fingerprint, status: 'acknowledged' })
      }
      return Promise.reject(new Error(`Unexpected API request: ${path}`))
    })

    const onToast = vi.fn()
    const wrapper = mount(AlertsPage, { props: { onToast } })
    await flushPromises()
    await wrapper.get('.alert-actions button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('已回读确认告警状态：已确认')
    expect(onToast).toHaveBeenLastCalledWith('告警状态已更新并回读确认')
    wrapper.unmount()
  })

  it('serializes alert mutations while one request is pending', async () => {
    let finishMutation!: () => void
    const pendingMutation = new Promise<{ fingerprint: string; status: string }>((resolve) => {
      finishMutation = () => resolve({ fingerprint: alert.fingerprint, status: 'acknowledged' })
    })
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/alerts?includeResolved=true') return Promise.resolve({ items: [alert] })
      if (path.endsWith('/acknowledge')) return pendingMutation
      if (path.endsWith('/silence')) return Promise.resolve({ fingerprint: alert.fingerprint, status: 'silenced' })
      return Promise.reject(new Error(`Unexpected API request: ${path}`))
    })

    const wrapper = mount(AlertsPage)
    await flushPromises()
    const buttons = wrapper.findAll('.alert-actions button')
    ;(buttons[0].element as HTMLButtonElement).click()
    ;(buttons[1].element as HTMLButtonElement).click()
    await flushPromises()

    const mutationCalls = apiMock.mock.calls.filter(([path]) => !String(path).includes('?includeResolved=true'))
    expect(mutationCalls).toHaveLength(1)
    expect(mutationCalls[0][0]).toContain('/acknowledge')

    finishMutation()
    await flushPromises()
    wrapper.unmount()
  })
})

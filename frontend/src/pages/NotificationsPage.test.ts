import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import NotificationsPage from './NotificationsPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

describe('NotificationsPage', () => {
  beforeEach(() => {
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/notification-settings' && options?.method === 'PUT') return JSON.parse(String(options.body))
      if (path === '/api/v1/notification-settings') return {
        settings: {
          enabled: true, criticalAlerts: true, warningAlerts: true, recoveryNotifications: true,
          inspectionResults: true, cooldownMinutes: 10, quietHoursEnabled: false, quietStart: '22:00', quietEnd: '08:00',
          recipientMode: 'admins', recipientKeys: [],
        },
        recipients: [
          { key: 'device-1::admin', deviceId: 'device-1', deviceName: 'nasw', userId: 'admin', nickname: '管理员', role: 'admin', online: true },
          { key: 'device-1::member', deviceId: 'device-1', deviceName: 'nasw', userId: 'member', nickname: '成员', role: 'normal', online: true },
        ],
        summary: { pending: 1, sent: 8, failed: 0, total: 9 },
        acceptedRisks: [{
          fingerprint: 'risk-1', deviceName: 'nasw', severity: 'warning', resource: '/dev/sda',
          message: 'Btrfs 设备错误', value: 1, unit: '', status: 'accepted',
          acceptedAt: new Date().toISOString(), lastSeenAt: new Date().toISOString(),
        }],
        channel: 'lazycat', delivery: 'outbox-retry',
      }
      if (path.includes('/unaccept')) return { fingerprint: 'risk-1', status: 'firing' }
      throw new Error(path)
    })
  })

  it('organizes all notification controls and accepted risks', async () => {
    const wrapper = mount(NotificationsPage)
    await flushPromises()

    expect(wrapper.text()).toContain('通知设置')
    expect(wrapper.text()).toContain('严重告警')
    expect(wrapper.text()).toContain('免打扰时段')
    expect(wrapper.text()).toContain('仅管理员')
    expect(wrapper.text()).toContain('1 人')
    expect(wrapper.text()).toContain('已接受风险')
    expect(wrapper.text()).toContain('Btrfs 设备错误')

    await wrapper.findAll('.notification-option-list button')[1].trigger('click')
    await wrapper.get('.notification-heading-actions .primary-button').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/v1/notification-settings', expect.objectContaining({ method: 'PUT' }))
  })
})

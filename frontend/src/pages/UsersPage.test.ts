import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import UsersPage from './UsersPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

afterEach(() => vi.clearAllMocks())

describe('UsersPage', () => {
  it('uses presence wording and saves application visibility by app id', async () => {
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/applications') {
        return { items: [{ id: 'app.one', title: '应用一', devices: [{ deviceId: 'd1' }] }, { id: 'cloud.lazycat.app.photo', title: '懒猫相册', devices: [{ deviceId: 'd1' }] }] }
      }
      if (path === '/api/v1/users' && !options) {
        return {
          count: 2,
          updatedAt: new Date().toISOString(),
          items: [
            { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'u1', nickname: '在线用户', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: true, activeDevices: 1, totalDevices: 1, applicationCount: 1, instanceCount: 1, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [], sessions: [] },
            { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'u2', nickname: '无终端用户', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: false, activeDevices: 0, totalDevices: 0, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [], sessions: [] },
          ],
        }
      }
      if (String(path).endsWith('/app-access')) return { updated: true }
      return {}
    })

    const wrapper = mount(UsersPage)
    await flushPromises()
    expect(wrapper.text()).toContain('在线')
    expect(wrapper.text()).toContain('未发现终端')
    expect(wrapper.text()).not.toContain('健康')

    await wrapper.findAll('.access-mode button')[1].trigger('click')
    await wrapper.get('.app-access-list button').trigger('click')
    await wrapper.get('.app-access-footer .primary-button').trigger('click')
    await flushPromises()

    const request = apiMock.mock.calls.find(([path]) => String(path).endsWith('/app-access'))
    expect(request?.[1]?.body).toBe(JSON.stringify({ noLimit: false, allowedAppIds: ['app.one'] }))
    wrapper.unmount()
  })
})

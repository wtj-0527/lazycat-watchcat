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

  it('keeps rendering when an offline user has null endpoint and session arrays', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/applications') return { items: [] }
      return {
        count: 2,
        updatedAt: new Date().toISOString(),
        items: [
          { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'online', nickname: '在线用户', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: true, activeDevices: 1, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 60, onlineSeconds7d: 60, onlineSeconds30d: 60, loginCount: 1, devices: [], sessions: [] },
          { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'offline', nickname: '离线用户', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: null, online: false, activeDevices: 0, totalDevices: 0, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: null, sessions: null },
        ],
      }
    })

    const wrapper = mount(UsersPage)
    await flushPromises()
    await wrapper.findAll('.user-list button')[1].trigger('click')
    await flushPromises()

    expect(wrapper.get('.user-detail').text()).toContain('离线用户')
    expect(wrapper.get('.user-detail').text()).toContain('从开始记录以来尚未观察到登录会话。')
    expect(wrapper.find('.app-access-list').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders complete endpoint data and manages local endpoints through real APIs', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/applications') return { items: [] }
      if (path === '/api/v1/users' && !options) {
        return {
          count: 1,
          updatedAt: new Date().toISOString(),
          items: [{
            deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'u1', nickname: '用户一', role: 'normal',
            appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: true,
            activeDevices: 1, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '',
            onlineSeconds24h: 60, onlineSeconds7d: 60, onlineSeconds30d: 60, loginCount: 1,
            devices: [{ id: 'endpoint-1', name: 'MacBook-Air.local', model: 'darwin', remarkName: '工作电脑', deviceApiUrl: 'https://mac.example.test:443', online: true, bindingTime: '2026-07-21T01:30:40Z', loginTime: '2026-08-27T03:21:38Z', timeZone: 'Asia/Shanghai', lang: 'zh-CN', isWifi: true }],
            sessions: [{ endDeviceId: 'endpoint-1', loginAt: '2026-08-27T03:21:38Z', durationSeconds: 120 }],
          }],
        }
      }
      return {}
    })
    const wrapper = mount(UsersPage)
    await flushPromises()

    expect(wrapper.get('.endpoint-card').text()).toContain('工作电脑')
    expect(wrapper.get('.endpoint-card').text()).toContain('MacBook-Air.local')
    expect(wrapper.get('.endpoint-card').text()).toContain('mac.example.test')
    expect(wrapper.get('.endpoint-card').text()).toContain('Asia/Shanghai')
    expect(wrapper.get('.session-timeline').text()).toContain('工作电脑')
    await wrapper.get('.endpoint-copy').trigger('click')
    expect(writeText).toHaveBeenCalledWith('mac.example.test')

    await wrapper.get('.endpoint-actions .secondary-button').trigger('click')
    const dialog = await import('@/dialog')
    dialog.dialogState.value = '新备注'
    dialog.resolveDialog(true)
    await flushPromises()
    expect(apiMock.mock.calls.some(([path, options]) => String(path).endsWith('/end-devices/endpoint-1/remark') && options?.body === JSON.stringify({ remarkName: '新备注' }))).toBe(true)

    await wrapper.get('.endpoint-actions .danger-button').trigger('click')
    dialog.resolveDialog(true)
    await flushPromises()
    expect(apiMock.mock.calls.some(([path, options]) => String(path).endsWith('/end-devices/endpoint-1') && options?.method === 'DELETE')).toBe(true)
    wrapper.unmount()
  })

  it('keeps remote endpoints read-only', async () => {
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/applications') return { items: [] }
      return {
        count: 1, updatedAt: new Date().toISOString(),
        items: [{ deviceId: 'remote', deviceName: 'canway', local: false, userId: 'u1', nickname: '远端用户', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: false, activeDevices: 0, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [{ id: 'e1', name: 'iPhone', model: 'iPhone', remarkName: '', isMobile: true, online: false }], sessions: [] }],
      }
    })
    const wrapper = mount(UsersPage)
    await flushPromises()
    expect(wrapper.find('.endpoint-actions').exists()).toBe(false)
    expect(wrapper.text()).toContain('远端登录终端为只读')
    wrapper.unmount()
  })
})

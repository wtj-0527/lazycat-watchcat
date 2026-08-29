import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import UsersPage from './UsersPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

afterEach(() => {
  vi.clearAllMocks()
  location.hash = ''
  document.body.querySelectorAll('.user-created-backdrop').forEach(element => element.remove())
})

describe('UsersPage', () => {
  it('selects local application visibility while creating a normal user', async () => {
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/applications') {
        return {
          items: [
            { id: 'cloud.lazycat.app.photo', title: '懒猫相册', devices: [{ deviceId: 'd1' }] },
            { id: 'remote.only', title: '远端应用', devices: [{ deviceId: 'd2' }] },
          ],
        }
      }
      if (path === '/api/v1/users' && !options) {
        return {
          count: 1,
          updatedAt: new Date().toISOString(),
          items: [
            { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'admin', nickname: '管理员', role: 'admin', appInstallPermission: true, appAccessNoLimit: true, allowedAppIds: [], online: true, activeDevices: 1, totalDevices: 1, applicationCount: 1, instanceCount: 1, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [], sessions: [] },
          ],
        }
      }
      if (path === '/api/v1/users' && options?.method === 'POST') return { created: true }
      return {}
    })

    const wrapper = mount(UsersPage)
    await flushPromises()
    await wrapper.get('.page-intro .primary-button').trigger('click')
    const fields = wrapper.findAll('.create-user-account input')
    await fields[0].setValue('new-user')
    await fields[1].setValue('password-123')
    await wrapper.findAll('.create-access-mode button')[1].trigger('click')

    expect(wrapper.get('.create-app-list').text()).toContain('懒猫相册')
    expect(wrapper.get('.create-app-list').text()).not.toContain('远端应用')
    await wrapper.get('.create-app-list button').trigger('click')
    await wrapper.get('.create-user-footer .primary-button').trigger('click')
    await flushPromises()

    const request = apiMock.mock.calls.find(([path, options]) => path === '/api/v1/users' && options?.method === 'POST')
    expect(request?.[1]?.body).toBe(JSON.stringify({
      userId: 'new-user',
      password: 'password-123',
      role: 'normal',
      appAccessNoLimit: false,
      allowedAppIds: ['cloud.lazycat.app.photo'],
    }))
    const receipt = document.body.querySelector<HTMLElement>('.user-created-dialog')
    expect(receipt?.textContent).toContain('用户创建成功')
    expect(receipt?.textContent).toContain('nasw')
    expect(receipt?.textContent).toContain('new-user')
    expect(receipt?.textContent).toContain('password-123')
    expect(receipt?.textContent).toContain('无受信任设备')
    expect(receipt?.textContent).toContain('lazycat.cloud/download')
    wrapper.unmount()
  })

  it('shows the local machine receipt, copies it, and does not show it when creation fails', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    let failCreation = false
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/applications') return { items: [] }
      if (path === '/api/v1/users' && !options) {
        return {
          count: 2,
          updatedAt: new Date().toISOString(),
          items: [
            { deviceId: 'remote', deviceName: 'nasw', local: false, userId: 'remote-admin', nickname: '远端管理员', role: 'admin', appInstallPermission: true, appAccessNoLimit: true, allowedAppIds: [], online: true, activeDevices: 1, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [], sessions: [] },
            { deviceId: 'local', deviceName: 'canway', local: true, userId: 'admin', nickname: '管理员', role: 'admin', appInstallPermission: true, appAccessNoLimit: true, allowedAppIds: [], online: true, activeDevices: 1, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [], sessions: [] },
          ],
        }
      }
      if (path === '/api/v1/users' && options?.method === 'POST') {
        if (failCreation) throw new Error('创建失败')
        return { created: true }
      }
      return {}
    })

    const wrapper = mount(UsersPage)
    await flushPromises()
    await wrapper.get('.page-intro .primary-button').trigger('click')
    let fields = wrapper.findAll('.create-user-account input')
    await fields[0].setValue('mandy')
    await fields[1].setValue('mandy123')
    await wrapper.get('.create-user-footer .primary-button').trigger('click')
    await flushPromises()

    const receipt = document.body.querySelector<HTMLElement>('.user-created-dialog')!
    expect(receipt.textContent).toContain('设备名称')
    expect(receipt.textContent).toContain('canway')
    expect(receipt.textContent).not.toContain('nasw')
    receipt.querySelectorAll<HTMLButtonElement>('button')[0].click()
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(`设备名称: canway

[成员1]
用户名：mandy
密码：mandy123

提示：
  1、成员首次登录输入账号信息后需点击“无受信任设备”获取管理员允许后才可登录
  2、提醒成员下载微服客户端: https://lazycat.cloud/download`)
    receipt.querySelectorAll<HTMLButtonElement>('button')[1].click()
    await flushPromises()
    await new Promise(resolve => setTimeout(resolve, 220))
    expect(document.body.querySelector('.user-created-dialog')).toBeNull()

    failCreation = true
    await wrapper.get('.page-intro .primary-button').trigger('click')
    fields = wrapper.findAll('.create-user-account input')
    await fields[0].setValue('failed-user')
    await fields[1].setValue('password-123')
    await wrapper.get('.create-user-footer .primary-button').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('.user-created-dialog')).toBeNull()
    wrapper.unmount()
  })

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

    await wrapper.findAll('.user-detail-tabs button')[1].trigger('click')
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
    await wrapper.findAll('.user-detail-tabs button')[3].trigger('click')
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
    await wrapper.findAll('.user-detail-tabs button')[2].trigger('click')

    expect(wrapper.get('.endpoint-card').text()).toContain('工作电脑')
    expect(wrapper.get('.endpoint-card').text()).toContain('MacBook-Air.local')
    expect(wrapper.get('.endpoint-card').text()).toContain('mac.example.test')
    expect(wrapper.get('.endpoint-card').text()).toContain('Asia/Shanghai')
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
    expect(apiMock.mock.calls.some(([path, options]) => String(path).endsWith('/end-devices/endpoint-1?deviceId=d1') && options?.method === 'DELETE')).toBe(true)
    await wrapper.findAll('.user-detail-tabs button')[3].trigger('click')
    expect(wrapper.get('.session-timeline').text()).toContain('工作电脑')
    wrapper.unmount()
  })

  it('allows deleting remote endpoints through the paired device command channel', async () => {
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/applications') return { items: [] }
      if (options?.method === 'DELETE') return { id: 'command-1', status: 'pending' }
      return {
        count: 1, updatedAt: new Date().toISOString(),
        items: [{ deviceId: 'remote', deviceName: 'canway', local: false, userId: 'u1', nickname: '远端用户', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: false, activeDevices: 0, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [{ id: 'e1', name: 'iPhone', model: 'iPhone', remarkName: '', isMobile: true, online: false }], sessions: [] }],
      }
    })
    const wrapper = mount(UsersPage)
    await flushPromises()
    await wrapper.findAll('.user-detail-tabs button')[2].trigger('click')
    expect(wrapper.find('.endpoint-actions').exists()).toBe(true)
    expect(wrapper.find('.endpoint-actions .secondary-button').exists()).toBe(false)
    await wrapper.get('.endpoint-actions .danger-button').trigger('click')
    const dialog = await import('@/dialog')
    dialog.resolveDialog(true)
    await flushPromises()
    expect(apiMock.mock.calls.some(([path, options]) => String(path).includes('/end-devices/e1?deviceId=remote') && options?.method === 'DELETE')).toBe(true)
    wrapper.unmount()
  })

  it('splits user details into tabs and keeps the selected user and tab in the URL', async () => {
    location.hash = '#users?device=d1&user=u2&tab=endpoints'
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/applications') return { items: [] }
      return {
        count: 2, updatedAt: new Date().toISOString(),
        items: [
          { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'u1', nickname: '用户一', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: true, activeDevices: 1, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [{ id: 'e1', name: '电脑一', model: 'windows', remarkName: '', online: true }], sessions: [] },
          { deviceId: 'd1', deviceName: 'nasw', local: true, userId: 'u2', nickname: '用户二', role: 'normal', appInstallPermission: false, appAccessNoLimit: true, allowedAppIds: [], online: false, activeDevices: 0, totalDevices: 1, applicationCount: 0, instanceCount: 0, firstObservedAt: '', updatedAt: '', onlineSeconds24h: 0, onlineSeconds7d: 0, onlineSeconds30d: 0, loginCount: 0, devices: [{ id: 'e2', name: '电脑二', model: 'windows', remarkName: '', online: false }], sessions: [] },
        ],
      }
    })
    const wrapper = mount(UsersPage)
    await flushPromises()

    expect(wrapper.get('.user-detail-head').text()).toContain('用户二')
    expect(wrapper.get('.endpoint-card').text()).toContain('电脑二')
    expect(wrapper.find('.user-metric-grid').exists()).toBe(false)
    expect(wrapper.get('.user-detail-tabs button[aria-selected="true"]').text()).toContain('登录终端')

    await wrapper.findAll('.user-detail-tabs button')[0].trigger('click')
    expect(wrapper.find('.user-metric-grid').exists()).toBe(true)
    expect(location.hash).toContain('user=u2')
    expect(location.hash).toContain('tab=overview')
    wrapper.unmount()
  })
})

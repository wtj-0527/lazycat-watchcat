import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SettingsPage from './SettingsPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

const now = new Date().toISOString()

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callback(0)
    return 1
  })
  apiMock.mockImplementation(async (path: string) => {
    if (path === '/api/v1/settings') return {
      appVersion: '1.8.0', deploymentMode: 'single-lpk', embeddedCollector: true, singleUser: true, maxDevices: 100,
      collectIntervalSeconds: 30, advancedIntervalSeconds: 300, rawRetentionDays: 7, rollupRetentionDays: 90,
      auditRetentionDays: 180, inspectionRetentionDays: 180, backupRetentionCount: 20,
      notificationChannel: 'lazycat', notificationDelivery: 'outbox-retry',
    }
    if (path === '/api/v1/operations') return { capabilities: [], schedule: { daily: { hour: 2 }, weekly: { hour: 3 }, timezone: 'Asia/Shanghai' } }
    if (path === '/api/v1/database/status') return { databaseSize: 1024, integrityOk: true, backupCount: 0 }
    if (path === '/api/v1/backups') return { items: null }
    if (path === '/api/v1/stability') return {
      startedAt: now, targetEndAt: now, lastSampleAt: now, sampleCount: 1, failureCount: 0, consecutiveFailures: 0,
      databaseIntegrityOk: true, databaseLatencyMs: 1, metricFreshnessSeconds: 2, pendingNotifications: 0, qualified: false, remainingSeconds: 3600,
    }
    throw new Error(`unexpected API call ${path}`)
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('SettingsPage tabs', () => {
  it('uses roving tabindex and supports arrow, Home, and End navigation', async () => {
    const wrapper = mount(SettingsPage, { attachTo: document.body })
    await flushPromises()
    const tabs = wrapper.findAll('[role="tab"]')

    expect(tabs).toHaveLength(8)
    for (const item of tabs) {
      expect(document.getElementById(item.attributes('aria-controls')!)).not.toBeNull()
    }
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    expect(tabs[0].attributes('tabindex')).toBe('0')
    expect(tabs[1].attributes('tabindex')).toBe('-1')

    await tabs[0].trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.get('#settings-tab-groups').attributes('aria-selected')).toBe('true')
    expect(document.activeElement?.id).toBe('settings-tab-groups')

    await wrapper.get('#settings-tab-groups').trigger('keydown', { key: 'End' })
    expect(wrapper.get('#settings-tab-audit').attributes('aria-selected')).toBe('true')

    await wrapper.get('#settings-tab-audit').trigger('keydown', { key: 'Home' })
    expect(wrapper.get('#settings-tab-onboarding').attributes('aria-selected')).toBe('true')
    const panel = wrapper.get('[role="tabpanel"]')
    expect(panel.attributes('aria-labelledby')).toBe('settings-tab-onboarding')
    wrapper.unmount()
  })


  it('normalizes a null capability list to an empty result', async () => {
    const base = apiMock.getMockImplementation()!
    apiMock.mockImplementation(async (path: string) => {
      if (path === '/api/v1/operations') return {
        capabilities: null,
        schedule: { daily: { hour: 2 }, weekly: { hour: 3 }, timezone: 'Asia/Shanghai' },
      }
      return base(path)
    })

    const wrapper = mount(SettingsPage)
    await flushPromises()
    await wrapper.get('#settings-tab-capabilities').trigger('click')

    expect(wrapper.text()).toContain('Collector 能力')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })

  it('creates an environment-neutral device invitation from the current origin', async () => {
    const base = apiMock.getMockImplementation()!
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/pairing-codes' && options?.method === 'POST') {
        return { code: 'PAIR-1234', expiresAt: now }
      }
      return base(path, options)
    })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })

    const wrapper = mount(SettingsPage, { props: { initialTab: 'onboarding' } })
    await flushPromises()

    expect(wrapper.text()).not.toContain('canway')
    expect(wrapper.text()).not.toContain('nasw')
    expect(wrapper.text()).not.toContain('192.168.')
    expect(wrapper.text()).not.toContain('当前部署')
    expect(wrapper.text()).not.toContain('高级连接设置')
    await wrapper.get('.connect-hero .primary-button').trigger('click')
    await flushPromises()
    await wrapper.get('.invite-ready-card .primary-button').trigger('click')

    expect(writeText).toHaveBeenCalledWith(expect.stringMatching(/^http:\/\/localhost:\d+\/#pairing-code=PAIR-1234$/))
    await wrapper.findAll('.text-button').at(1)!.trigger('click')
    expect(wrapper.text()).toContain('目标设备可访问的猫眼地址')
    wrapper.unmount()
  })

  it('shows persisted evidence after a backup is created and read back', async () => {
    const backup = {
      name: 'manual-20260823.db', type: 'manual', appVersion: '1.8.0', createdAt: now,
      size: 2048, sha256: 'a'.repeat(64), verified: true,
    }
    const base = apiMock.getMockImplementation()!
    let created = false
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/backups' && options?.method === 'POST') {
        created = true
        return backup
      }
      if (path === '/api/v1/backups' && created) return { items: [backup] }
      return base(path)
    })

    const wrapper = mount(SettingsPage, { attachTo: document.body })
    await flushPromises()
    await wrapper.get('#settings-tab-retention').trigger('click')
    await wrapper.get('.operations-layout .primary-button').trigger('click')
    await flushPromises()

    const evidence = wrapper.get('.operation-evidence.success')
    expect(evidence.text()).toContain(backup.name)
    expect(evidence.text()).toContain(backup.sha256.slice(0, 16))
    expect(wrapper.text()).toContain('SHA-256 aaaaaaaaaaaaaaaa…')
    wrapper.unmount()
  })

  it('previews and confirms cleanup before pruning unused images', async () => {
    const base = apiMock.getMockImplementation()!
    let pruned = false
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/docker/images/unused') {
        return pruned
          ? {
              available: true, count: 1, totalSize: 2048,
              danglingCount: 0, danglingSize: 0, cachedCount: 1, cachedSize: 2048,
              items: [{ id: `sha256:${'b'.repeat(64)}`, tags: ['cached:future'], size: 2048, createdAt: now, category: 'cached' }],
            }
          : {
              available: true, count: 2, totalSize: 3072,
              danglingCount: 1, danglingSize: 1024, cachedCount: 1, cachedSize: 2048,
              items: [
                { id: `sha256:${'a'.repeat(64)}`, tags: ['<none>:<none>'], size: 1024, createdAt: now, category: 'dangling' },
                { id: `sha256:${'b'.repeat(64)}`, tags: ['cached:future'], size: 2048, createdAt: now, category: 'cached' },
              ],
            }
      }
      if (path === '/api/v1/docker/images/prune' && options?.method === 'POST') {
        pruned = true
        return { imagesDeleted: 2, referencesUntagged: 1, spaceReclaimed: 2048 }
      }
      return base(path)
    })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mount(SettingsPage)
    await flushPromises()
    await wrapper.get('#settings-tab-retention').trigger('click')

    expect(wrapper.text()).toContain('清理 1 个悬空镜像')
    expect(wrapper.text()).toContain('cached:future')
    expect(wrapper.text()).toContain('删除并允许重拉')
    await wrapper.get('.image-cleanup-card .danger-button').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledOnce()
    expect(apiMock).toHaveBeenCalledWith('/api/v1/docker/images/prune', { method: 'POST' })
    expect(wrapper.text()).toContain('删除 2 个镜像')
    expect(wrapper.text()).toContain('没有悬空镜像')
    wrapper.unmount()
  })

  it('saves backup retention and deletes a selected backup after confirmation', async () => {
    const backup = {
      name: 'manual-delete.db', type: 'manual', appVersion: '1.12.4', createdAt: now,
      size: 2048, sha256: 'b'.repeat(64), verified: true,
    }
    const base = apiMock.getMockImplementation()!
    let deleted = false
    apiMock.mockImplementation(async (path: string, options?: RequestInit) => {
      if (path === '/api/v1/backups') return { items: deleted ? [] : [backup] }
      if (path === `/api/v1/backups/${backup.name}` && options?.method === 'DELETE') {
        deleted = true
        return undefined
      }
      return base(path, options)
    })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mount(SettingsPage)
    await flushPromises()
    await wrapper.get('#settings-tab-retention').trigger('click')

    expect(wrapper.text()).toContain('数据库备份（份）')
    const deleteButton = wrapper.findAll('.operations-layout .danger-button').find((button) => button.text() === '删除')
    expect(deleteButton).toBeTruthy()
    await deleteButton!.trigger('click')
    await flushPromises()

    expect(apiMock).toHaveBeenCalledWith(`/api/v1/backups/${backup.name}`, { method: 'DELETE' })
    expect(wrapper.text()).toContain(`备份 ${backup.name} 已删除`)
    wrapper.unmount()
  })
})

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import StoragePage from './StoragePage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

afterEach(() => {
  vi.clearAllMocks()
})

describe('StoragePage', () => {
  it('normalizes null storage and capability lists to an empty state', async () => {
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({ items: null, updatedAt: new Date().toISOString() })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: null })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    expect(wrapper.text()).toContain('尚无存储数据')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })

  it('uses backend disk thresholds and includes NVMe and ATA health risks', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'd1', deviceName: '猫盒', name: 'disk.temperature', value: 82, unit: 'celsius', labels: { device: '/dev/sda' }, collectedAt },
          { deviceId: 'd1', deviceName: '猫盒', name: 'disk.nvme.critical_warning', value: 1, unit: 'bitmask', labels: { device: '/dev/nvme0n1' }, collectedAt },
          { deviceId: 'd1', deviceName: '猫盒', name: 'disk.ata.reallocated_sectors', value: 2, unit: 'count', labels: { device: '/dev/sda' }, collectedAt },
          { deviceId: 'd1', deviceName: '猫盒', name: 'filesystem.root.usage', value: 20, unit: '%', labels: { mount: '/' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    const rows = wrapper.findAll('.storage-risk-card tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0].find('.pill').classes()).toContain('critical')
    expect(rows[1].find('.pill').classes()).toContain('critical')
    expect(rows[2].find('.pill').classes()).toContain('warning')
    expect(wrapper.get('.storage-risk-card').text()).toContain('disk.nvme.critical_warning')
    expect(wrapper.get('.storage-risk-card').text()).toContain('disk.ata.reallocated_sectors')
    wrapper.unmount()
  })

  it('keeps storage evidence available when capability lookup fails', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [{
          deviceId: 'd1',
          deviceName: '猫盒',
          name: 'filesystem.root.usage',
          value: 50,
          unit: '%',
          labels: { mount: '/' },
          collectedAt,
        }],
      })
      if (path === '/api/v1/operations') return Promise.reject(new Error('capability service unavailable'))
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    expect(wrapper.text()).toContain('猫盒')
    expect(wrapper.text()).toContain('50.0%')
    expect(wrapper.get('.capability-card').text()).toContain('未知')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })

  it('expands physical disks, volumes, and their history panels together', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'd1', deviceName: '猫盒', name: 'disk.capacity', value: 4_000_000_000_000, unit: 'bytes', labels: { device: 'sdb', model: 'WDC WD40EZAX', serial: 'WD-TEST', media: 'hdd', transport: 'usb' }, collectedAt },
          { deviceId: 'd1', deviceName: '猫盒', name: 'btrfs.usage', value: 51.1, unit: '%', labels: { mount: '/lzcsys/run/mnt/sdb1', backing_device: '/dev/sdb1' }, collectedAt },
          { deviceId: 'd1', deviceName: '猫盒', name: 'btrfs.size', value: 4_000_000_000_000, unit: 'bytes', labels: { mount: '/lzcsys/run/mnt/sdb1', backing_device: '/dev/sdb1' }, collectedAt },
          { deviceId: 'd1', deviceName: '猫盒', name: 'btrfs.free_estimated', value: 1_900_000_000_000, unit: 'bytes', labels: { mount: '/lzcsys/run/mnt/sdb1', backing_device: '/dev/sdb1' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    expect(wrapper.get('.storage-resource-card').text()).toContain('备份盘')
    expect(wrapper.get('.storage-resource-card').text()).toContain('sdb · WDC WD40EZAX')
    expect(wrapper.get('.storage-resource-card').text()).toContain('备份卷')
    expect(wrapper.text()).not.toContain('最高使用率卷 · 14 天趋势')
    expect(wrapper.findAll('.storage-history-panel')).toHaveLength(2)
    expect(wrapper.get('.storage-resource-card').text()).toContain('磁盘 I/O 趋势')
    expect(wrapper.get('.storage-resource-card').text()).toContain('备份卷 · 使用趋势')
    wrapper.unmount()
  })
})

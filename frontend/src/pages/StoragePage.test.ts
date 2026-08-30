import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import StoragePage from './StoragePage.vue'
import { selectGlobalDevice } from '@/deviceScope'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

afterEach(() => {
  vi.clearAllMocks()
  location.hash = ''
  selectGlobalDevice('all')
})

describe('StoragePage', () => {
  it('loads storage history with the 24-hour range by default', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'd1', deviceName: '猫盒', name: 'disk.capacity', value: 2_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'TOSHIBA MQ04ABD200' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/io-sources')) return Promise.resolve({ deviceId: 'd1', collectedAt, processTotal: 0, processes: [], applications: [], limitations: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    expect(apiMock.mock.calls.some(([path]) => String(path).includes('hours=24'))).toBe(true)
    expect(wrapper.get('.range-tabs button.active').text()).toBe('24小时')
    wrapper.unmount()
  })

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

    const rows = wrapper.findAll('.embedded-risk-row')
    expect(rows).toHaveLength(3)
    expect(rows.filter((row) => row.find('.pill').classes().includes('critical'))).toHaveLength(2)
    expect(rows.filter((row) => row.find('.pill').classes().includes('warning'))).toHaveLength(1)
    expect(wrapper.get('.storage-resource-card').text()).toContain('disk.nvme.critical_warning')
    expect(wrapper.get('.storage-resource-card').text()).toContain('disk.ata.reallocated_sectors')
    expect(wrapper.find('.storage-risk-card').exists()).toBe(false)
    expect(wrapper.find('.btrfs-health-card').exists()).toBe(false)
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
    expect(wrapper.get('.storage-resource-card').text()).toContain('猫盒 · 备份盘')
    expect(wrapper.get('.storage-resource-card').text()).toContain('sdb · Western Digital')
    expect(wrapper.get('.storage-resource-card').text()).toContain('WDC WD40EZAX · WD-TEST')
    expect(wrapper.get('.storage-resource-card').text()).toContain('备份卷')
    expect(wrapper.text()).not.toContain('最高使用率卷 · 14 天趋势')
    expect(wrapper.findAll('.storage-history-panel')).toHaveLength(3)
    expect(wrapper.get('.storage-resource-card').text()).toContain('磁盘 I/O 趋势')
    expect(wrapper.get('.storage-resource-card').text()).toContain('磁盘繁忙度')
    expect(wrapper.get('.storage-resource-card').text()).toContain('备份卷 · sdb1')
    expect(apiMock.mock.calls.some(([path]) => String(path).includes('points=240'))).toBe(true)
    wrapper.unmount()
  })

  it('associates a mounted non-Btrfs partition with its physical disk and shows usage', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'd1', deviceName: 'nasw', name: 'disk.capacity', value: 1_000_204_886_016, unit: 'bytes', labels: { device: 'sda', model: 'ST1000LM048-2E7172', serial: 'WL1EME5D', media: 'hdd', transport: 'sata' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'filesystem.volume.usage', value: 84, unit: '%', labels: { mount: '/lzcsys/run/media/sda1', backing_device: '/dev/sda1', filesystem: 'ntfs' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'filesystem.volume.size', value: 644_247_191_552, unit: 'bytes', labels: { mount: '/lzcsys/run/media/sda1', backing_device: '/dev/sda1', filesystem: 'ntfs' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'filesystem.volume.available', value: 103_368_351_744, unit: 'bytes', labels: { mount: '/lzcsys/run/media/sda1', backing_device: '/dev/sda1', filesystem: 'ntfs' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    const disk = wrapper.get('.storage-expanded-disk')
    expect(disk.text()).toContain('nasw · 外接数据盘')
    expect(disk.text()).toContain('sda1 · NTFS')
    expect(disk.text()).toContain('/lzcsys/run/media/sda1')
    expect(disk.text()).toContain('84.0%')
    expect(disk.text()).toContain('503.73 GiB')
    expect(disk.text()).not.toContain('当前没有已挂载卷')
    expect(disk.find('.storage-capacity-track').exists()).toBe(true)
    wrapper.unmount()
  })

  it('uses the worst physical disk, Btrfs, and capacity state for a volume', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'd1', deviceName: 'nasw', name: 'disk.capacity', value: 1_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'ST1000LM048', serial: 'SERIAL', media: 'hdd' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'disk.ata.pending_sectors', value: 2, unit: 'count', labels: { device: '/dev/sda' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'btrfs.usage', value: 40, unit: '%', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'btrfs.size', value: 1_000_000_000_000, unit: 'bytes', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'btrfs.free_estimated', value: 600_000_000_000, unit: 'bytes', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'd1', deviceName: 'nasw', name: 'btrfs.scrub.known', value: 1, unit: 'bool', labels: { mount: '/lzcsys/data' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    const volumePanel = wrapper.get('.volume-panel')
    expect(volumePanel.get('.pill').classes()).toContain('critical')
    expect(volumePanel.text()).toContain('物理磁盘存在严重告警')
    expect(volumePanel.text()).not.toContain('尚无 Scrub 历史')
    wrapper.unmount()
  })

  it('highlights the physical disk selected by an alert deep link', async () => {
    location.hash = '#storage?deviceId=d1&disk=sda'
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [{
          deviceId: 'd1', deviceName: 'nasw', name: 'disk.capacity', value: 1_000_000_000_000,
          unit: 'bytes', labels: { device: 'sda', model: 'ST1000LM048', serial: 'SERIAL', media: 'hdd' }, collectedAt,
        }],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()
    expect(wrapper.get('.storage-location-evidence').text()).toContain('已定位到 nasw · sda')
    expect(wrapper.get('.storage-expanded-disk').classes()).toContain('targeted')
    wrapper.unmount()
  })

  it('keeps identical Linux device names separated by WatchCat device', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'nasw', deviceName: 'nasw', name: 'disk.capacity', value: 1_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'ST1000LM048-2E7172', serial: 'SEAGATE-1', media: 'hdd', transport: 'sata' }, collectedAt },
          { deviceId: 'canway', deviceName: 'canway', name: 'disk.capacity', value: 2_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'TOSHIBA MQ04ABD200', serial: 'TOSHIBA-1', media: 'hdd', transport: 'sata' }, collectedAt },
          { deviceId: 'nasw', deviceName: 'nasw', name: 'btrfs.usage', value: 10, unit: '%', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'nasw', deviceName: 'nasw', name: 'btrfs.size', value: 1_000_000_000_000, unit: 'bytes', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'canway', deviceName: 'canway', name: 'btrfs.usage', value: 20, unit: '%', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'canway', deviceName: 'canway', name: 'btrfs.size', value: 2_000_000_000_000, unit: 'bytes', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()
    const cards = wrapper.findAll('.storage-expanded-disk')

    expect(cards).toHaveLength(2)
    expect(cards[0].text() + cards[1].text()).toContain('nasw · 主数据盘')
    expect(cards[0].text() + cards[1].text()).toContain('canway · 主数据盘')
    expect(cards[0].text() + cards[1].text()).toContain('sda · Seagate')
    expect(cards[0].text() + cards[1].text()).toContain('sda · Toshiba')
    expect(wrapper.findAll('.volume-panel')).toHaveLength(2)
    wrapper.unmount()
  })

  it('filters storage statistics, disks, volumes, and history by device', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'nasw', deviceName: 'nasw', name: 'disk.capacity', value: 1_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'ST1000LM048', media: 'hdd' }, collectedAt },
          { deviceId: 'canway', deviceName: 'canway', name: 'disk.capacity', value: 2_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'TOSHIBA MQ04ABD200', media: 'hdd' }, collectedAt },
          { deviceId: 'nasw', deviceName: 'nasw', name: 'btrfs.usage', value: 10, unit: '%', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
          { deviceId: 'canway', deviceName: 'canway', name: 'btrfs.usage', value: 20, unit: '%', labels: { mount: '/lzcsys/data', backing_device: '/dev/sda1' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()
    expect(wrapper.findAll('.storage-expanded-disk')).toHaveLength(2)

    selectGlobalDevice('canway')
    await flushPromises()

    expect(wrapper.findAll('.storage-expanded-disk')).toHaveLength(1)
    expect(wrapper.get('.storage-expanded-disk').text()).toContain('canway · 主数据盘')
    expect(wrapper.get('.storage-expanded-disk').text()).not.toContain('nasw')
    expect(wrapper.findAll('.stats .stat')[0].text()).toContain('1')
    expect(apiMock.mock.calls.some(([path]) => String(path).includes('/devices/canway/metrics?'))).toBe(true)
    wrapper.unmount()
  })

  it('shows application I/O attribution, loads its history, and links to the canonical process view', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockImplementation((path: string) => {
      if (path === '/api/v1/storage') return Promise.resolve({
        updatedAt: collectedAt,
        items: [
          { deviceId: 'canway', deviceName: 'canway', name: 'disk.capacity', value: 2_000_000_000_000, unit: 'bytes', labels: { device: 'sda', model: 'TOSHIBA MQ04ABD200', media: 'hdd' }, collectedAt },
        ],
      })
      if (path === '/api/v1/operations') return Promise.resolve({ capabilities: [] })
      if (path === '/api/v1/devices/canway/io-sources?limit=10&page=1') return Promise.resolve({
        deviceId: 'canway', collectedAt, processTotal: 128, activeProcessTotal: 1,
        processPage: 1, processPageSize: 10, applicationTotal: 2, limitations: [],
        applications: [
          {
            appId: 'community.lazycat.app.hermes-studio', appTitle: 'Hermes Studio',
            deployId: 'community.lazycat.app.hermes-studio2', userName: 'Mandy',
            instanceStatus: 'running', containers: ['hermes-web'],
            processCount: 3, activeProcessCount: 2, readRate: 2048, writeRate: 4096,
            processes: [],
          },
          {
            appId: 'cloud.lazycat.app.photo', appTitle: '懒猫相册',
            deployId: 'cloud.lazycat.app.photo2', userName: 'Alice',
            instanceStatus: 'paused', containers: [],
            processCount: 0, activeProcessCount: 0, readRate: 0, writeRate: 0,
            processes: [],
          },
        ],
      })
      if (path.includes('/api/v1/applications/community.lazycat.app.hermes-studio/metrics?')) return Promise.resolve({
        series: {
          blockReadRate: [{ value: 2048, collectedAt }],
          blockWriteRate: [{ value: 4096, collectedAt }],
        },
      })
      if (path.includes('/metrics?')) return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`Unexpected API path: ${path}`))
    })

    const wrapper = mount(StoragePage)
    await flushPromises()

    expect(wrapper.get('.storage-io-attribution').text()).toContain('Hermes Studio')
    expect(wrapper.get('.storage-io-attribution').text()).toContain('懒猫相册')
    expect(wrapper.get('.storage-io-attribution').text()).toContain('2 个实例')
    expect(wrapper.get('.storage-io-attribution').text()).toContain('当前空闲')
    expect(wrapper.get('.storage-io-attribution').text()).not.toContain('宿主机进程')
    expect(wrapper.get('.storage-io-attribution').text()).not.toContain('rclone')
    expect(wrapper.find('.storage-io-child-processes').exists()).toBe(false)

    const applicationRow = wrapper.findAll('.storage-io-source-row').find((row) => row.text().includes('Hermes Studio'))
    if (!applicationRow) throw new Error('Hermes Studio application row missing')
    await applicationRow.trigger('click')
    await flushPromises()
    expect(wrapper.get('.storage-io-history').text()).toContain('Hermes Studio · I/O 历史')
    expect(wrapper.get('.storage-io-history').text()).toContain('应用容器块设备读写速率')
    expect(apiMock.mock.calls.some(([path]) => String(path).includes('/applications/community.lazycat.app.hermes-studio/metrics?'))).toBe(true)

    const processLink = wrapper.findAll('.storage-process-link').find((button) => button.element.parentElement?.textContent?.includes('Hermes Studio'))
    if (!processLink) throw new Error('Hermes Studio process link missing')
    await processLink.trigger('click')
    expect(location.hash).toContain('devices?')
    expect(location.hash).toContain('appId=community.lazycat.app.hermes-studio')
    expect(location.hash).toContain('deployId=community.lazycat.app.hermes-studio2')
    wrapper.unmount()
  })
})

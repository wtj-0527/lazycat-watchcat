import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import OverviewPage from './OverviewPage.vue'

const apiMock = vi.hoisted(() => vi.fn())
vi.mock('@/api', () => ({ api: apiMock }))

afterEach(() => {
  vi.clearAllMocks()
})

describe('OverviewPage', () => {
  it('normalizes null device and alert lists to empty states', async () => {
    apiMock.mockResolvedValue({
      stats: { devices: 0, online: 0, offline: 0, critical: 0, warning: 0 },
      devices: null,
      alerts: null,
      updatedAt: new Date().toISOString(),
    })

    const wrapper = mount(OverviewPage)
    await flushPromises()

    expect(wrapper.text()).toContain('设备接入并完成首次采集后')
    expect(wrapper.text()).not.toContain('数据加载失败')
    wrapper.unmount()
  })

  it('does not let a safe root filesystem mask critical Btrfs usage', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockResolvedValue({
      stats: { devices: 1, online: 1, offline: 0, critical: 1, warning: 0 },
      devices: [{
        id: 'd1', name: '猫盒', hostname: 'box', osVersion: 'LazyCat OS', collectorVersion: '1.8.0',
        status: 'active', lastSeenAt: collectedAt, online: true, stale: false, health: 'critical',
        latest: {
          'filesystem.root.usage': [{ name: 'filesystem.root.usage', value: 20, unit: '%', labels: { mount: '/' }, collectedAt }],
          'system.cpu.usage': [{ name: 'system.cpu.usage', value: 18, unit: '%', labels: {}, collectedAt }],
          'system.memory.usage': [{ name: 'system.memory.usage', value: 42, unit: '%', labels: {}, collectedAt }],
          'system.load.1m': [{ name: 'system.load.1m', value: 1.2, unit: '', labels: {}, collectedAt }],
          'system.temperature': [{ name: 'system.temperature', value: 51, unit: 'celsius', labels: { sensor: 'package' }, collectedAt }],
          'system.uptime': [{ name: 'system.uptime', value: 7200, unit: 'seconds', labels: {}, collectedAt }],
          'btrfs.usage': [
            { name: 'btrfs.usage', value: 96, unit: '%', labels: { mount: '/data' }, collectedAt },
            { name: 'btrfs.usage', value: 42, unit: '%', labels: { mount: '/backup' }, collectedAt },
          ],
          'network.interface.receive.bytes_total': [{ name: 'network.interface.receive.bytes_total', value: 2048, unit: 'bytes', labels: { interface: 'eth0' }, collectedAt }],
          'network.interface.transmit.bytes_total': [{ name: 'network.interface.transmit.bytes_total', value: 4096, unit: 'bytes', labels: { interface: 'eth0' }, collectedAt }],
          'disk.io.read.bytes_total': [{ name: 'disk.io.read.bytes_total', value: 10 * 1024 ** 4, unit: 'bytes', labels: { device: 'sda' }, collectedAt }],
          'disk.io.write.bytes_total': [{ name: 'disk.io.write.bytes_total', value: 8 * 1024 ** 4, unit: 'bytes', labels: { device: 'sda' }, collectedAt }],
        },
      }],
      alerts: [{
        fingerprint: 'btrfs-data',
        deviceId: 'd1',
        deviceName: '猫盒',
        severity: 'critical',
        status: 'firing',
        resource: '/data',
        message: 'Btrfs 使用率 96.0%',
        value: 96,
        unit: '%',
      }],
      updatedAt: collectedAt,
    })

    const wrapper = mount(OverviewPage)
    await flushPromises()

    expect(wrapper.get('.capacity-evidence-list').text()).toContain('96.0%')
    expect(wrapper.get('.capacity-evidence-list').text()).toContain('42.0%')
    expect(wrapper.findAll('.capacity-evidence-row')).toHaveLength(2)
    expect(wrapper.get('.capacity-evidence-list').find('.pill').classes()).toContain('critical')
    expect(wrapper.get('.capacity-evidence-head').text()).toContain('预计写满')
    expect(wrapper.get('.capacity-evidence-list').text()).toContain('采集能力 4/5')
    expect(wrapper.findAll('.overview-summary-grid > .card')).toHaveLength(2)
    expect(wrapper.findAll('.fleet-device-metrics')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('设备实时指标')
    expect(wrapper.get('.fleet-realtime-section').text()).toContain('CPU 使用率')
    expect(wrapper.get('.fleet-realtime-section').text()).toContain('18.0%')
    expect(wrapper.get('.fleet-realtime-section').text()).toContain('CPU 封装温度')
    expect(wrapper.get('.fleet-realtime-section').text()).toContain('51.0 ℃')
    expect(wrapper.get('.device-health-evidence').text()).toContain('严重原因')
    expect(wrapper.get('.device-health-evidence').text()).toContain('Btrfs 使用率 96.0%')
    expect(wrapper.get('.device-health-evidence').text()).toContain('/data')
    const metricCards = wrapper.findAll('.realtime-metric')
    expect(metricCards[6].text()).toContain('收2.0 KiB')
    expect(metricCards[6].text()).toContain('发4.0 KiB')
    expect(metricCards[7].text()).toContain('读10.00 TiB')
    expect(metricCards[7].text()).toContain('写8.00 TiB')
    expect(metricCards[7].attributes('title')).toBeUndefined()
    await metricCards[7].trigger('mouseenter')
    expect(metricCards[7].get('[role="tooltip"]').text()).toContain('disk.io.read.bytes_total')
    expect(wrapper.get('.capability-summary-hover').attributes('title')).toBeUndefined()
    wrapper.unmount()
  })

  it('shows the exact hidden SMART metric that makes a device critical', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockResolvedValue({
      stats: { devices: 1, online: 1, offline: 0, critical: 1, warning: 0 },
      devices: [{
        id: 'd1', name: 'nasw', hostname: 'nasw', collectorVersion: '1.4.20',
        status: 'online', lastSeenAt: collectedAt, online: true, stale: false, health: 'critical',
        latest: {
          'system.cpu.usage': [{ name: 'system.cpu.usage', value: 5.4, unit: '%', labels: {}, collectedAt }],
          'disk.ata.pending_sectors': [{ name: 'disk.ata.pending_sectors', value: 8, unit: 'count', labels: { device: 'sda' }, collectedAt, risk: 'critical' }],
        },
      }],
      alerts: [],
      updatedAt: collectedAt,
    })

    const wrapper = mount(OverviewPage)
    await flushPromises()
    const evidence = wrapper.get('.device-health-evidence')
    expect(evidence.text()).toContain('严重原因')
    expect(evidence.text()).toContain('待处理扇区')
    expect(evidence.text()).toContain('sda')
    expect(evidence.get('a').attributes('href')).toBe('#alerts')
    wrapper.unmount()
  })

  it('uses canonical temperature sources instead of vendor-specific NVMe sub-sensors', async () => {
    const collectedAt = new Date().toISOString()
    apiMock.mockResolvedValue({
      stats: { devices: 1, online: 1, offline: 0, critical: 0, warning: 0 },
      devices: [{
        id: 'd1', name: 'canway', hostname: 'canway', collectorVersion: '1.4.1',
        status: 'online', lastSeenAt: collectedAt, online: true, stale: false, health: 'healthy',
        latest: {
          'system.temperature': [
            { name: 'system.temperature', value: 60.85, unit: 'celsius', labels: { sensor: 'nvme_composite' }, collectedAt },
            { name: 'system.temperature', value: 84.85, unit: 'celsius', labels: { sensor: 'nvme_sensor_1' }, collectedAt },
            { name: 'system.temperature', value: 73, unit: 'celsius', labels: { sensor: 'coretemp_package_id_0' }, collectedAt },
          ],
        },
      }],
      alerts: [],
      updatedAt: collectedAt,
    })

    const wrapper = mount(OverviewPage)
    await flushPromises()
    const temperature = wrapper.findAll('.realtime-metric')[3]
    expect(temperature.text()).toContain('CPU 封装温度')
    expect(temperature.text()).toContain('73.0 ℃')
    expect(temperature.text()).not.toContain('84.9 ℃')
    expect(temperature.classes()).not.toContain('critical')
    wrapper.unmount()
  })
})

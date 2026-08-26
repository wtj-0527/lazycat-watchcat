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

    expect(wrapper.text()).toContain('尚未接入设备')
    expect(wrapper.text()).toContain('当前没有活动风险')
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
        },
      }],
      alerts: [],
      updatedAt: collectedAt,
    })

    const wrapper = mount(OverviewPage)
    await flushPromises()

    expect(wrapper.get('.capacity-evidence-list').text()).toContain('96.0%')
    expect(wrapper.get('.capacity-evidence-list').text()).toContain('42.0%')
    expect(wrapper.findAll('.capacity-evidence-row')).toHaveLength(2)
    expect(wrapper.get('.capacity-evidence-list').find('.pill').classes()).toContain('critical')
    expect(wrapper.get('.device-health-head').text()).toContain('最新数据')
    expect(wrapper.get('.capacity-evidence-head').text()).toContain('预计写满')
    expect(wrapper.get('.capacity-evidence-list').text()).toContain('采集能力 3/5')
    expect(wrapper.findAll('.overview-summary-grid > .card')).toHaveLength(4)
    expect(wrapper.findAll('.fleet-device-metrics')).toHaveLength(1)
    expect(wrapper.get('.fleet-realtime-card').text()).toContain('CPU 使用率')
    expect(wrapper.get('.fleet-realtime-card').text()).toContain('18.0%')
    expect(wrapper.get('.fleet-realtime-card').text()).toContain('最高温度')
    expect(wrapper.get('.fleet-realtime-card').text()).toContain('51.0 ℃')
    wrapper.unmount()
  })
})

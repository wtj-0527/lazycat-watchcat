import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { Alert } from '@/types'
import AlertRow from './AlertRow.vue'

const alert: Alert = {
  fingerprint: 'd1:filesystem.root.usage',
  deviceId: 'd1',
  deviceName: '猫盒-01',
  severity: 'critical',
  resource: '根文件系统',
  message: '根文件系统使用率过高',
  value: 96,
  unit: '%',
  status: 'firing',
  lastSeenAt: new Date().toISOString(),
}

describe('AlertRow', () => {
  it('shows source evidence and keeps recovery rule-driven', () => {
    const wrapper = mount(AlertRow, { props: { alert, actionable: true } })

    expect(wrapper.text()).toContain('当前 96.0%')
    expect(wrapper.text()).toContain('证据：根文件系统使用率过高')
    expect(wrapper.findAll('.alert-actions button')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('标记解决')
  })

  it('does not invent a zero measurement for connectivity alerts', () => {
    const wrapper = mount(AlertRow, {
      props: {
        alert: {
          ...alert,
          fingerprint: 'd1:offline',
          resource: 'collector',
          message: '设备未在 90 秒内上报',
          value: 0,
          unit: '',
        },
      },
    })

    expect(wrapper.text()).toContain('证据：设备未在 90 秒内上报')
    expect(wrapper.text()).not.toContain('当前值 0.0')
    expect(wrapper.find('.contract-note').text()).not.toContain('· 0.0 ·')
  })
})

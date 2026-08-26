import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RealtimeMetricCard from './RealtimeMetricCard.vue'

describe('RealtimeMetricCard', () => {
  it('shows paired values without truncating them and uses an application tooltip', async () => {
    const wrapper = mount(RealtimeMetricCard, {
      props: {
        label: '磁盘累计 I/O',
        value: '读 10.93 TiB · 写 8.70 TiB',
        parts: [{ label: '读', value: '10.93 TiB' }, { label: '写', value: '8.70 TiB' }],
        detail: 'disk.io.read.bytes_total · sda、sdb · 21 秒前',
        tooltipPlacement: 'above',
      },
    })

    expect(wrapper.findAll('.realtime-metric-parts > span')).toHaveLength(2)
    expect(wrapper.attributes('title')).toBeUndefined()
    await wrapper.trigger('mouseenter')
    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toContain('10.93 TiB')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('disk.io.read.bytes_total')
  })
})

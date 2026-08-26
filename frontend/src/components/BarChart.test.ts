import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BarChart from './BarChart.vue'

describe('BarChart', () => {
  it('shows production tooltip details on pointer and keyboard focus', async () => {
    const wrapper = mount(BarChart, {
      props: {
        unit: '%',
        items: [{ label: 'nasw /data', value: 77.7, color: '#2563eb', hint: '清理空间或扩容' }],
      },
    })
    const row = wrapper.get('.bar-chart-row')

    await row.trigger('mouseenter')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('77.7%')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('清理空间或扩容')

    await row.trigger('mouseleave')
    expect(wrapper.find('[role="tooltip"]').exists()).toBe(false)

    await row.trigger('focus')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('nasw /data')
  })

  it('shows only the hovered row when multiple resources have the same label', async () => {
    const wrapper = mount(BarChart, {
      props: {
        items: [
          { label: 'eth0', value: 0, hint: 'receive.errors' },
          { label: 'eth0', value: 1, hint: 'transmit.errors' },
          { label: 'eth0', value: 2, hint: 'receive.dropped' },
        ],
      },
    })
    const rows = wrapper.findAll('.bar-chart-row')
    await rows[1].trigger('mouseenter')

    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toContain('transmit.errors')
    expect(wrapper.get('[role="tooltip"]').text()).not.toContain('receive.errors')
  })
})

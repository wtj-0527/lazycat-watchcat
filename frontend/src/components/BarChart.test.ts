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
})

import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DonutChart from './DonutChart.vue'

describe('DonutChart', () => {
  it('shows the category value and percentage in a floating tooltip', async () => {
    const wrapper = mount(DonutChart, {
      props: {
        centerLabel: '设备',
        items: [
          { label: '健康', value: 3, color: '#118847' },
          { label: '警告', value: 1, color: '#c05600' },
        ],
      },
    })
    const visual = wrapper.get('.donut-visual')
    vi.spyOn(visual.element, 'getBoundingClientRect').mockReturnValue({
      left: 0, top: 0, width: 180, height: 180, right: 180, bottom: 180, x: 0, y: 0, toJSON: () => ({}),
    })
    await wrapper.findAll('.donut-segment')[1].trigger('mouseenter', { clientX: 130, clientY: 70 })

    expect(wrapper.get('.donut-tooltip').text()).toContain('警告')
    expect(wrapper.get('.donut-tooltip').text()).toContain('1 设备')
    expect(wrapper.get('.donut-tooltip').text()).toContain('25.0%')
    expect(wrapper.findAll('.donut-segment')[1].classes()).toContain('active')

    await visual.trigger('mouseleave')
    expect(wrapper.find('.donut-tooltip').exists()).toBe(false)
  })
})

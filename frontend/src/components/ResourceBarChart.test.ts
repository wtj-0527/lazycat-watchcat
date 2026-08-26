import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ResourceBarChart from './ResourceBarChart.vue'

describe('ResourceBarChart', () => {
  it('switches metrics, paginates all instances and shows one bounded tooltip', async () => {
    const items = Array.from({ length: 12 }, (_, index) => ({
      id: String(index),
      label: `应用${index}`,
      detail: `container-${index}`,
      cpu: index,
      memory: (index + 1) * 1024 ** 2,
      network: (index + 1) * 2048,
      io: (index + 1) * 4096,
      running: index !== 0,
    }))
    const wrapper = mount(ResourceBarChart, { props: { items } })
    const chart = wrapper.get('.resource-bar-explorer')
    vi.spyOn(chart.element, 'getBoundingClientRect').mockReturnValue({
      left: 0, top: 0, width: 1000, height: 800, right: 1000, bottom: 800, x: 0, y: 0, toJSON: () => ({}),
    })

    expect(wrapper.findAll('.resource-chart-row')).toHaveLength(10)
    expect(wrapper.get('.resource-chart-row').text()).toContain('应用11')
    expect(wrapper.get('.app-pagination').text()).toContain('共 12 条')

    await wrapper.findAll('[role="tab"]')[2].trigger('click')
    expect(wrapper.findAll('[role="tab"]')[2].attributes('aria-selected')).toBe('true')
    expect(wrapper.get('.resource-chart-row').text()).toContain('24.0 KiB')

    await wrapper.get('.resource-chart-row').trigger('mouseenter', { clientX: 980, clientY: 780 })
    expect(wrapper.get('[role="tooltip"]').text()).toContain('CPU')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('left: 706px')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('top: 658px')
  })
})

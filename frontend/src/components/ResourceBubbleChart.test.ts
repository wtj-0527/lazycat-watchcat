import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ResourceBubbleChart from './ResourceBubbleChart.vue'

describe('ResourceBubbleChart', () => {
  it('shows all resource values in one bounded tooltip', async () => {
    const wrapper = mount(ResourceBubbleChart, {
      props: {
        items: [{ id: 'a', label: '应用一', detail: 'main · abc', cpu: 24, memory: 1024 ** 3, network: 2048, io: 4096, running: true }],
      },
    })
    const visual = wrapper.get('.resource-bubble-chart')
    vi.spyOn(visual.element, 'getBoundingClientRect').mockReturnValue({
      left: 0, top: 0, width: 760, height: 330, right: 760, bottom: 330, x: 0, y: 0, toJSON: () => ({}),
    })
    await wrapper.get('.resource-bubble').trigger('mouseenter', { clientX: 730, clientY: 310 })

    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toContain('CPU')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('1.00 GiB')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('left: 474px')
  })
})

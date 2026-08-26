import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ResourceRankingBoard from './ResourceRankingBoard.vue'

describe('ResourceRankingBoard', () => {
  it('shows all items in four scrollable rankings and one bounded application tooltip', async () => {
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
    const wrapper = mount(ResourceRankingBoard, {
      props: { items },
    })
    const board = wrapper.get('.resource-ranking-board')
    vi.spyOn(board.element, 'getBoundingClientRect').mockReturnValue({
      left: 0, top: 0, width: 1000, height: 620, right: 1000, bottom: 620, x: 0, y: 0, toJSON: () => ({}),
    })

    expect(wrapper.get('.resource-summary-strip').text()).toContain('12')
    expect(wrapper.findAll('.resource-ranking-column')).toHaveLength(4)
    expect(wrapper.findAll('.resource-ranking-list')).toHaveLength(4)
    expect(wrapper.findAll('.resource-ranking-row')).toHaveLength(48)
    expect(wrapper.findAll('.resource-ranking-list')[0].attributes('aria-label')).toContain('可上下滚动')

    await wrapper.findAll('.resource-ranking-row')[0].trigger('mouseenter', { clientX: 980, clientY: 600 })
    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toContain('CPU')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('12.0 MiB')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('left: 706px')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('top: 478px')
  })
})

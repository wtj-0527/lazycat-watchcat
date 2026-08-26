import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ResourceRankingBoard from './ResourceRankingBoard.vue'

describe('ResourceRankingBoard', () => {
  it('shows summaries, four rankings and one bounded application tooltip', async () => {
    const wrapper = mount(ResourceRankingBoard, {
      props: {
        items: [
          { id: 'a', label: '应用一', detail: 'main · abc', cpu: 24, memory: 1024 ** 3, network: 2048, io: 4096, running: true },
          { id: 'b', label: '应用二', detail: 'worker · def', cpu: 12, memory: 512 * 1024 ** 2, network: 1024, io: 2048, running: false },
        ],
      },
    })
    const board = wrapper.get('.resource-ranking-board')
    vi.spyOn(board.element, 'getBoundingClientRect').mockReturnValue({
      left: 0, top: 0, width: 1000, height: 620, right: 1000, bottom: 620, x: 0, y: 0, toJSON: () => ({}),
    })

    expect(wrapper.get('.resource-summary-strip').text()).toContain('2')
    expect(wrapper.findAll('.resource-ranking-column')).toHaveLength(4)
    expect(wrapper.findAll('.resource-ranking-row')).toHaveLength(8)

    await wrapper.findAll('.resource-ranking-row')[0].trigger('mouseenter', { clientX: 980, clientY: 600 })
    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(1)
    expect(wrapper.get('[role="tooltip"]').text()).toContain('CPU')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('1.00 GiB')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('left: 706px')
    expect(wrapper.get('[role="tooltip"]').attributes('style')).toContain('top: 478px')
  })
})

import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import LineChart from './LineChart.vue'

describe('LineChart', () => {
  it('renders real points and downsamples long histories for browser stability', () => {
    const points = Array.from({ length: 2000 }, (_, index) => ({ value: index % 100, label: String(index), at: `sample ${index}` }))
    const wrapper = mount(LineChart, { props: { series: [{ name: '内存', color: '#2563eb', points }], max: 100, unit: '%' } })
    expect(wrapper.find('.chart-line').attributes('d')).toBeTruthy()
    expect(wrapper.findAll('circle')).toHaveLength(120)
    expect(wrapper.text()).toContain('内存')
  })

  it('shows a floating multi-series tooltip at the hovered time point', async () => {
    const wrapper = mount(LineChart, {
      props: {
        series: [
          { name: '接收', color: '#15803d', points: [{ value: 1, at: '10:00' }, { value: 4, at: '10:05' }] },
          { name: '发送', color: '#c05600', points: [{ value: 2, at: '10:00' }, { value: 8, at: '10:05' }] },
        ],
        unit: ' KiB/s',
      },
    })
    const svg = wrapper.get('svg')
    vi.spyOn(svg.element, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0, width: 900, height: 220, right: 900, bottom: 220, x: 0, y: 0, toJSON: () => ({}) })
    await svg.trigger('mousemove', { clientX: 880 })

    expect(wrapper.get('.chart-tooltip').text()).toContain('10:05')
    expect(wrapper.get('.chart-tooltip').text()).toContain('接收4.00 KiB/s')
    expect(wrapper.get('.chart-tooltip').text()).toContain('发送8.00 KiB/s')
    expect(wrapper.find('.chart-hover-layer').exists()).toBe(true)

    await svg.trigger('mouseleave')
    expect(wrapper.find('.chart-tooltip').exists()).toBe(false)
  })
})

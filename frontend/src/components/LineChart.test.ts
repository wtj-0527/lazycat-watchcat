import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LineChart from './LineChart.vue'

describe('LineChart', () => {
  it('renders real points and downsamples long histories for browser stability', () => {
    const points = Array.from({ length: 2000 }, (_, index) => ({ value: index % 100, label: String(index), at: `sample ${index}` }))
    const wrapper = mount(LineChart, { props: { series: [{ name: '内存', color: '#2563eb', points }], max: 100, unit: '%' } })
    expect(wrapper.find('.chart-line').attributes('d')).toBeTruthy()
    expect(wrapper.findAll('circle')).toHaveLength(120)
    expect(wrapper.text()).toContain('内存')
  })
})

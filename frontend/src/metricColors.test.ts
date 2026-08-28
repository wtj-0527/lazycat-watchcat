import { describe, expect, it } from 'vitest'
import { metricColors } from './metricColors'

describe('metricColors', () => {
  it('keeps equivalent traffic directions visually consistent', () => {
    expect(metricColors.read).toBe(metricColors.receive)
    expect(metricColors.write).toBe(metricColors.transmit)
  })

  it('keeps primary system metrics visually distinct', () => {
    expect(new Set([
      metricColors.cpu,
      metricColors.memory,
      metricColors.swap,
      metricColors.storage,
    ]).size).toBe(4)
  })
})

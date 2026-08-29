import { describe, expect, it } from 'vitest'
import { globalDeviceId, selectGlobalDevice } from './deviceScope'

describe('global device scope', () => {
  it('persists the selected device and can return to all devices', () => {
    selectGlobalDevice('device-canway')
    expect(globalDeviceId.value).toBe('device-canway')
    expect(localStorage.getItem('watchcatDeviceScope')).toBe('device-canway')

    selectGlobalDevice('all')
    expect(globalDeviceId.value).toBe('all')
  })
})

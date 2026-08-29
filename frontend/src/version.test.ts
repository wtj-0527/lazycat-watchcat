import { describe, expect, it } from 'vitest'
import { versionReload } from './version'

describe('frontend version reload', () => {
  it('does not reload when frontend and backend versions match', () => {
    expect(versionReload('1.4.47', '1.4.47', 'https://watchcat.example/#storage')).toBeUndefined()
  })

  it('creates a cache-busting URL when the backend is newer', () => {
    const result = versionReload('1.4.46', '1.4.47', 'https://watchcat.example/#storage')

    expect(result?.key).toBe('watchcatVersionReload:1.4.47')
    expect(result?.url).toBe('https://watchcat.example/?_watchcat_version=1.4.47#storage')
  })

  it('replaces an existing cache-busting version without losing the hash', () => {
    const result = versionReload(
      '1.4.46',
      '1.4.48',
      'https://watchcat.example/?_watchcat_version=1.4.47#storage',
    )

    expect(result?.url).toBe('https://watchcat.example/?_watchcat_version=1.4.48#storage')
  })
})

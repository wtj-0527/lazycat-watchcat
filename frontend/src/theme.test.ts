import { beforeEach, describe, expect, it } from 'vitest'
import { applyTheme, resolvedTheme, storedTheme } from './theme'

describe('theme', () => {
  beforeEach(() => localStorage.clear())

  it('defaults to device and resolves the device color scheme', () => {
    expect(storedTheme()).toBe('device')
    expect(resolvedTheme('device', true)).toBe('dark')
    expect(resolvedTheme('device', false)).toBe('light')
  })

  it('persists explicit modes and applies document attributes', () => {
    localStorage.setItem('watchcatTheme', 'dark')
    expect(storedTheme()).toBe('dark')
    applyTheme('dark', false)
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.dataset.themeMode).toBe('dark')
    expect(document.documentElement.style.colorScheme).toBe('dark')
  })
})

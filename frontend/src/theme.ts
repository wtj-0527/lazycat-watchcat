export type ThemeMode = 'light' | 'dark' | 'device'

export function storedTheme(): ThemeMode {
  const value = localStorage.getItem('watchcatTheme')
  return value === 'light' || value === 'dark' || value === 'device' ? value : 'device'
}

export function resolvedTheme(mode: ThemeMode, deviceDark: boolean): 'light' | 'dark' {
  return mode === 'device' ? (deviceDark ? 'dark' : 'light') : mode
}

export function applyTheme(mode: ThemeMode, deviceDark: boolean) {
  const resolved = resolvedTheme(mode, deviceDark)
  document.documentElement.dataset.theme = resolved
  document.documentElement.dataset.themeMode = mode
  document.documentElement.style.colorScheme = resolved
}

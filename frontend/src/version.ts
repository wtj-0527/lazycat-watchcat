export const frontendVersion = __APP_VERSION__

export interface VersionReload {
  key: string
  url: string
}

export function versionReload(frontend: string, backend: string, href: string): VersionReload | undefined {
  const next = backend.trim()
  if (!next || frontend === next) return undefined

  const url = new URL(href)
  url.searchParams.set('_watchcat_version', next)
  return {
    key: `watchcatVersionReload:${next}`,
    url: url.toString(),
  }
}

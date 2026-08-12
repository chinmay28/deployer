import type { Installation } from '../types'

/** Loopback names an app on the machine asking, which is exactly wrong when the
 *  machine asking is a phone. Deployer registers itself as 127.0.0.1 because
 *  that is how it reaches itself over SSH, so any app on the home host inherits
 *  an address only the home host can use. */
const LOOPBACK = new Set(['127.0.0.1', 'localhost', '0.0.0.0', '::1', '::'])

/**
 * launchUrl is where "Open" should send the browser, or null where Deployer
 * has nothing solid to send it to.
 *
 * The server does the working out; the one thing it cannot know is which of a
 * machine's addresses the phone used to get here. Where it answered with a
 * loopback address, the hostname this page was loaded from is the same machine
 * by a name that travels — Deployer is served from it.
 */
export function launchUrl(installation: Installation | undefined | null): string | null {
  if (!installation?.url) return null
  try {
    const url = new URL(installation.url)
    if (LOOPBACK.has(url.hostname.replace(/^\[|\]$/g, '')) && window.location.hostname) {
      url.hostname = window.location.hostname
    }
    return url.toString()
  } catch {
    return null
  }
}

/** The installations of an app that can actually be opened, each with the
 *  address to open it at. */
export function launchable(
  installations: Installation[],
): { installation: Installation; url: string }[] {
  return installations.flatMap((installation) => {
    const url = launchUrl(installation)
    return url ? [{ installation, url }] : []
  })
}

/**
 * open sends the browser to an app. A PWA installed to the home screen has no
 * address bar, so _blank hands the link to the phone's default browser, where
 * the app can be bookmarked and shared like any other page.
 */
export function openApp(url: string) {
  window.open(url, '_blank', 'noopener,noreferrer')
}

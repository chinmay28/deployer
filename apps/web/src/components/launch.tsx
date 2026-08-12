import { useState, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import { launchable, openApp } from '../lib/launch'
import type { Installation } from '../types'
import { Sheet } from './ui'

/**
 * The button that opens a deployed app in the phone's browser. Deployer knows
 * an app is healthy and which port it answers on; this is the one tap that
 * turns knowing into using it.
 *
 * It shows nothing at all when the app has not said enough for Deployer to name
 * an address — a dead "Open" would be worse than none. Where the same app is
 * deployed to several hosts there is no right one to guess at, so it asks.
 */
export function LaunchButton({
  installations,
  className = 'ghost',
  label = 'Open',
}: {
  /** The installations this button can open: one card's, or all of an app's. */
  installations: Installation[]
  className?: string
  label?: string
}) {
  const [picking, setPicking] = useState(false)
  const targets = launchable(installations)
  if (targets.length === 0) return null

  const press = (e: MouseEvent) => {
    // The button usually sits on a card that is itself a link to the app, and
    // opening the app is not navigating to its page.
    e.preventDefault()
    e.stopPropagation()
    if (targets.length === 1) {
      openApp(targets[0].url)
      return
    }
    setPicking(true)
  }

  const close = () => setPicking(false)

  return (
    <>
      <button
        className={className}
        onClick={press}
        title={targets.length === 1 ? targets[0].url : 'Open on…'}
      >
        {label}
      </button>

      {/* Through a portal: the sheet is a full-screen overlay, and the card it
          was tapped on is usually a link that would swallow its clicks. */}
      {picking &&
        createPortal(
          <Sheet
            title={`Open ${targets[0].installation.appName || 'app'}`}
            subtitle="It is deployed to more than one host."
            onClose={close}
          >
            <div className="pick-list">
              {targets.map(({ installation, url }) => (
                <button
                  key={installation.id}
                  className="secondary"
                  onClick={() => {
                    openApp(url)
                    close()
                  }}
                >
                  <span className="title">{installation.hostName}</span>
                  <span className="sub">{url}</span>
                </button>
              ))}
            </div>
          </Sheet>,
          document.body,
        )}
    </>
  )
}

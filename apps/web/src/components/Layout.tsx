import type { ReactNode } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { APP_VERSION } from '../version'

/** Page gives every screen the same sticky header and scroll area. */
export function Page({
  title,
  back,
  action,
  brand,
  children,
}: {
  title: string
  /** Path to go back to; omitted on the four tab roots. */
  back?: string
  action?: ReactNode
  /** Show the app icon and the running version alongside the title. Only the
   * Overview does: it is the screen whose title *is* the app's name. */
  brand?: boolean
  children: ReactNode
}) {
  const navigate = useNavigate()
  return (
    <>
      <header className="header">
        {back && (
          <button className="back" onClick={() => navigate(back)} aria-label="Back">
            ‹ Back
          </button>
        )}
        {brand ? (
          <>
            <img className="brand-logo" src="/icon.svg" alt="" aria-hidden="true" />
            {/* Name over version, as a lockup — the version reads as part of
                the name rather than as another thing on the screen. */}
            <div className="brand">
              <h1>{title}</h1>
              <span className="brand-version">{APP_VERSION}</span>
            </div>
          </>
        ) : (
          <h1>{title}</h1>
        )}
        {action}
      </header>
      <main className="main">{children}</main>
    </>
  )
}

export function TabBar() {
  return (
    <nav className="tabbar" aria-label="Sections">
      <NavLink to="/" end>
        <HomeIcon />
        Overview
      </NavLink>
      <NavLink to="/hosts">
        <ServerIcon />
        Hosts
      </NavLink>
      <NavLink to="/apps">
        <AppsIcon />
        Apps
      </NavLink>
      <NavLink to="/settings">
        <GearIcon />
        Settings
      </NavLink>
    </nav>
  )
}

const stroke = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

function HomeIcon() {
  return (
    <svg viewBox="0 0 24 24" {...stroke}>
      <path d="M3 10.5 12 3l9 7.5" />
      <path d="M5.5 9.5V20h13V9.5" />
    </svg>
  )
}

function ServerIcon() {
  return (
    <svg viewBox="0 0 24 24" {...stroke}>
      <rect x="3" y="4" width="18" height="7" rx="2" />
      <rect x="3" y="13" width="18" height="7" rx="2" />
      <path d="M7 7.5h.01M7 16.5h.01" />
    </svg>
  )
}

function AppsIcon() {
  return (
    <svg viewBox="0 0 24 24" {...stroke}>
      <path d="M12 3 21 7.5v9L12 21l-9-4.5v-9z" />
      <path d="M3 7.5 12 12l9-4.5M12 12v9" />
    </svg>
  )
}

function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" {...stroke}>
      <circle cx="12" cy="12" r="3.2" />
      <path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 9 19.4a1.6 1.6 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.6 9a1.6 1.6 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 1.8.3H9a1.6 1.6 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V9a1.6 1.6 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1z" />
    </svg>
  )
}

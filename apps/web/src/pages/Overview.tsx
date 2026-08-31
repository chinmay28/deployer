import { Link, useNavigate } from 'react-router-dom'
import type { MouseEvent } from 'react'
import { api } from '../api'
import { HostMeters } from '../components/host'
import { LaunchButton } from '../components/launch'
import { TabPage } from '../components/Layout'
import { HomeBadge, HostBadge } from '../components/status'
import { Empty, Loading, useLoader } from '../components/ui'
import { ago, ports } from '../lib/format'
import type { Host, Installation } from '../types'

/** The dashboard: one card per host, each carrying what is deployed to it,
 *  refreshed while it is open. */
export default function Overview() {
  const { data, error, loading, offline } = useLoader(() => api.overview(), [], 5000)

  return (
    <TabPage>
      <Loading error={error} offline={offline} hasData={!!data} />
      {!data && loading && <div className="empty">Loading…</div>}

      {data &&
        (data.hosts.length === 0 ? (
          <Empty
            message="No hosts yet. Add the machine you want to deploy to."
            action={
              <Link to="/hosts/new">
                <button className="primary">Add a host</button>
              </Link>
            }
          />
        ) : (
          data.hosts.map((host) => (
            <HostSummary
              key={host.id}
              host={host}
              installs={data.installations.filter((install) => install.hostId === host.id)}
            />
          ))
        ))}
    </TabPage>
  )
}

function HostSummary({ host, installs }: { host: Host; installs: Installation[] }) {
  const sample = host.latest
  return (
    <Link className="card" to={`/hosts/${host.id}`}>
      <div className="row between">
        <div className="grow">
          <div className="title">{host.name}</div>
          <div className="sub">
            {host.address}
            {sample ? ` · ${ago(sample.takenAt)}` : ''}
          </div>
        </div>
        <div className="row" style={{ gap: 6 }}>
          {host.isSelf && <HomeBadge />}
          <HostBadge status={host.status} />
        </div>
      </div>

      {sample ? (
        <HostMeters sample={sample} />
      ) : (
        <div className="sub" style={{ marginTop: 10 }}>
          {host.lastError ? host.lastError : 'Waiting for the first reading…'}
        </div>
      )}

      {installs.length > 0 && <HostApps installs={installs} />}
    </Link>
  )
}

/**
 * A host's apps, trouble first: a strip that says how many are well at a
 * glance, then every app as a row — a red one that says what is wrong for an
 * app that is not responding, a quiet one-liner for the rest.
 */
function HostApps({ installs }: { installs: Installation[] }) {
  const failing = installs.filter((i) => i.healthStatus === 'failing')
  const passing = installs.filter((i) => i.healthStatus === 'passing')
  const checked = passing.length + failing.length
  const rows = [...failing, ...installs.filter((i) => i.healthStatus !== 'failing')]

  return (
    <>
      <div className="list-divider" />
      <div className="meter-label">
        <span>Apps</span>
        {/* "4 of 8 healthy" where checks run; a bare count where none do,
            because "0 of 3 healthy" would read as trouble on a host whose
            apps simply have no checks. */}
        <b>{checked > 0 ? `${passing.length} of ${installs.length} healthy` : installs.length}</b>
      </div>
      <div className="seg-strip">
        {installs.map((install) => (
          <span
            key={install.id}
            className={
              install.healthStatus === 'passing'
                ? 'good'
                : install.healthStatus === 'failing'
                  ? 'bad'
                  : ''
            }
          />
        ))}
      </div>

      {rows.map((install, index) => (
        <div key={install.id} style={{ marginTop: index === 0 ? 6 : 0 }}>
          {index > 0 && <div className="list-divider inset" />}
          <AppRow install={install} />
        </div>
      ))}
    </>
  )
}

const DOT_COLOR: Record<Installation['healthStatus'], string> = {
  passing: 'var(--good)',
  failing: 'var(--bad)',
  unknown: 'var(--text-dim)',
  unchecked: 'var(--text-dim)',
}

/** One app on a host's card. The card is a link to the host, so the row
 *  navigates to the app by hand — the way the Open button already keeps its
 *  tap to itself. */
function AppRow({ install }: { install: Installation }) {
  const navigate = useNavigate()
  const open = (e: MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    navigate(`/apps/${install.appId}`)
  }
  return (
    <div className="row" style={{ minHeight: 44 }} onClick={open}>
      <span className="dot" style={{ color: DOT_COLOR[install.healthStatus] }} />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="truncate" style={{ fontSize: 14 }}>
          {install.appName}
          {install.version && (
            <span className="sub" style={{ fontSize: 12 }}>
              {' '}
              {install.version}
            </span>
          )}
        </div>
        {install.healthStatus === 'failing' && (
          <div className="sub" style={{ fontSize: 12, color: 'var(--bad)' }}>
            Not responding
          </div>
        )}
      </div>
      <span className="sub" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
        {ports(install.ports)}
      </span>
      <LaunchButton installations={[install]} className="open-chip icon-only" label="" />
    </div>
  )
}

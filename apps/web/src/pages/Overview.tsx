import { Link, useNavigate } from 'react-router-dom'
import type { MouseEvent } from 'react'
import { api } from '../api'
import { HostMeters } from '../components/host'
import { LaunchButton } from '../components/launch'
import { TabPage } from '../components/Layout'
import { HomeBadge, HostBadge } from '../components/status'
import { Empty, Loading, useLoader } from '../components/ui'
import { ago, parts, ports } from '../lib/format'
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
 * A host's apps by exception: a strip that says how many are well, rows only
 * for the ones that are not, and everything quiet on one dim line. A card is
 * for spotting trouble; the full list lives on the host's own page.
 */
function HostApps({ installs }: { installs: Installation[] }) {
  const failing = installs.filter((i) => i.healthStatus === 'failing')
  const passing = installs.filter((i) => i.healthStatus === 'passing')
  const unchecked = installs.filter((i) => i.healthStatus === 'unchecked')
  const unknown = installs.filter((i) => i.healthStatus === 'unknown')
  const checked = passing.length + failing.length
  const names = (list: Installation[]) => list.map((i) => i.appName).join(', ')

  // The dim closing line: whatever is not worth a row of its own.
  const quiet = parts(
    passing.length > 0 && `Healthy: ${names(passing)}`,
    unchecked.length > 0 && `No health check: ${names(unchecked)}`,
    unknown.length > 0 && `Not checked yet: ${names(unknown)}`,
  )

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

      {failing.map((install, index) => (
        <div key={install.id} style={{ marginTop: index === 0 ? 6 : 0 }}>
          {index > 0 && <div className="list-divider inset" />}
          <ExceptionRow install={install} />
        </div>
      ))}

      {quiet && (
        <>
          {failing.length > 0 && <div className="list-divider" />}
          <div className="sub truncate" style={{ marginTop: failing.length > 0 ? 0 : 10 }}>
            {quiet}
          </div>
        </>
      )}
    </>
  )
}

/** One app that needs attention. The card is a link to the host, so the row
 *  navigates to the app by hand — the way the Open button already keeps its
 *  tap to itself. */
function ExceptionRow({ install }: { install: Installation }) {
  const navigate = useNavigate()
  const open = (e: MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    navigate(`/apps/${install.appId}`)
  }
  return (
    <div className="row" style={{ minHeight: 44 }} onClick={open}>
      <span className="dot" style={{ color: 'var(--bad)' }} />
      <div className="grow" style={{ minWidth: 0 }}>
        <div className="truncate" style={{ fontSize: 14 }}>
          {install.appName}
        </div>
        <div className="sub" style={{ fontSize: 12, color: 'var(--bad)' }}>
          {parts('Not responding', install.version, ports(install.ports))}
        </div>
      </div>
      <LaunchButton installations={[install]} className="open-chip icon-only" label="" />
    </div>
  )
}

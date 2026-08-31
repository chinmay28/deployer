import { Link, useNavigate } from 'react-router-dom'
import type { MouseEvent } from 'react'
import { api } from '../api'
import { HostMeters } from '../components/host'
import { LaunchButton } from '../components/launch'
import { TabPage } from '../components/Layout'
import { HealthBadge, HomeBadge, HostBadge } from '../components/status'
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

      {installs.length > 0 && (
        <>
          <div className="list-divider" />
          {installs.map((install) => (
            <InstallRow key={install.id} install={install} />
          ))}
        </>
      )}
    </Link>
  )
}

/** One deployed app on a host's card. The card is a link to the host, so the
 *  row navigates to the app by hand — the way the Open button already keeps
 *  its tap to itself. */
function InstallRow({ install }: { install: Installation }) {
  const navigate = useNavigate()
  const open = (e: MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    navigate(`/apps/${install.appId}`)
  }
  const detail = parts(install.version, ports(install.ports))
  return (
    <div className="row between" style={{ padding: '5px 0' }} onClick={open}>
      <div className="grow">
        <div style={{ fontSize: 14 }}>{install.appName}</div>
        {detail && <div className="sub">{detail}</div>}
      </div>
      <div className="row" style={{ gap: 6 }}>
        <LaunchButton installations={[install]} />
        <HealthBadge status={install.healthStatus} />
      </div>
    </div>
  )
}

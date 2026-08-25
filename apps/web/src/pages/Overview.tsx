import { Link } from 'react-router-dom'
import { api } from '../api'
import { LaunchButton } from '../components/launch'
import { TabPage } from '../components/Layout'
import { DeploymentBadge, HealthBadge, HomeBadge, HostBadge } from '../components/status'
import { Empty, Loading, Meter, SectionTitle, useLoader } from '../components/ui'
import { primaryDisk } from '../lib/disk'
import { ago, bytes, parts, percent, ports, time } from '../lib/format'
import type { Host } from '../types'

/** The dashboard: everything in one request, refreshed while it is open. */
export default function Overview() {
  const { data, error, loading, offline } = useLoader(() => api.overview(), [], 5000)

  return (
    <TabPage>
      <Loading error={error} offline={offline} hasData={!!data} />
      {!data && loading && <div className="empty">Loading…</div>}

      {data && (
        <>
          <SectionTitle>Hosts</SectionTitle>
          {data.hosts.length === 0 ? (
            <Empty
              message="No hosts yet. Add the machine you want to deploy to."
              action={
                <Link to="/hosts/new">
                  <button className="primary">Add a host</button>
                </Link>
              }
            />
          ) : (
            data.hosts.map((host) => <HostSummary key={host.id} host={host} />)
          )}

          <SectionTitle>Deployed apps</SectionTitle>
          {data.installations.length === 0 ? (
            <Empty
              message="Nothing deployed yet. Add an app, then deploy it to a host."
              action={
                <Link to="/apps/new">
                  <button className="primary">Add an app</button>
                </Link>
              }
            />
          ) : (
            data.installations.map((install) => (
              <Link key={install.id} className="card" to={`/apps/${install.appId}`}>
                <div className="row between">
                  <div className="grow">
                    <div className="title">{install.appName}</div>
                    <div className="sub">
                      {parts(
                        `on ${install.hostName}`,
                        install.version,
                        ports(install.ports),
                        `updated ${ago(install.updatedAt)}`,
                      )}
                    </div>
                  </div>
                  <div className="row" style={{ gap: 6 }}>
                    <LaunchButton installations={[install]} />
                    <HealthBadge status={install.healthStatus} />
                  </div>
                </div>
              </Link>
            ))
          )}

          {data.recentDeployments.length > 0 && (
            <>
              <SectionTitle>Recent deployments</SectionTitle>
              {data.recentDeployments.map((deployment) => (
                <Link key={deployment.id} className="card" to={`/deployments/${deployment.id}`}>
                  <div className="row between">
                    <div className="grow">
                      {/* An uninstall is said in words rather than with the
                          arrow a deploy gets: it went the other way, and this
                          is not a row to misread at a glance. */}
                      <div className="title">
                        {deployment.kind === 'uninstall'
                          ? `Uninstall ${deployment.appName} from ${deployment.hostName}`
                          : `${deployment.appName} → ${deployment.hostName}`}
                      </div>
                      <div className="sub">{time(deployment.startedAt)}</div>
                    </div>
                    <DeploymentBadge status={deployment.status} />
                  </div>
                </Link>
              ))}
            </>
          )}
        </>
      )}
    </TabPage>
  )
}

function HostSummary({ host }: { host: Host }) {
  const sample = host.latest
  const disk = primaryDisk(sample?.disks)
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
        <div className="meters">
          <Meter label="CPU" value={sample.cpuPct} display={`${Math.round(sample.cpuPct)}%`} />
          <Meter
            label="Memory"
            used={sample.memUsed}
            total={sample.memTotal}
            display={`${Math.round(percent(sample.memUsed, sample.memTotal))}%`}
          />
          {disk ? (
            <Meter
              label={`Disk ${disk.mount}`}
              used={disk.usedBytes}
              total={disk.totalBytes}
              display={bytes(disk.totalBytes - disk.usedBytes) + ' free'}
            />
          ) : (
            <Meter label="Disk" value={0} display="—" />
          )}
        </div>
      ) : (
        <div className="sub" style={{ marginTop: 10 }}>
          {host.lastError ? host.lastError : 'Waiting for the first reading…'}
        </div>
      )}
    </Link>
  )
}

import { Link } from 'react-router-dom'
import { api } from '../api'
import { HostMeters } from '../components/host'
import { TabPage } from '../components/Layout'
import { HomeBadge, HostBadge } from '../components/status'
import { Empty, Loading, useLoader } from '../components/ui'
import { ago, parts, uptime } from '../lib/format'

export default function Hosts() {
  const { data, error, loading, offline } = useLoader(() => api.hosts(), [], 10000)

  return (
    <TabPage>
      <Loading error={error} offline={offline} hasData={!!data} />
      {!data && loading && <div className="empty">Loading…</div>}

      {data?.length === 0 && (
        <Empty
          message="Add a machine Deployer can reach over SSH — a hostname like nakedpi.local, or an IP address."
          action={
            <Link to="/hosts/new">
              <button className="primary">Add a host</button>
            </Link>
          }
        />
      )}

      {data?.map((host) => {
        const sample = host.latest
        return (
          <Link key={host.id} className="card" to={`/hosts/${host.id}`}>
            <div className="row between">
              <div className="grow">
                <div className="title">{host.name}</div>
                <div className="sub">
                  {host.username}@{host.address}
                  {host.port !== 22 ? `:${host.port}` : ''}
                </div>
              </div>
              <div className="row" style={{ gap: 6 }}>
                {host.isSelf && <HomeBadge />}
                <HostBadge status={host.status} />
              </div>
            </div>

            {sample && <HostMeters sample={sample} />}

            <div className="sub" style={{ marginTop: sample ? 8 : 10 }}>
              {parts(
                host.isSelf && 'Deployer runs here',
                host.os || 'Not yet identified',
                host.arch,
                host.kernel,
              )}
            </div>
            {/* The machine's own state where there is a reading, and why there
                is none where there is not. */}
            {sample ? (
              <div className="sub" style={{ marginTop: 4 }}>
                {parts(
                  `up ${uptime(sample.uptimeS)}`,
                  `load ${sample.load1.toFixed(2)}`,
                  sample.tempC != null && `${sample.tempC.toFixed(1)} °C`,
                  host.status === 'online' && !host.sudoOk && 'no passwordless sudo',
                  `seen ${ago(host.lastSeenAt)}`,
                )}
              </div>
            ) : (
              <div className="sub" style={{ marginTop: 4 }}>
                {parts(
                  `seen ${ago(host.lastSeenAt)}`,
                  host.lastError || 'Waiting for the first reading…',
                )}
              </div>
            )}
          </Link>
        )
      })}
    </TabPage>
  )
}

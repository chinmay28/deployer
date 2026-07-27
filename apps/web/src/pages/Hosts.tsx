import { Link } from 'react-router-dom'
import { api } from '../api'
import { Page } from '../components/Layout'
import { HomeBadge, HostBadge } from '../components/status'
import { Banner, Empty, useLoader } from '../components/ui'
import { ago } from '../lib/format'

export default function Hosts() {
  const { data, error, loading } = useLoader(() => api.hosts(), [], 10000)

  return (
    <Page
      title="Hosts"
      action={
        <Link to="/hosts/new">
          <button className="ghost">Add</button>
        </Link>
      }
    >
      {error && <Banner tone="bad">{error}</Banner>}
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

      {data?.map((host) => (
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
          <div className="sub" style={{ marginTop: 8 }}>
            {host.isSelf ? 'Deployer runs here · ' : ''}
            {host.os ? `${host.os} · ${host.arch}` : 'Not yet identified'} · seen {ago(host.lastSeenAt)}
          </div>
        </Link>
      ))}
    </Page>
  )
}

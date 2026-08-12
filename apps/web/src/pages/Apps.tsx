import { Link } from 'react-router-dom'
import { api } from '../api'
import { LaunchButton } from '../components/launch'
import { TabPage } from '../components/Layout'
import { HealthBadge } from '../components/status'
import { Empty, Loading, useLoader } from '../components/ui'
import { ago, parts, ports, versions } from '../lib/format'

export default function Apps() {
  const { data, error, loading, offline } = useLoader(() => api.apps(), [], 15000)
  const { data: installs } = useLoader(() => api.installations(), [], 15000)

  return (
    <TabPage>
      <Loading error={error} offline={offline} hasData={!!data} />
      {!data && loading && <div className="empty">Loading…</div>}

      {data?.length === 0 && (
        <Empty
          message="An app is a name plus the one-line command that installs it, like a quickstart script."
          action={
            <Link to="/apps/new">
              <button className="primary">Add an app</button>
            </Link>
          }
        />
      )}

      {data?.map((app) => {
        const deployed = installs?.filter((i) => i.appId === app.id) ?? []
        // Across hosts an app is normally on the same port, so one list of
        // ports describes all of them.
        const listening = [...new Set(deployed.flatMap((i) => i.ports ?? []))].sort((a, b) => a - b)
        const running = versions(deployed.map((i) => i.version))
        return (
          <Link key={app.id} className="card" to={`/apps/${app.id}`}>
            <div className="row between">
              <div className="grow">
                <div className="title">{app.name}</div>
                <div className="sub">{app.description || 'No description'}</div>
              </div>
              <div className="row" style={{ gap: 6 }}>
                <LaunchButton installations={deployed} />
                {deployed.length > 0 && <HealthBadge status={deployed[0].healthStatus} />}
              </div>
            </div>
            <div className="sub" style={{ marginTop: 8 }}>
              {deployed.length === 0
                ? 'Not deployed anywhere yet'
                : deployed.length === 1
                  ? parts(
                      `On ${deployed[0].hostName}`,
                      running,
                      ports(listening),
                      `updated ${ago(deployed[0].updatedAt)}`,
                    )
                  : parts(`On ${deployed.length} hosts`, running, ports(listening))}
            </div>
          </Link>
        )
      })}
    </TabPage>
  )
}

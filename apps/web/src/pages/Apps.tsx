import { Link } from 'react-router-dom'
import { api } from '../api'
import { Page } from '../components/Layout'
import { HealthBadge } from '../components/status'
import { Empty, Loading, useLoader } from '../components/ui'
import { ago } from '../lib/format'

export default function Apps() {
  const { data, error, loading, offline } = useLoader(() => api.apps(), [], 15000)
  const { data: installs } = useLoader(() => api.installations(), [], 15000)

  return (
    <Page
      title="Apps"
      action={
        <Link to="/apps/new">
          <button className="ghost">Add</button>
        </Link>
      }
    >
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
        return (
          <Link key={app.id} className="card" to={`/apps/${app.id}`}>
            <div className="row between">
              <div className="grow">
                <div className="title">{app.name}</div>
                <div className="sub">{app.description || 'No description'}</div>
              </div>
              {deployed.length > 0 && <HealthBadge status={deployed[0].healthStatus} />}
            </div>
            <div className="sub" style={{ marginTop: 8 }}>
              {deployed.length === 0
                ? 'Not deployed anywhere yet'
                : deployed.length === 1
                  ? `On ${deployed[0].hostName} · updated ${ago(deployed[0].updatedAt)}`
                  : `On ${deployed.length} hosts`}
            </div>
          </Link>
        )
      })}
    </Page>
  )
}

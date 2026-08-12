import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { LaunchButton } from '../components/launch'
import { Page } from '../components/Layout'
import { DeploymentBadge, HealthBadge } from '../components/status'
import { Banner, Card, Empty, Field, Loading, SectionTitle, Sheet, useLoader } from '../components/ui'
import { ago, parts, ports, shellQuote, time } from '../lib/format'
import type { App, Host, Installation } from '../types'

export default function AppDetail() {
  const { id } = useParams()
  const appId = Number(id)
  const navigate = useNavigate()

  const { data: app, error, offline } = useLoader(() => api.app(appId), [appId], 10000)
  const { data: hosts } = useLoader(() => api.hosts(), [], 15000)
  const { data: installs, reload: reloadInstalls } = useLoader(
    () => api.installations(),
    [],
    10000,
  )
  const { data: history, reload: reloadHistory } = useLoader(
    () => api.deployments({ appId, limit: 10 }),
    [appId],
    10000,
  )

  const [deployTarget, setDeployTarget] = useState<Installation | 'new' | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const mine = useMemo(() => installs?.filter((i) => i.appId === appId) ?? [], [installs, appId])

  const removeApp = async () => {
    try {
      await api.deleteApp(appId)
      navigate('/apps', { replace: true })
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e))
      setConfirmDelete(false)
    }
  }

  /**
   * Which action is in flight, as `action:installationId`. Check goes over SSH
   * or out to the app's own URL and can take seconds, so the button has to say
   * it is working — a press that visibly does nothing reads as a press that
   * did not land, and gets made again.
   */
  const [pending, setPending] = useState<string | null>(null)
  const running = (action: string, installation: Installation) =>
    pending === `${action}:${installation.id}`

  const act = async (action: string, installation: Installation, run: () => Promise<unknown>) => {
    setPending(`${action}:${installation.id}`)
    setActionError(null)
    try {
      await run()
      reloadInstalls()
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e))
    } finally {
      setPending(null)
    }
  }

  const check = (installation: Installation) =>
    act('check', installation, () => api.checkInstallation(installation.id))

  const forget = (installation: Installation) =>
    act('forget', installation, () => api.forgetInstallation(installation.id))

  return (
    <Page
      title={app?.name ?? 'App'}
      back="/apps"
      action={
        <Link to={`/apps/${appId}/edit`}>
          <button className="ghost">Edit</button>
        </Link>
      }
    >
      <Loading error={error} offline={offline} hasData={!!app} />
      {actionError && <Banner tone="bad">{actionError}</Banner>}

      {app && (
        <>
          <Card>
            {app.description && <p style={{ marginTop: 0 }}>{app.description}</p>}
            <div className="meter-label">
              <span>Install command</span>
            </div>
            <pre className="mono" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0 }}>
              {app.installCommand}
            </pre>
            <div className="actions">
              <button className="primary" onClick={() => setDeployTarget('new')}>
                Deploy
              </button>
              {/* Where the app is running somewhere, opening it is the thing
                  most often wanted from this page. */}
              <LaunchButton installations={mine} className="secondary" label="Open app" />
            </div>
          </Card>

          <SectionTitle>Deployed on</SectionTitle>
          {mine.length === 0 ? (
            <Empty message="Not deployed anywhere yet." />
          ) : (
            mine.map((installation) => (
              <Card key={installation.id}>
                <div className="row between">
                  <div className="grow">
                    <div className="title">{installation.hostName}</div>
                    <div className="sub">
                      {parts(
                        installation.hostAddress,
                        ports(installation.ports),
                        `updated ${ago(installation.updatedAt)}`,
                      )}
                    </div>
                  </div>
                  <div className="row" style={{ gap: 6 }}>
                    <LaunchButton installations={[installation]} />
                    <HealthBadge status={installation.healthStatus} />
                  </div>
                </div>
                {/* What the last check found, and when it ran. The time is
                    what a Check that changes nothing has to show for itself —
                    without it, confirming a healthy app still looks like a
                    button that did nothing. */}
                {(installation.healthDetail || installation.healthCheckedAt) && (
                  <div className="sub" style={{ marginTop: 8 }}>
                    {parts(
                      installation.healthDetail,
                      installation.healthCheckedAt &&
                        `checked ${ago(installation.healthCheckedAt)}`,
                    )}
                  </div>
                )}
                <div className="actions">
                  <button
                    className="primary"
                    onClick={() => setDeployTarget(installation)}
                    disabled={!!pending}
                  >
                    Redeploy
                  </button>
                  <button
                    className="secondary"
                    onClick={() => check(installation)}
                    disabled={!!pending}
                  >
                    {running('check', installation) ? 'Checking…' : 'Check'}
                  </button>
                  <button
                    className="secondary"
                    onClick={() => forget(installation)}
                    disabled={!!pending}
                  >
                    {running('forget', installation) ? 'Forgetting…' : 'Forget'}
                  </button>
                </div>
              </Card>
            ))
          )}

          <SectionTitle>History</SectionTitle>
          {history?.length === 0 ? (
            <Empty message="No deployments yet." />
          ) : (
            history?.map((deployment) => (
              <Link key={deployment.id} className="card" to={`/deployments/${deployment.id}`}>
                <div className="row between">
                  <div className="grow">
                    <div className="title">{deployment.hostName}</div>
                    <div className="sub">{time(deployment.startedAt)}</div>
                  </div>
                  <DeploymentBadge status={deployment.status} />
                </div>
              </Link>
            ))
          )}

          <SectionTitle>Danger zone</SectionTitle>
          <Card>
            <p className="sub" style={{ marginTop: 0 }}>
              Deleting an app removes its deployment history from Deployer. Anything already
              installed on a host keeps running.
            </p>
            <button className="danger block" onClick={() => setConfirmDelete(true)}>
              Delete app
            </button>
          </Card>
        </>
      )}

      {app && deployTarget && (
        <DeploySheet
          app={app}
          hosts={hosts ?? []}
          installation={deployTarget === 'new' ? null : deployTarget}
          onClose={() => setDeployTarget(null)}
          onStarted={(deploymentId) => {
            reloadHistory()
            navigate(`/deployments/${deploymentId}`)
          }}
        />
      )}

      {confirmDelete && (
        <Sheet
          title={`Delete ${app?.name}?`}
          subtitle="This removes the app and its history from Deployer only."
          onClose={() => setConfirmDelete(false)}
        >
          <div className="actions">
            <button className="secondary" onClick={() => setConfirmDelete(false)}>
              Cancel
            </button>
            <button className="danger" onClick={removeApp}>
              Delete
            </button>
          </div>
        </Sheet>
      )}
    </Page>
  )
}

/**
 * DeploySheet is the confirmation step: pick the host, review the parameters,
 * see exactly what will run, then deploy. Redeploys land here prefilled, which
 * is what makes them one tap plus a confirm.
 */
function DeploySheet({
  app,
  hosts,
  installation,
  onClose,
  onStarted,
}: {
  app: App
  hosts: Host[]
  installation: Installation | null
  onClose: () => void
  onStarted: (deploymentId: number) => void
}) {
  const [hostId, setHostId] = useState<number>(installation?.hostId ?? hosts[0]?.id ?? 0)
  const [values, setValues] = useState<Record<string, string>>(() => {
    const initial: Record<string, string> = {}
    for (const param of app.params) {
      initial[param.name] = installation?.params[param.name] ?? param.default
    }
    return initial
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const host = hosts.find((h) => h.id === hostId)
  const preview = useMemo(() => {
    const substitutions: Record<string, string> = { ...values }
    if (host) {
      substitutions.host = host.address
      substitutions.hostname = host.name
      substitutions.user = host.username
    }
    return app.installCommand.replace(/\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g, (match, name) =>
      name in substitutions ? shellQuote(substitutions[name]) : match,
    )
  }, [app.installCommand, values, host])

  const start = async () => {
    setBusy(true)
    setError(null)
    try {
      const deployment = installation
        ? await api.redeploy(installation.id, values)
        : await api.deploy(app.id, hostId, values)
      onStarted(deployment.id)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  return (
    <Sheet
      title={installation ? `Redeploy ${app.name}` : `Deploy ${app.name}`}
      subtitle={installation ? `to ${installation.hostName}` : undefined}
      onClose={onClose}
    >
      {error && <Banner tone="bad">{error}</Banner>}

      {!installation && (
        <Field label="Host">
          {hosts.length === 0 ? (
            <p className="sub">Add a host first.</p>
          ) : (
            <select value={hostId} onChange={(e) => setHostId(Number(e.target.value))}>
              {hosts.map((h) => (
                <option key={h.id} value={h.id}>
                  {h.name} ({h.address}){h.status !== 'online' ? ' — offline' : ''}
                </option>
              ))}
            </select>
          )}
        </Field>
      )}

      {host && host.status !== 'online' && (
        <Banner tone="warn">{host.name} is not reachable right now. The deploy will likely fail.</Banner>
      )}
      {host && host.status === 'online' && !host.sudoOk && (
        <Banner tone="warn">
          {host.username} doesn't have passwordless sudo on {host.name}. Commands that need root will
          fail.
        </Banner>
      )}

      {app.params.map((param) => (
        <Field key={param.name} label={param.label || param.name} help={param.help}>
          <input
            value={values[param.name] ?? ''}
            onChange={(e) => setValues((current) => ({ ...current, [param.name]: e.target.value }))}
            placeholder={param.default}
            autoCapitalize="none"
            autoCorrect="off"
          />
        </Field>
      ))}

      <div className="meter-label">
        <span>This will run on the host</span>
      </div>
      <pre className="mono" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
        {preview}
      </pre>

      <div className="actions">
        <button className="secondary" onClick={onClose} disabled={busy}>
          Cancel
        </button>
        <button className="primary" onClick={start} disabled={busy || !hostId}>
          {busy ? 'Starting…' : installation ? 'Redeploy' : 'Deploy'}
        </button>
      </div>
    </Sheet>
  )
}

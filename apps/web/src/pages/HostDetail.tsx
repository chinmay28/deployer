import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { Page } from '../components/Layout'
import { SetupSheet } from '../components/provision'
import { DeploymentBadge, HomeBadge, HostBadge } from '../components/status'
import { Badge, Banner, Card, Loading, Meter, SectionTitle, Sheet, Sparkline, useLoader } from '../components/ui'
import { ago, bytes, percent, time, uptime } from '../lib/format'
import type { HostTestResult } from '../types'

export default function HostDetail() {
  const { id } = useParams()
  const hostId = Number(id)
  const navigate = useNavigate()

  // Asking for metrics also tells the server someone is watching, which raises
  // the sampling rate for this host.
  const { data: host, error, offline, reload } = useLoader(() => api.host(hostId), [hostId], 5000)
  const { data: metrics } = useLoader(() => api.hostMetrics(hostId, 60), [hostId], 5000)
  const { data: deployments } = useLoader(() => api.deployments({ hostId, limit: 8 }), [hostId], 10000)

  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<HostTestResult | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [settingUp, setSettingUp] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const test = async () => {
    setTesting(true)
    setResult(null)
    try {
      setResult(await api.testHost(hostId))
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e))
    } finally {
      setTesting(false)
    }
  }

  const remove = async () => {
    try {
      await api.deleteHost(hostId)
      navigate('/hosts', { replace: true })
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e))
      setConfirmDelete(false)
    }
  }

  const sample = host?.latest
  const cpuHistory = metrics?.samples.map((s) => s.cpuPct) ?? []

  return (
    <Page
      title={host?.name ?? 'Host'}
      back="/hosts"
      action={
        <Link to={`/hosts/${hostId}/edit`}>
          <button className="ghost">Edit</button>
        </Link>
      }
    >
      <Loading error={error} offline={offline} hasData={!!host} />
      {actionError && <Banner tone="bad">{actionError}</Banner>}

      {host && (
        <>
          <Card>
            <div className="row between">
              <div className="grow">
                <div className="title">
                  {host.username}@{host.address}
                  {host.port !== 22 ? `:${host.port}` : ''}
                </div>
                <div className="sub">
                  {host.os || 'Not yet identified'} · seen {ago(host.lastSeenAt)}
                </div>
              </div>
              <div className="row" style={{ gap: 6 }}>
                {host.isSelf && <HomeBadge />}
                <HostBadge status={host.status} />
              </div>
            </div>

            {host.isSelf && (
              <div className="sub" style={{ marginTop: 10 }}>
                Deployer itself runs on this machine. It still connects over SSH, so it needs its
                own key authorized here like any other host.
              </div>
            )}

            {host.status !== 'online' && host.lastError && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="bad">{host.lastError}</Banner>
              </div>
            )}
            {host.status === 'online' && !host.sudoOk && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="warn">
                  Passwordless sudo isn't set up for {host.username}. Deployments that need root will
                  fail — set up access with the host's password, or run the command in Settings.
                </Banner>
              </div>
            )}

            <div className="actions">
              <button className="secondary" onClick={test} disabled={testing}>
                {testing ? 'Testing…' : 'Test connection'}
              </button>
              {/* A working host needs nothing, so setup only fronts up when
                  something is actually missing. */}
              {(host.status !== 'online' || !host.sudoOk) && (
                <button className="primary" onClick={() => setSettingUp(true)}>
                  Set up access
                </button>
              )}
            </div>
          </Card>

          {sample && (
            <Card>
              <div className="meters">
                <Meter label="CPU" value={sample.cpuPct} display={`${Math.round(sample.cpuPct)}%`} />
                {/* Just the used figure: three columns on a phone have no room
                    for "902 MB / 15.7 GB" without wrapping. */}
                <Meter
                  label="Memory"
                  used={sample.memUsed}
                  total={sample.memTotal}
                  display={bytes(sample.memUsed)}
                />
                <Meter
                  label="Load"
                  value={Math.min(100, sample.load1 * 25)}
                  display={sample.load1.toFixed(2)}
                />
              </div>
              <div className="sub" style={{ marginTop: 8 }}>
                {bytes(sample.memUsed)} of {bytes(sample.memTotal)} memory used
              </div>

              <div className="list-divider" />
              <div className="meter-label">
                <span>CPU, last hour</span>
                <b>{metrics ? `${metrics.samples.length} samples` : ''}</b>
              </div>
              <Sparkline values={cpuHistory} />

              <div className="list-divider" />
              {sample.disks.map((disk) => (
                <div key={disk.mount} style={{ marginBottom: 10 }}>
                  <Meter
                    label={disk.mount}
                    used={disk.usedBytes}
                    total={disk.totalBytes}
                    display={`${bytes(disk.usedBytes)} / ${bytes(disk.totalBytes)}`}
                  />
                </div>
              ))}
              {sample.swapTotal > 0 && (
                <Meter
                  label="Swap"
                  used={sample.swapUsed}
                  total={sample.swapTotal}
                  display={`${Math.round(percent(sample.swapUsed, sample.swapTotal))}%`}
                />
              )}

              <div className="list-divider" />
              <div className="row between sub">
                <span>Uptime {uptime(sample.uptimeS)}</span>
                {sample.tempC != null && <span>{sample.tempC.toFixed(1)} °C</span>}
              </div>
            </Card>
          )}

          <SectionTitle>Details</SectionTitle>
          <Card>
            <Detail label="Hostname" value={host.hostname || '—'} />
            <Detail label="Kernel" value={host.kernel || '—'} />
            <Detail label="Architecture" value={host.arch || '—'} />
            <Detail label="Passwordless sudo" value={host.sudoOk ? 'Yes' : 'No'} />
          </Card>

          {deployments && deployments.length > 0 && (
            <>
              <SectionTitle>Deployments here</SectionTitle>
              {deployments.map((deployment) => (
                <Link key={deployment.id} className="card" to={`/deployments/${deployment.id}`}>
                  <div className="row between">
                    <div className="grow">
                      <div className="title">{deployment.appName}</div>
                      <div className="sub">{time(deployment.startedAt)}</div>
                    </div>
                    <DeploymentBadge status={deployment.status} />
                  </div>
                </Link>
              ))}
            </>
          )}

          <SectionTitle>Danger zone</SectionTitle>
          <Card>
            <p className="sub" style={{ marginTop: 0 }}>
              Removing a host deletes its monitoring history from Deployer. Nothing on the machine
              itself is changed.
              {host.isSelf && ' Removing this one also turns off updating Deployer from the UI.'}
            </p>
            <button className="danger block" onClick={() => setConfirmDelete(true)}>
              Remove host
            </button>
          </Card>
        </>
      )}

      {result && (
        <Sheet title={result.ok ? 'Connection works' : 'Could not connect'} onClose={() => setResult(null)}>
          {result.ok ? (
            <>
              <Banner tone="good">
                Connected to {result.hostname} ({result.os})
              </Banner>
              <div className="row" style={{ marginBottom: 12 }}>
                <span className="sub grow">Passwordless sudo</span>
                {result.sudoOk ? <Badge tone="good">Working</Badge> : <Badge tone="warn">Not set up</Badge>}
              </div>
            </>
          ) : (
            <Banner tone="bad">{result.error}</Banner>
          )}
          {result.hints?.map((hint) => (
            <p key={hint} className="sub">
              {hint}
            </p>
          ))}
          {!result.ok || !result.sudoOk ? (
            <div className="actions">
              <button className="secondary" onClick={() => setResult(null)}>
                Done
              </button>
              <button
                className="primary"
                onClick={() => {
                  setResult(null)
                  setSettingUp(true)
                }}
              >
                Set up access
              </button>
            </div>
          ) : (
            <button className="primary block" onClick={() => setResult(null)}>
              Done
            </button>
          )}
        </Sheet>
      )}

      {settingUp && host && (
        <SetupSheet
          hostId={hostId}
          username={host.username}
          address={host.address}
          port={host.port}
          onClose={() => setSettingUp(false)}
          onFinished={reload}
        />
      )}

      {confirmDelete && (
        <Sheet
          title={`Remove ${host?.name}?`}
          subtitle="Deployer will forget this host and its monitoring history. The machine is left alone."
          onClose={() => setConfirmDelete(false)}
        >
          <div className="actions">
            <button className="secondary" onClick={() => setConfirmDelete(false)}>
              Cancel
            </button>
            <button className="danger" onClick={remove}>
              Remove
            </button>
          </div>
        </Sheet>
      )}
    </Page>
  )
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="row between" style={{ padding: '5px 0' }}>
      <span className="sub">{label}</span>
      <span style={{ fontSize: 14 }}>{value}</span>
    </div>
  )
}

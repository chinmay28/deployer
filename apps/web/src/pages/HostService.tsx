import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { Page } from '../components/Layout'
import { BootBadge, ServiceBadge } from '../components/status'
import { Banner, Card, Loading, SectionTitle, Sheet, useLoader } from '../components/ui'
import { bytes, serviceName, uptime } from '../lib/format'
import type { ServiceAction, ServiceUnit } from '../types'

/** How much journal to fetch. The first is what the page opens on; the rest are
 *  a tap away, because the answer is usually in the last twenty lines and
 *  occasionally a thousand back. */
const LOG_LINES = [50, 200, 1000]

/** Stopping something is worth a second tap; starting it is not. Disabling is
 *  the one that bites later — a service that no longer comes back after a
 *  reboot fails silently, months from now. */
const CONFIRM: Partial<Record<ServiceAction, { title: string; body: string; verb: string }>> = {
  stop: {
    title: 'Stop this service?',
    body: 'It stays stopped until something starts it again — a reboot, if it is enabled, or you.',
    verb: 'Stop',
  },
  disable: {
    title: 'Stop starting this at boot?',
    body: 'It keeps running now, and will not come back on its own the next time the machine restarts.',
    verb: 'Disable',
  },
}

/**
 * One systemd service: what it is doing, the buttons that change that, the tail
 * of its journal, and its unit file.
 *
 * Saving the unit file runs `systemctl daemon-reload` straight after, because a
 * unit file edited and not reloaded is a change that has not happened — the
 * single most common way an edit from a phone appears to do nothing.
 */
export default function HostService() {
  const { id } = useParams()
  const hostId = Number(id)
  const [params] = useSearchParams()
  const name = params.get('name') ?? ''

  const { data: host } = useLoader(() => api.host(hostId), [hostId])
  const {
    data: unit,
    error,
    offline,
    reload,
  } = useLoader(() => api.service(hostId, name), [hostId, name])

  const [busy, setBusy] = useState<ServiceAction | null>(null)
  const [confirm, setConfirm] = useState<ServiceAction | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [failure, setFailure] = useState<string | null>(null)

  const [lines, setLines] = useState(LOG_LINES[0])
  const {
    data: log,
    error: logError,
    loading: logLoading,
    reload: reloadLog,
  } = useLoader(() => api.serviceLog(hostId, name, lines), [hostId, name, lines])

  const short = serviceName(name)

  // systemctl waits for the service, and Deployer waits for systemctl: a unit
  // that takes half a minute to come up takes half a minute to answer.
  const run = async (action: ServiceAction) => {
    setBusy(action)
    setConfirm(null)
    setNotice(null)
    setFailure(null)
    try {
      await api.serviceAction(hostId, name, action)
      setNotice(`${DONE[action]} ${short}.`)
      reload()
      reloadLog()
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const ask = (action: ServiceAction) => (CONFIRM[action] ? setConfirm(action) : run(action))

  const active = unit?.active === 'active' || unit?.active === 'reloading'
  const usable = !!unit && !unit.template && unit.load !== 'not-found' && unit.load !== 'masked'
  const working = busy !== null

  return (
    <Page
      title={short || 'Service'}
      back={`/hosts/${hostId}/services`}
      action={
        <button className="ghost" onClick={reload} disabled={working}>
          Refresh
        </button>
      }
    >
      <Loading error={error} offline={offline} hasData={!!unit} />
      {failure && <Banner tone="bad">{failure}</Banner>}
      {notice && <Banner tone="good">{notice}</Banner>}

      {!unit && !error && (
        <Card>
          <div className="sub">Asking {host?.name ?? 'the host'} about {name}…</div>
        </Card>
      )}

      {unit && (
        <>
          <Card>
            <div className="row between">
              <div className="grow">
                <div className="title">{unit.name}</div>
                <div className="sub">{unit.description || 'No description in the unit file'}</div>
              </div>
              <ServiceBadge unit={unit} />
            </div>

            <div className="row" style={{ gap: 6, marginTop: 10, flexWrap: 'wrap' }}>
              <BootBadge unit={unit} />
            </div>

            <div className="stats">
              <Stat
                label={active ? 'Running for' : 'Stopped for'}
                value={unit.sinceS > 0 ? uptime(unit.sinceS) : '—'}
              />
              <Stat label="Memory" value={unit.memory > 0 ? bytes(unit.memory) : '—'} />
              <Stat
                label={unit.restarts > 0 ? 'Restarts' : 'Main PID'}
                value={
                  unit.restarts > 0 ? String(unit.restarts) : unit.mainPid > 0 ? String(unit.mainPid) : '—'
                }
              />
            </div>

            {unit.active === 'failed' && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="bad">
                  {short} failed{unit.result && unit.result !== 'success' ? ` (${unit.result})` : ''}.
                  The last lines of its journal are below.
                </Banner>
              </div>
            )}
            {unit.load === 'not-found' && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="bad">
                  systemd has no unit file for {name}. It may have been deleted since this list was
                  read — reload the unit files from the services screen.
                </Banner>
              </div>
            )}
            {unit.load === 'masked' && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="warn">
                  This unit is masked: systemd refuses to start it at all. Deleting the link at{' '}
                  <span className="mono">/etc/systemd/system/{name}</span> in the file browser
                  unmasks it.
                </Banner>
              </div>
            )}
            {unit.template && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="warn">
                  This is a template. Nothing starts it directly — systemd makes a service out of it
                  per instance, named {short.replace(/@$/, '')}@something.service.
                </Banner>
              </div>
            )}
            {host && host.status === 'online' && !host.sudoOk && (
              <div style={{ marginTop: 12 }}>
                <Banner tone="warn">
                  {host.username} doesn't have passwordless sudo here, so systemd will refuse these
                  buttons. Set up access from the host's page first.
                </Banner>
              </div>
            )}
          </Card>

          {usable && (
            <>
              <SectionTitle>Manage</SectionTitle>
              <Card>
                <div className="actions" style={{ marginTop: 0 }}>
                  <button
                    className="primary"
                    onClick={() => ask('start')}
                    disabled={working || active}
                  >
                    {busy === 'start' ? 'Starting…' : 'Start'}
                  </button>
                  <button className="secondary" onClick={() => ask('restart')} disabled={working}>
                    {busy === 'restart' ? 'Restarting…' : 'Restart'}
                  </button>
                  <button className="danger" onClick={() => ask('stop')} disabled={working || !active}>
                    {busy === 'stop' ? 'Stopping…' : 'Stop'}
                  </button>
                </div>
                <p className="sub" style={{ marginBottom: 0 }}>
                  Deployer waits for systemd to finish, so a service that takes its time starting
                  takes its time answering.
                </p>
                <button
                  className="secondary block"
                  style={{ marginTop: 10 }}
                  onClick={() => ask('reload')}
                  disabled={working}
                >
                  {busy === 'reload' ? 'Reloading…' : 'Reload its configuration'}
                </button>
                <p className="sub" style={{ marginBottom: 0, marginTop: 6 }}>
                  Only services written to handle it can reload without stopping; systemd says so if
                  this one can't.
                </p>
              </Card>

              <Card>
                <div className="title">At boot</div>
                <p className="sub" style={{ marginTop: 4 }}>
                  {unit.fileState === 'enabled'
                    ? `${short} starts by itself when the machine does.`
                    : unit.fileState === 'static'
                      ? 'This unit is started by another one rather than on its own, so there is nothing to enable.'
                      : `${short} only runs when something starts it. It will not come back after a reboot.`}
                </p>
                {unit.fileState !== 'static' && (
                  <button
                    className="secondary block"
                    onClick={() => ask(unit.fileState === 'enabled' ? 'disable' : 'enable')}
                    disabled={working}
                  >
                    {busy === 'enable' || busy === 'disable'
                      ? 'Working…'
                      : unit.fileState === 'enabled'
                        ? 'Stop starting at boot'
                        : 'Start at boot'}
                  </button>
                )}
              </Card>
            </>
          )}

          <SectionTitle>Log</SectionTitle>
          <Card>
            <div className="row between">
              <div className="chips" style={{ margin: 0 }}>
                {LOG_LINES.map((count) => (
                  <button
                    key={count}
                    className={`chip ${lines === count ? 'on' : ''}`}
                    aria-pressed={lines === count}
                    onClick={() => setLines(count)}
                  >
                    {count}
                  </button>
                ))}
              </div>
              <button className="ghost" onClick={reloadLog} disabled={logLoading}>
                Refresh
              </button>
            </div>

            {logError && <Banner tone="bad">{logError}</Banner>}
            {log?.truncated && (
              <Banner tone="warn">
                Only the end of this log fits. The oldest of the {log.lines} lines were dropped.
              </Banner>
            )}
            <pre className="log" style={{ marginTop: 10 }}>
              {log ? log.content.trimEnd() || 'Nothing in the journal for this service.' : 'Reading the journal…'}
            </pre>
          </Card>

          <UnitFile hostId={hostId} unit={unit} onChanged={reload} />
        </>
      )}

      {confirm && CONFIRM[confirm] && (
        <Sheet
          title={CONFIRM[confirm]!.title}
          subtitle={CONFIRM[confirm]!.body}
          onClose={() => setConfirm(null)}
        >
          <div className="actions">
            <button className="secondary" onClick={() => setConfirm(null)} disabled={working}>
              Cancel
            </button>
            <button className="danger" onClick={() => run(confirm)} disabled={working}>
              {CONFIRM[confirm]!.verb}
            </button>
          </div>
        </Sheet>
      )}
    </Page>
  )
}

/** DONE is what to say afterwards, in the past tense the button was in. */
const DONE: Record<ServiceAction, string> = {
  start: 'Started',
  stop: 'Stopped',
  restart: 'Restarted',
  reload: 'Reloaded',
  enable: 'Enabled',
  disable: 'Disabled',
}

/**
 * The unit file, read and written through the same endpoints the file browser
 * uses — an atomic write that keeps the file's mode and owner. What is added
 * here is the daemon-reload afterwards, without which the edit is a change to a
 * file and not to a service.
 */
function UnitFile({
  hostId,
  unit,
  onChanged,
}: {
  hostId: number
  unit: ServiceUnit
  onChanged: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState<string | null>(null)
  const [failure, setFailure] = useState<string | null>(null)

  const path = unit.path
  // Nothing is fetched until the editor is opened: a unit file is one more SSH
  // session, and most visits to this screen are to read the log.
  const { data: file, reload: reloadFile } = useLoader(
    () => (editing && path ? api.file(hostId, path) : Promise.resolve(null)),
    [hostId, path, editing],
  )

  const content = draft ?? file?.content ?? ''
  const dirty = draft !== null && draft !== (file?.content ?? '')

  const save = async () => {
    setSaving(true)
    setFailure(null)
    setSaved(null)
    try {
      await api.saveFile(hostId, path, draft ?? '')
      // Two calls on purpose: the write is the file browser's, the reload is
      // systemd's, and either can fail on its own terms.
      await api.reloadServices(hostId)
      setDraft(null)
      setSaved(`Saved, and systemd re-read it. Restart ${serviceName(unit.name)} to run the new one.`)
      reloadFile()
      onChanged()
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!path) {
    return (
      <>
        <SectionTitle>Unit file</SectionTitle>
        <Card>
          <p className="sub" style={{ margin: 0 }}>
            systemd did not report a unit file for this service, so there is nothing to edit.
          </p>
        </Card>
      </>
    )
  }

  return (
    <>
      <SectionTitle>Unit file</SectionTitle>
      <Card>
        <div className="row between">
          <div className="grow">
            <div className="mono truncate">{path}</div>
            <div className="sub">
              {file ? `${file.mode} · ${file.owner}:${file.group}` : 'systemd read this file'}
            </div>
          </div>
          <button className="ghost" onClick={() => setEditing((open) => !open)}>
            {editing ? 'Close' : 'Edit'}
          </button>
        </div>

        {failure && (
          <div style={{ marginTop: 10 }}>
            <Banner tone="bad">{failure}</Banner>
          </div>
        )}
        {saved && (
          <div style={{ marginTop: 10 }}>
            <Banner tone="good">{saved}</Banner>
          </div>
        )}

        {editing && (
          <>
            {!file ? (
              <div className="sub" style={{ marginTop: 10 }}>
                Reading {path}…
              </div>
            ) : (
              <>
                <textarea
                  className="editor"
                  style={{ marginTop: 10 }}
                  value={content}
                  onChange={(e) => setDraft(e.target.value)}
                  spellCheck={false}
                  autoCapitalize="off"
                  autoCorrect="off"
                  autoComplete="off"
                  aria-label={`${unit.name} unit file`}
                />
                <p className="sub">
                  Saving writes the file and runs <span className="mono">daemon-reload</span>. The
                  service keeps running the old settings until it is restarted.
                </p>
                <div className="actions">
                  <button className="secondary" onClick={() => setDraft(null)} disabled={!dirty || saving}>
                    Discard
                  </button>
                  <button className="primary" onClick={save} disabled={!dirty || saving}>
                    {saving ? 'Saving…' : 'Save and reload'}
                  </button>
                </div>
              </>
            )}
          </>
        )}

        <div className="list-divider" />
        <Link className="sub" to={`/hosts/${hostId}/file?path=${encodeURIComponent(path)}`}>
          Open in the file browser ›
        </Link>
      </Card>
    </>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v">{value}</div>
    </div>
  )
}

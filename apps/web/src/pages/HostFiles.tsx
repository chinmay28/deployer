import { useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../api'
import { Page } from '../components/Layout'
import { Badge, Banner, Card, Empty, Field, Loading, Sheet, useLoader } from '../components/ui'
import { ago, bytes } from '../lib/format'
import type { DirEntry } from '../types'

/**
 * The file browser. One directory at a time, the path in the query string so
 * the back button walks back up the way you came and a link to a directory is
 * a link someone can keep.
 *
 * Deployer already holds a key that can run anything on this machine, so the
 * listing is taken as root wherever sudo allows it — a browser that could not
 * open /etc would be hiding the reality rather than limiting it. Which account
 * it acted as is shown rather than assumed.
 */
export default function HostFiles() {
  const { id } = useParams()
  const hostId = Number(id)
  const [params, setParams] = useSearchParams()
  const path = params.get('path') ?? ''
  const navigate = useNavigate()

  const {
    data: listing,
    error,
    offline,
    reload,
  } = useLoader(() => api.files(hostId, path), [hostId, path])
  const [acting, setActing] = useState<DirEntry | null>(null)
  const [creating, setCreating] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)

  const go = (to: string) => setParams({ path: to })

  const open = (entry: DirEntry) => {
    const target = join(listing?.path ?? '/', entry.name)
    if (isDirectory(entry)) {
      go(target)
      return
    }
    if (entry.linkType === 'broken') {
      setNotice(`${entry.name} points at ${entry.target}, which isn't there.`)
      return
    }
    navigate(`/hosts/${hostId}/file?path=${encodeURIComponent(target)}`)
  }

  // Back walks up the tree, which is what a file browser's back means. At the
  // top there is nowhere further up, so it returns to the host.
  const up =
    listing && listing.path !== '/'
      ? `/hosts/${hostId}/files?path=${encodeURIComponent(listing.parent)}`
      : `/hosts/${hostId}`

  return (
    <Page
      title={listing ? name(listing.path) : 'Files'}
      back={up}
      action={
        <button className="ghost" onClick={() => setCreating(true)}>
          New
        </button>
      }
    >
      <Loading error={error} offline={offline} hasData={!!listing} />
      {notice && <Banner tone="warn">{notice}</Banner>}

      {listing && (
        <>
          <Card>
            <Crumbs path={listing.path} onGo={go} />
            <div className="row between sub" style={{ marginTop: 10 }}>
              <span>
                {listing.entries.length} item{listing.entries.length === 1 ? '' : 's'}
              </span>
              {/* Root is the difference between reading a config and editing
                  it, so it is stated rather than left to be discovered. */}
              <Badge tone={listing.asUser === 'root' ? 'accent' : 'neutral'}>
                as {listing.asUser || 'unknown'}
              </Badge>
            </div>
          </Card>

          {listing.truncated && (
            <Banner tone="warn">
              This directory has more in it than Deployer will list. Only the first{' '}
              {listing.entries.length} are shown.
            </Banner>
          )}

          {listing.entries.length === 0 ? (
            <Empty message="Nothing in here." />
          ) : (
            listing.entries.map((entry) => (
              <div className="card file-row" key={entry.name}>
                <button className="file-open" onClick={() => open(entry)}>
                  <FileIcon entry={entry} />
                  <span className="grow">
                    <span className="title">{entry.name}</span>
                    <span className="sub truncate">{describe(entry)}</span>
                  </span>
                  {isDirectory(entry) && <span className="chevron">›</span>}
                </button>
                <button
                  className="file-more"
                  onClick={() => setActing(entry)}
                  aria-label={`Actions for ${entry.name}`}
                >
                  ⋯
                </button>
              </div>
            ))
          )}
        </>
      )}

      {acting && listing && (
        <EntrySheet
          hostId={hostId}
          dir={listing.path}
          entry={acting}
          onClose={() => setActing(null)}
          onDone={(message) => {
            setActing(null)
            setNotice(message)
            reload()
          }}
        />
      )}

      {creating && listing && (
        <CreateSheet
          hostId={hostId}
          dir={listing.path}
          onClose={() => setCreating(false)}
          onFolder={() => {
            setCreating(false)
            reload()
          }}
          onFile={(target) =>
            navigate(`/hosts/${hostId}/file?path=${encodeURIComponent(target)}&new=1`)
          }
        />
      )}
    </Page>
  )
}

/** Crumbs turn the path into taps: every ancestor is one, root included. */
function Crumbs({ path, onGo }: { path: string; onGo: (to: string) => void }) {
  const parts = path.split('/').filter(Boolean)
  return (
    <div className="crumbs">
      <button className="crumb" onClick={() => onGo('/')}>
        /
      </button>
      {parts.map((part, i) => (
        <button
          key={i}
          className="crumb"
          onClick={() => onGo('/' + parts.slice(0, i + 1).join('/'))}
        >
          {part}
        </button>
      ))}
    </div>
  )
}

function FileIcon({ entry }: { entry: DirEntry }) {
  const stroke = {
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.6,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
  }
  return (
    <svg className={`file-icon ${isDirectory(entry) ? 'dir' : ''}`} viewBox="0 0 24 24" {...stroke}>
      {isDirectory(entry) ? (
        <path d="M3 7.5a2 2 0 0 1 2-2h3.6l1.8 2H19a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      ) : (
        <>
          <path d="M6 3.5h7.5L18 8v12.5H6z" />
          <path d="M13.5 3.5V8H18" />
        </>
      )}
      {entry.type === 'link' && <path d="M9.5 14.5l5-5" />}
    </svg>
  )
}

/** describe is the one line under a name: what it is, how big, who owns it. */
function describe(entry: DirEntry): string {
  const parts: string[] = []
  if (entry.type === 'link') {
    parts.push(entry.linkType === 'broken' ? '↳ broken link' : `↳ ${entry.target}`)
  } else if (entry.type === 'dir') {
    parts.push('folder')
  } else if (entry.type === 'other') {
    parts.push('special file')
  } else {
    parts.push(bytes(entry.size))
  }
  if (entry.mode) parts.push(entry.mode)
  if (entry.owner) parts.push(entry.owner)
  if (entry.modifiedAt) parts.push(ago(entry.modifiedAt))
  return parts.join(' · ')
}

/** EntrySheet is rename and delete, the two things worth doing from a phone. */
function EntrySheet({
  hostId,
  dir,
  entry,
  onClose,
  onDone,
}: {
  hostId: number
  dir: string
  entry: DirEntry
  onClose: () => void
  onDone: (message: string) => void
}) {
  const path = join(dir, entry.name)
  const [renaming, setRenaming] = useState(false)
  const [newName, setNewName] = useState(entry.name)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  // A directory with something in it is refused once, and only deleted when
  // the answer to "everything inside as well?" is yes.
  const [needsRecursive, setNeedsRecursive] = useState(false)

  const rename = async () => {
    setBusy(true)
    try {
      await api.renameFile(hostId, path, join(dir, newName.trim()))
      onDone(`Renamed to ${newName.trim()}.`)
    } catch (e) {
      setFailure(message(e))
    } finally {
      setBusy(false)
    }
  }

  const remove = async (recursive: boolean) => {
    setBusy(true)
    try {
      await api.deleteFile(hostId, path, recursive)
      onDone(`Deleted ${entry.name}.`)
    } catch (e) {
      // 409 is the host saying the directory is not empty, which is a question
      // rather than a failure.
      if (e instanceof ApiError && e.status === 409) {
        setNeedsRecursive(true)
        setFailure(null)
      } else {
        setFailure(message(e))
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Sheet title={entry.name} subtitle={path} onClose={onClose}>
      {failure && <Banner tone="bad">{failure}</Banner>}

      {renaming ? (
        <>
          <Field label="New name">
            <input value={newName} onChange={(e) => setNewName(e.target.value)} autoFocus />
          </Field>
          <div className="actions">
            <button className="secondary" onClick={() => setRenaming(false)}>
              Cancel
            </button>
            <button
              className="primary"
              onClick={rename}
              disabled={busy || !newName.trim() || newName.trim() === entry.name}
            >
              {busy ? 'Renaming…' : 'Rename'}
            </button>
          </div>
        </>
      ) : needsRecursive ? (
        <>
          <Banner tone="warn">
            {entry.name} is not empty. Deleting it takes everything inside with it.
          </Banner>
          <div className="actions">
            <button className="secondary" onClick={onClose}>
              Keep it
            </button>
            <button className="danger" onClick={() => remove(true)} disabled={busy}>
              {busy ? 'Deleting…' : 'Delete everything'}
            </button>
          </div>
        </>
      ) : confirming ? (
        <>
          <p className="sub">This deletes it on the host. There is no undo.</p>
          <div className="actions">
            <button className="secondary" onClick={() => setConfirming(false)}>
              Cancel
            </button>
            <button className="danger" onClick={() => remove(false)} disabled={busy}>
              {busy ? 'Deleting…' : 'Delete'}
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="sub" style={{ marginBottom: 12 }}>
            {entry.mode && `mode ${entry.mode} · `}
            {entry.owner}
            {entry.group ? `:${entry.group}` : ''}
            {entry.type === 'file' ? ` · ${bytes(entry.size)}` : ''}
          </div>
          <button className="secondary block" onClick={() => setRenaming(true)}>
            Rename
          </button>
          <div style={{ height: 10 }} />
          <button className="danger block" onClick={() => setConfirming(true)}>
            Delete
          </button>
        </>
      )}
    </Sheet>
  )
}

/** CreateSheet makes an empty file or a folder here. A new file opens straight
 *  into the editor rather than being written empty and found later. */
function CreateSheet({
  hostId,
  dir,
  onClose,
  onFolder,
  onFile,
}: {
  hostId: number
  dir: string
  onClose: () => void
  onFolder: () => void
  onFile: (path: string) => void
}) {
  const [kind, setKind] = useState<'file' | 'folder'>('file')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)

  const create = async () => {
    const target = join(dir, name.trim())
    if (kind === 'file') {
      onFile(target)
      return
    }
    setBusy(true)
    try {
      await api.mkdir(hostId, target)
      onFolder()
    } catch (e) {
      setFailure(message(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Sheet title="New" subtitle={`in ${dir}`} onClose={onClose}>
      {failure && <Banner tone="bad">{failure}</Banner>}
      <div className="segmented">
        <button className={kind === 'file' ? 'on' : ''} onClick={() => setKind('file')}>
          File
        </button>
        <button className={kind === 'folder' ? 'on' : ''} onClick={() => setKind('folder')}>
          Folder
        </button>
      </div>
      <Field label="Name">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={kind === 'file' ? 'notes.conf' : 'backups'}
          autoFocus
        />
      </Field>
      <div className="actions">
        <button className="secondary" onClick={onClose}>
          Cancel
        </button>
        <button className="primary" onClick={create} disabled={busy || !name.trim()}>
          {busy ? 'Creating…' : 'Create'}
        </button>
      </div>
    </Sheet>
  )
}

/** A symlink to a directory opens as one; a broken one opens as nothing. */
function isDirectory(entry: DirEntry): boolean {
  return entry.type === 'dir' || (entry.type === 'link' && entry.linkType === 'dir')
}

export function join(dir: string, name: string): string {
  return dir === '/' ? `/${name}` : `${dir}/${name}`
}

export function name(path: string): string {
  const parts = path.split('/').filter(Boolean)
  return parts.length ? parts[parts.length - 1] : '/'
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { Page } from '../components/Layout'
import { Banner, Card, Field } from '../components/ui'

/** Add or edit a host. Both use the same fields, so they share a screen. */
export default function HostForm() {
  const { id } = useParams()
  const hostId = id ? Number(id) : null
  const navigate = useNavigate()

  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [port, setPort] = useState('22')
  const [username, setUsername] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [loaded, setLoaded] = useState(hostId === null)

  useEffect(() => {
    if (hostId === null) return
    api
      .host(hostId)
      .then((host) => {
        setName(host.name)
        setAddress(host.address)
        setPort(String(host.port))
        setUsername(host.username)
        setLoaded(true)
      })
      .catch((e) => setError(e.message))
  }, [hostId])

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError(null)
    const input = { name, address, port: Number(port) || 22, username }
    try {
      const host = hostId === null ? await api.createHost(input) : await api.updateHost(hostId, input)
      navigate(`/hosts/${host.id}`, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setSaving(false)
    }
  }

  return (
    <Page title={hostId === null ? 'Add host' : 'Edit host'} back={hostId === null ? '/hosts' : `/hosts/${hostId}`}>
      {error && <Banner tone="bad">{error}</Banner>}
      {!loaded ? (
        <div className="empty">Loading…</div>
      ) : (
        <form onSubmit={save}>
          <Card>
            <Field label="Name" help="What you'll call it in Deployer.">
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="nakedpi"
                autoCapitalize="none"
                autoCorrect="off"
                required
              />
            </Field>
            <Field label="Address" help="Hostname or IP address, reachable over SSH.">
              <input
                value={address}
                onChange={(e) => setAddress(e.target.value)}
                placeholder="nakedpi.local"
                autoCapitalize="none"
                autoCorrect="off"
                inputMode="url"
                required
              />
            </Field>
            <Field label="SSH user" help="Needs passwordless sudo for installs that require root.">
              <input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="pi"
                autoCapitalize="none"
                autoCorrect="off"
                required
              />
            </Field>
            <Field label="Port">
              <input
                value={port}
                onChange={(e) => setPort(e.target.value)}
                inputMode="numeric"
                pattern="[0-9]*"
                placeholder="22"
              />
            </Field>
          </Card>

          <p className="sub" style={{ margin: '0 4px 12px' }}>
            After saving, Deployer will try to connect. If it can't, Settings has the public key and
            the two commands to run on the host.
          </p>

          <button className="primary block" type="submit" disabled={saving}>
            {saving ? 'Saving…' : hostId === null ? 'Add host' : 'Save changes'}
          </button>
        </form>
      )}
    </Page>
  )
}

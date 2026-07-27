import { useState } from 'react'
import { api } from '../api'
import { Page } from '../components/Layout'
import { Banner, Card, Copyable, SectionTitle, Sheet, useLoader } from '../components/ui'

export default function Settings() {
  const { data, error, reload } = useLoader(() => api.sshKey(), [])
  const [confirmRotate, setConfirmRotate] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)

  const rotate = async () => {
    setRotating(true)
    try {
      await api.rotateSSHKey()
      setNotice('New key generated. Add it to every host before the next deployment.')
      reload()
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e))
    } finally {
      setRotating(false)
      setConfirmRotate(false)
    }
  }

  return (
    <Page title="Settings">
      {error && <Banner tone="bad">{error}</Banner>}
      {notice && <Banner tone="warn">{notice}</Banner>}

      <SectionTitle>Adding a host</SectionTitle>
      <p className="sub" style={{ margin: '0 4px 12px' }}>
        Run these two commands on the machine, signed in as the user Deployer will connect as.
      </p>

      {data && (
        <>
          <Card>
            <div className="meter-label">
              <span>1 · Trust Deployer's key</span>
            </div>
            <Copyable text={data.authorizeCommand} />
          </Card>

          <Card>
            <div className="meter-label">
              <span>2 · Allow unattended installs</span>
            </div>
            <p className="sub" style={{ marginTop: 0 }}>
              Install scripts end in <span className="mono">| sudo bash</span>, so the user needs sudo
              without a password prompt.
            </p>
            <Copyable text={data.sudoCommand} />
          </Card>

          <SectionTitle>Deployer's SSH key</SectionTitle>
          <Card>
            <div className="meter-label">
              <span>Public key</span>
            </div>
            <Copyable text={data.publicKey} />
            <div className="sub" style={{ marginTop: 10 }}>
              Fingerprint <span className="mono">{data.fingerprint}</span>
            </div>
          </Card>

          <Card>
            <p className="sub" style={{ marginTop: 0 }}>
              Rotating generates a new keypair. Every host stops accepting Deployer until you add the
              new public key to it, so keep this for when a key may have leaked.
            </p>
            <button className="danger block" onClick={() => setConfirmRotate(true)}>
              Rotate key
            </button>
          </Card>
        </>
      )}

      <SectionTitle>About</SectionTitle>
      <Card>
        <p className="sub" style={{ marginTop: 0 }}>
          Deployer holds a key that can run commands as root on your hosts. Keep it on your LAN or
          Tailscale network — don't expose it to the internet. Its database also holds the private
          key, so treat backups of it as a secret.
        </p>
      </Card>

      {confirmRotate && (
        <Sheet
          title="Rotate the SSH key?"
          subtitle="Every host will need the new public key added before it will accept Deployer again."
          onClose={() => setConfirmRotate(false)}
        >
          <div className="actions">
            <button className="secondary" onClick={() => setConfirmRotate(false)}>
              Cancel
            </button>
            <button className="danger" onClick={rotate} disabled={rotating}>
              {rotating ? 'Rotating…' : 'Rotate'}
            </button>
          </div>
        </Sheet>
      )}
    </Page>
  )
}

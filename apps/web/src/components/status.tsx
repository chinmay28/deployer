import { Badge } from './ui'
import type { DeploymentStatus, HealthStatus, HostStatus, ServiceUnit } from '../types'

export function HostBadge({ status }: { status: HostStatus }) {
  switch (status) {
    case 'online':
      return (
        <Badge tone="good" dot>
          Online
        </Badge>
      )
    case 'offline':
      return (
        <Badge tone="bad" dot>
          Offline
        </Badge>
      )
    case 'error':
      return (
        <Badge tone="bad" dot>
          Needs attention
        </Badge>
      )
    default:
      return (
        <Badge tone="neutral" dot>
          Not checked
        </Badge>
      )
  }
}

/** HomeBadge marks the machine Deployer itself is running on. */
export function HomeBadge() {
  return <Badge tone="accent">Home</Badge>
}

/** What a service is doing now. A one-shot unit that ran and exited is not
 *  running and has not failed either — calling that "stopped" would read as a
 *  problem, so it gets its own word. */
export function ServiceBadge({ unit }: { unit: ServiceUnit }) {
  switch (unit.active) {
    case 'active':
      return unit.sub === 'exited' ? (
        <Badge tone="neutral" dot>
          Finished
        </Badge>
      ) : (
        <Badge tone="good" dot>
          Running
        </Badge>
      )
    case 'failed':
      return (
        <Badge tone="bad" dot>
          Failed
        </Badge>
      )
    case 'activating':
      return (
        <Badge tone="accent" dot pulse>
          Starting
        </Badge>
      )
    case 'deactivating':
      return (
        <Badge tone="warn" dot pulse>
          Stopping
        </Badge>
      )
    case 'reloading':
      return (
        <Badge tone="accent" dot pulse>
          Reloading
        </Badge>
      )
    default:
      return (
        <Badge tone="neutral" dot>
          Stopped
        </Badge>
      )
  }
}

/** Whether the service comes back on its own after a reboot — the question
 *  "is it running?" does not answer, and the one people forget. */
export function BootBadge({ unit }: { unit: ServiceUnit }) {
  switch (unit.fileState) {
    case 'enabled':
      return <Badge tone="accent">Starts at boot</Badge>
    case 'enabled-runtime':
      return <Badge tone="warn">Until reboot</Badge>
    case 'disabled':
      return <Badge tone="neutral">Manual start</Badge>
    case 'static':
      return <Badge tone="neutral">Started by another unit</Badge>
    case 'masked':
      return <Badge tone="warn">Masked</Badge>
    default:
      return null
  }
}

export function HealthBadge({ status }: { status: HealthStatus }) {
  switch (status) {
    case 'passing':
      return <Badge tone="good">Healthy</Badge>
    case 'failing':
      return <Badge tone="bad">Not responding</Badge>
    case 'unchecked':
      return <Badge tone="neutral">No health check</Badge>
    default:
      return <Badge tone="neutral">Unknown</Badge>
  }
}

export function DeploymentBadge({ status }: { status: DeploymentStatus }) {
  switch (status) {
    case 'running':
      return (
        <Badge tone="accent" dot pulse>
          Running
        </Badge>
      )
    case 'succeeded':
      return <Badge tone="good">Succeeded</Badge>
    case 'failed':
      return <Badge tone="bad">Failed</Badge>
    case 'canceled':
      return <Badge tone="warn">Canceled</Badge>
    case 'interrupted':
      return <Badge tone="warn">Interrupted</Badge>
  }
}

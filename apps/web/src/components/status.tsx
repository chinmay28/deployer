import { Badge } from './ui'
import type { DeploymentStatus, HealthStatus, HostStatus } from '../types'

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

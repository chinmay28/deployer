export type HostStatus = 'unknown' | 'online' | 'offline' | 'error'

export interface Disk {
  mount: string
  device: string
  totalBytes: number
  usedBytes: number
}

export interface Sample {
  hostId: number
  takenAt: string
  cpuPct: number
  memUsed: number
  memTotal: number
  swapUsed: number
  swapTotal: number
  load1: number
  uptimeS: number
  tempC: number | null
  disks: Disk[]
}

export interface Host {
  id: number
  name: string
  address: string
  port: number
  username: string
  status: HostStatus
  lastError: string
  lastSeenAt: string | null
  hostname: string
  os: string
  kernel: string
  arch: string
  sudoOk: boolean
  createdAt: string
  /** True for the machine Deployer itself runs on. */
  isSelf: boolean
  latest: Sample | null
}

export interface HostTestResult {
  ok: boolean
  error?: string
  sudoOk: boolean
  hostname?: string
  os?: string
  kernel?: string
  arch?: string
  hints?: string[]
}

export interface ProvisionStep {
  name: string
  ok: boolean
  detail?: string
}

/** The outcome of the one-time setup that authorizes Deployer on a host. */
export interface ProvisionResult {
  ok: boolean
  error?: string
  sudoOk: boolean
  steps: ProvisionStep[]
  hints?: string[]
}

/** What a directory entry is. A symlink keeps its own type and says what it
 *  resolves to, so the browser can open it as what it points at. */
export type EntryType = 'dir' | 'file' | 'link' | 'other'

export interface DirEntry {
  name: string
  type: EntryType
  /** For a symlink: 'dir', 'file', 'other', or 'broken' where it leads nowhere. */
  linkType?: EntryType | 'broken'
  /** The symlink as written, unresolved. */
  target?: string
  size: number
  /** Permission bits in octal, e.g. "644". */
  mode: string
  owner: string
  group: string
  modifiedAt: string
}

export interface DirListing {
  /** Where the host ended up, with symlinks resolved. */
  path: string
  parent: string
  entries: DirEntry[]
  /** True when the directory holds more than the listing returns. */
  truncated: boolean
  /** Who the commands ran as — root wherever passwordless sudo is set up. */
  asUser: string
}

export interface HostFile {
  path: string
  size: number
  mode: string
  owner: string
  group: string
  modifiedAt: string
  content: string
  /** Only the first part of the file came back. */
  truncated: boolean
  /** Not text: shown, never offered for editing. */
  binary: boolean
  asUser: string
}

export interface Crontab {
  user: string
  content: string
  /** False where the user has no crontab yet, which is not an error. */
  exists: boolean
}

export type HealthType = 'none' | 'http' | 'systemd'
export type HealthStatus = 'unknown' | 'passing' | 'failing' | 'unchecked'

export interface Param {
  name: string
  label: string
  default: string
  help: string
  required: boolean
}

export interface App {
  id: number
  name: string
  description: string
  installCommand: string
  params: Param[]
  healthType: HealthType
  healthTarget: string
  createdAt: string
  /** True for the app that installs Deployer itself. */
  selfUpdate: boolean
}

export type DeploymentStatus = 'running' | 'succeeded' | 'failed' | 'canceled' | 'interrupted'

export interface Deployment {
  id: number
  appId: number
  hostId: number
  command: string
  params: Record<string, string>
  status: DeploymentStatus
  exitCode: number | null
  error: string
  log?: string
  startedAt: string
  finishedAt: string | null
  appName?: string
  hostName?: string
}

export interface Installation {
  id: number
  appId: number
  hostId: number
  params: Record<string, string>
  lastDeploymentId: number | null
  healthStatus: HealthStatus
  healthDetail: string
  healthCheckedAt: string | null
  installedAt: string
  updatedAt: string
  appName: string
  hostName: string
  hostAddress: string
  healthType: HealthType
  healthTarget: string
  lastStatus: DeploymentStatus | ''
}

export interface Overview {
  hosts: Host[]
  installations: Installation[]
  recentDeployments: Deployment[]
}

export interface SelfInfo {
  version: string
  machineId: string
  host: Host | null
  app: App | null
  ref: string
  runningDeploymentId: number | null
  ready: boolean
  blocked?: string
}

export interface SSHKeyInfo {
  publicKey: string
  fingerprint: string
  authorizeCommand: string
  sudoCommand: string
}

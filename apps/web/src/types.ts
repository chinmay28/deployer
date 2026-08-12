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

/** What one metric did over a window: its floor, its ceiling and its mean. */
export interface Stat {
  min: number
  max: number
  avg: number
}

/** A host's CPU and memory over the last day, reduced by the server so the
 *  phone never carries a day of samples to work out six numbers. */
export interface MetricSummary {
  hostId: number
  since: string
  /** How many samples the numbers came from. Zero on a host with no history. */
  samples: number
  cpuPct: Stat
  /** The share of memory in use, and the same thing in bytes. */
  memPct: Stat
  memUsed: Stat
  memTotal: number
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

/** One systemd service, as `systemctl show` describes it. Deployer lists the
 *  ones an administrator installed by hand, not the distribution's own. */
export interface ServiceUnit {
  name: string
  description: string
  /** systemd's LoadState: 'loaded', 'not-found', 'masked', 'error'. */
  load: string
  /** ActiveState: 'active', 'inactive', 'failed', 'activating', 'deactivating'. */
  active: string
  /** The finer state within active: 'running', 'exited', 'dead'. */
  sub: string
  /** UnitFileState: 'enabled', 'disabled', 'static', 'masked', or empty. */
  fileState: string
  /** The unit file systemd read, which is the one the editor opens. */
  path: string
  /** True for a foo@.service, which is a pattern rather than a service. */
  template: boolean
  /** True for a .timer, which runs nothing itself and starts another unit on a
   *  schedule. Most of the fields below mean something else on one. */
  timer: boolean
  /** For a timer: the unit it starts when it fires. */
  triggers?: string
  /** For a timer: seconds until it next fires. 0 where systemd does not say,
   *  which is what a stopped timer says. */
  nextS?: number
  /** For a timer: how long ago it last fired, in seconds. 0 where it never
   *  has. */
  lastS?: number
  mainPid: number
  /** What its cgroup is using now; 0 where systemd does not account for it. */
  memory: number
  /** How many times systemd has restarted it by itself. */
  restarts: number
  /** How long it has been in its current state, in seconds. */
  sinceS: number
  /** Why it last stopped: 'success', 'exit-code', 'signal', 'timeout'. */
  result: string
  /** Why systemd could not read the unit file, where it could not. */
  loadError?: string
  /** The units that pull this one in, most specific relation first — the
   *  answer a 'static' unit file leaves out. Only the single-service call asks
   *  systemd for it, so it is absent on a listing. */
  startedBy?: string[]
}

export interface ServiceList {
  units: ServiceUnit[]
  /** Who the commands ran as — root wherever passwordless sudo is set up. */
  asUser: string
  /** The host has more hand-installed units than Deployer will list. */
  truncated: boolean
}

export interface ServiceLog {
  name: string
  lines: number
  content: string
  /** The log was longer than Deployer will carry and lost its beginning. */
  truncated: boolean
  asUser: string
}

/** Everything Deployer will ask systemd to do. */
export type ServiceAction = 'start' | 'stop' | 'restart' | 'reload' | 'enable' | 'disable'

/** An action answers with the service as it now is — or, where reading it back
 *  afterwards failed, with just what was done. */
export type ServiceActionResult = ServiceUnit | { name: string; action: string }

/** What Deployer thinks took a host down the last time it restarted. */
export type BootCause =
  | 'clean'
  | 'panic'
  | 'lockup'
  | 'oom'
  | 'overheat'
  | 'undervoltage'
  | 'power'
  | 'storage'
  | 'unknown'

/** How much the verdict is worth. 'certain' is only ever used where the machine
 *  said so in as many words; everything inferred is at best 'likely'. */
export type BootConfidence = 'certain' | 'likely' | 'unclear'

/** Where the evidence came from: systemd's journal, an rsyslog file where the
 *  journal keeps nothing across restarts, or nowhere at all. */
export type BootSource = 'journal' | 'logfile' | 'none'

/** One thing found in the record that bears on why the machine went down. */
export interface BootSign {
  kind: string
  label: string
  /** The log line it was found in, so the verdict can be checked. */
  line: string
  /** The part worth pulling out: the panic's reason, the process the
   *  out-of-memory killer picked. */
  detail?: string
  /** How many seconds before the machine came back this was logged. */
  beforeS?: number
  /** Close enough to the restart to be part of it, rather than something that
   *  also happened during that boot. */
  near: boolean
  count: number
}

/** One time the machine came up, as wtmp remembers it. */
export interface Restart {
  bootedAt: string
  /** How long that boot lasted, or how long the current one has been running. */
  upS?: number
  /** True where a shutdown was recorded before it — something asked. Only
   *  meaningful when the report's cleanKnown is set. */
  clean: boolean
  kernel?: string
  current?: boolean
  /** False where the record was found but its time was not, which is what an
   *  old `last` with no ISO stamps leaves behind. */
  timed: boolean
}

/** The Raspberry Pi firmware's own account of its power and heat. The "since
 *  boot" flags describe the boot that is running now, not the one that ended,
 *  which makes them corroboration rather than proof. */
export interface Throttle {
  raw: string
  underVoltageNow: boolean
  cappedNow: boolean
  throttledNow: boolean
  softTempNow: boolean
  underVoltage: boolean
  capped: boolean
  throttled: boolean
  softTemp: boolean
}

/** What the hardware watchdog says about the reset that started this boot. Not
 *  every driver fills it in, so it is shown and never relied on. */
export interface Watchdog {
  bootStatus: number
  flags?: string[]
}

/** Deployer's answer to "why did it restart?", with everything it looked at. */
export interface BootReport {
  cause: BootCause
  confidence: BootConfidence
  headline: string
  detail: string
  /** The steps to the verdict, in the order they mattered. */
  reasons: string[]

  bootedAt: string
  uptimeS: number
  /** How long the boot before this one lasted. */
  previousUpS?: number

  model?: string
  kernel?: string
  /** What it was running before, where that is known and different — a restart
   *  that changed the kernel was an update. */
  previousKernel?: string

  signs: BootSign[]
  restarts: Restart[]
  /** How many of those restarts nothing asked for. */
  unclean: number
  /** Whether the history can tell a restart that was asked for from one that
   *  was not. busybox's `last` cannot. */
  cleanKnown: boolean

  source: BootSource
  /** Whether the host has journalctl at all. */
  journal: boolean
  /** Whether its journal survives a restart. False is the reason this screen
   *  usually has nothing to show. */
  persistent: boolean
  /** The rsyslog file the evidence came out of, where the journal had none. */
  logFile?: string
  bootsKept?: number

  /** The last thing the machine said before it went. */
  logTail: string
  truncated: boolean

  throttle?: Throttle
  watchdog?: Watchdog
  tempC?: number
  asUser: string
}

/** The state of persistent logging on a host, and what came of turning it on. */
export interface JournalStorage {
  enabled: boolean
  /** True where it was already on and Deployer changed nothing. */
  already: boolean
  /** What journald.conf says about Storage=, where it says anything. */
  configured?: string
  /** Why the journal still will not survive a restart, where it will not. */
  blocked?: string
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
  /** Ports the app answers on, worked out from its health check and its
   *  parameters. Absent when neither of them says. */
  ports?: number[]
  /** The version deployed here, worked out from the parameters that named it.
   *  Absent for an app whose command installs whatever is current. */
  version?: string
  /** Where to open the app in a browser, from the same two sources. Absent
   *  when neither of them says enough to name an address. */
  url?: string
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

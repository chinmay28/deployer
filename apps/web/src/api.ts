import type {
  App,
  BootReport,
  Crontab,
  Deployment,
  DirListing,
  Host,
  HostFile,
  HostProcesses,
  HostTestResult,
  Installation,
  JournalStorage,
  MetricSummary,
  Overview,
  ProvisionResult,
  Sample,
  SelfInfo,
  ServiceAction,
  ServiceActionResult,
  ServiceList,
  ServiceLog,
  ServiceUnit,
  SSHKeyInfo,
} from './types'

/** ApiError carries the server's message so the UI can show it verbatim. */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      ...init,
      headers: init?.body ? { 'Content-Type': 'application/json', ...init?.headers } : init?.headers,
    })
  } catch {
    throw new ApiError(0, 'Could not reach Deployer. Is it still running?')
  }
  if (response.status === 204) return undefined as T
  const text = await response.text()
  const body = text ? JSON.parse(text) : undefined
  if (!response.ok) {
    throw new ApiError(response.status, body?.error ?? `Request failed (${response.status})`)
  }
  return body as T
}

const json = (body: unknown): RequestInit => ({ body: JSON.stringify(body) })

export interface HostInput {
  name: string
  address: string
  port: number
  username: string
}

export interface AppInput {
  name: string
  description: string
  installCommand: string
  params: App['params']
  healthType: App['healthType']
  healthTarget: string
}

export const api = {
  overview: () => request<Overview>('/api/overview'),

  hosts: () => request<Host[]>('/api/hosts'),
  host: (id: number) => request<Host>(`/api/hosts/${id}`),
  createHost: (input: HostInput) => request<Host>('/api/hosts', { method: 'POST', ...json(input) }),
  updateHost: (id: number, input: Partial<HostInput>) =>
    request<Host>(`/api/hosts/${id}`, { method: 'PATCH', ...json(input) }),
  deleteHost: (id: number) => request<void>(`/api/hosts/${id}`, { method: 'DELETE' }),
  testHost: (id: number) => request<HostTestResult>(`/api/hosts/${id}/test`, { method: 'POST' }),
  /** One-time setup. The password is sent, used and forgotten — never stored. */
  provisionHost: (id: number, password: string) =>
    request<ProvisionResult>(`/api/hosts/${id}/provision`, { method: 'POST', ...json({ password }) }),
  /** Samples cover the window asked for; the summary always covers the last 24
   *  hours, which is everything the server keeps. The process snapshot comes
   *  from the newest probe — the same round trip that took the sample, so
   *  seeing what a host is busy with costs no extra SSH session. */
  hostMetrics: (id: number, minutes = 60) =>
    request<{
      hostId: number
      minutes: number
      samples: Sample[]
      summary: MetricSummary
      processes: HostProcesses | null
    }>(`/api/hosts/${id}/metrics?minutes=${minutes}`),
  /** Restart the machine. It goes down a few seconds after this returns.
   *  There is no shutdown: Deployer cannot turn a host back on. */
  reboot: (id: number) =>
    request<{ status: string }>(`/api/hosts/${id}/reboot`, { method: 'POST' }),

  /** Why the machine restarted last: Deployer's best guess and the evidence
   *  behind it. One SSH session that reads wtmp, the previous boot's log and
   *  the Pi firmware's throttle flags, so it is asked for on demand and never
   *  polled. */
  boot: (id: number) => request<BootReport>(`/api/hosts/${id}/boot`),
  /** Make the host's journal survive a restart, which is what turns "there is
   *  no record of it" into an answer next time. Idempotent. */
  keepJournal: (id: number) =>
    request<JournalStorage>(`/api/hosts/${id}/boot/journal`, { method: 'POST' }),

  /** An empty user means the account Deployer signs in as. */
  crontab: (id: number, user = '') =>
    request<Crontab>(`/api/hosts/${id}/cron?user=${encodeURIComponent(user)}`),
  saveCrontab: (id: number, user: string, content: string) =>
    request<Crontab>(`/api/hosts/${id}/cron`, { method: 'PUT', ...json({ user, content }) }),

  /** The services someone installed on this host by hand. Every one of these
   *  opens an SSH session, so they are asked for on demand, never polled. */
  services: (id: number) => request<ServiceList>(`/api/hosts/${id}/services`),
  service: (id: number, name: string) =>
    request<ServiceUnit>(`/api/hosts/${id}/services/unit?name=${encodeURIComponent(name)}`),
  serviceLog: (id: number, name: string, lines: number) =>
    request<ServiceLog>(
      `/api/hosts/${id}/services/logs?name=${encodeURIComponent(name)}&lines=${lines}`,
    ),
  /** Runs one systemctl verb. It returns once systemd has finished, which for
   *  a service that takes its time starting is a while. */
  serviceAction: (id: number, name: string, action: ServiceAction) =>
    request<ServiceActionResult>(`/api/hosts/${id}/services/action`, {
      method: 'POST',
      ...json({ name, action }),
    }),
  /** Writes a new unit file and hands it to systemd. The service is created
   *  stopped: starting it and enabling it are separate calls, so a unit that
   *  will not start is a service that exists and did not start. systemd
   *  validates the file, and anything it refuses to load is taken back off
   *  the disk rather than left there being wrong. */
  createService: (id: number, name: string, content: string) =>
    request<ServiceUnit>(`/api/hosts/${id}/services`, { method: 'POST', ...json({ name, content }) }),
  /** Deletes a service that is not running: its unit file, the symlinks
   *  enabling it, and its drop-in overrides. Whatever it ran is left alone. */
  deleteService: (id: number, name: string) =>
    request<void>(`/api/hosts/${id}/services?name=${encodeURIComponent(name)}`, { method: 'DELETE' }),

  /** daemon-reload: what turns an edited unit file into an edited service. */
  reloadServices: (id: number) =>
    request<{ status: string }>(`/api/hosts/${id}/services/reload`, { method: 'POST' }),

  /** An empty path lists the SSH user's home, which only the host knows. */
  files: (id: number, path = '') =>
    request<DirListing>(`/api/hosts/${id}/files?path=${encodeURIComponent(path)}`),
  file: (id: number, path: string) =>
    request<HostFile>(`/api/hosts/${id}/files/content?path=${encodeURIComponent(path)}`),
  saveFile: (id: number, path: string, content: string) =>
    request<HostFile>(`/api/hosts/${id}/files/content`, { method: 'PUT', ...json({ path, content }) }),
  mkdir: (id: number, path: string) =>
    request<{ path: string }>(`/api/hosts/${id}/files/mkdir`, { method: 'POST', ...json({ path }) }),
  renameFile: (id: number, path: string, to: string) =>
    request<{ path: string }>(`/api/hosts/${id}/files/rename`, { method: 'POST', ...json({ path, to }) }),
  deleteFile: (id: number, path: string, recursive = false) =>
    request<void>(
      `/api/hosts/${id}/files?path=${encodeURIComponent(path)}${recursive ? '&recursive=true' : ''}`,
      { method: 'DELETE' },
    ),

  apps: () => request<App[]>('/api/apps'),
  app: (id: number) => request<App>(`/api/apps/${id}`),
  createApp: (input: AppInput) => request<App>('/api/apps', { method: 'POST', ...json(input) }),
  updateApp: (id: number, input: Partial<AppInput>) =>
    request<App>(`/api/apps/${id}`, { method: 'PATCH', ...json(input) }),
  deleteApp: (id: number) => request<void>(`/api/apps/${id}`, { method: 'DELETE' }),
  deploy: (appId: number, hostId: number, params: Record<string, string>) =>
    request<Deployment>(`/api/apps/${appId}/deploy`, { method: 'POST', ...json({ hostId, params }) }),

  installations: () => request<Installation[]>('/api/installations'),
  redeploy: (installationId: number, params?: Record<string, string>) =>
    request<Deployment>(`/api/installations/${installationId}/redeploy`, {
      method: 'POST',
      ...json({ params: params ?? {} }),
    }),
  checkInstallation: (id: number) =>
    request<{ healthStatus: string; healthDetail: string }>(`/api/installations/${id}/check`, {
      method: 'POST',
    }),
  forgetInstallation: (id: number) =>
    request<void>(`/api/installations/${id}`, { method: 'DELETE' }),

  deployments: (params: { appId?: number; hostId?: number; limit?: number } = {}) => {
    const query = new URLSearchParams()
    if (params.appId) query.set('appId', String(params.appId))
    if (params.hostId) query.set('hostId', String(params.hostId))
    if (params.limit) query.set('limit', String(params.limit))
    const suffix = query.toString() ? `?${query}` : ''
    return request<Deployment[]>(`/api/deployments${suffix}`)
  },
  deployment: (id: number) => request<Deployment>(`/api/deployments/${id}`),
  cancelDeployment: (id: number) =>
    request<{ status: string }>(`/api/deployments/${id}/cancel`, { method: 'POST' }),

  self: () => request<SelfInfo>('/api/self'),
  selfUpdate: (ref: string) =>
    request<Deployment>('/api/self/update', { method: 'POST', ...json({ ref }) }),

  sshKey: () => request<SSHKeyInfo>('/api/settings/ssh'),
  rotateSSHKey: () => request<SSHKeyInfo>('/api/settings/ssh/rotate', { method: 'POST' }),
}

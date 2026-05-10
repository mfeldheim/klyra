// ui/src/api/client.ts

export type AlarmStatus = 'OK' | 'FIRING' | 'UNKNOWN'
export type Transition = 'FIRING' | 'RESOLVED'

export interface AlarmState {
  monitorName: string
  status: AlarmStatus
  lastCheck: string
  firedAt?: string
  lastValue?: unknown
  message?: string
  icon?: string
  group?: string
  incidentId?: string
}

export interface HistoryEvent {
  monitorName: string
  transition: Transition
  at: string
  message?: string
  incidentId?: string
}

export interface Silence {
  id: string
  monitorName: string
  until: string
  reason?: string
}

export interface StatusResponse {
  alarms: Record<string, AlarmState>
  updatedAt: string
}

export interface MonitorConfig {
  name: string
  type: string
  interval: string
  threshold: { operator: string; value: unknown; for?: string }
  actions: string[]
  group?: string
}

export interface ConfigResponse {
  monitors?: MonitorConfig[]
  actions?: { name: string; type: string }[]
}

export interface MeResponse { user: string }

export interface Incident {
  id: string
  monitorName: string
  firedAt: string
  resolvedAt?: string
  status: 'active' | 'resolved'
  investigationStatus: 'pending' | 'running' | 'complete' | 'failed'
  value?: unknown
  message?: string
  icon?: string
}

export interface IncidentResponse {
  incident: Incident
  active: boolean
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path)
  if (!res.ok) throw new Error(`${path}: ${res.status}`)
  return res.json()
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path}: ${res.status}`)
  return res.json()
}

async function del(path: string): Promise<void> {
  const res = await fetch(path, { method: 'DELETE' })
  if (!res.ok) throw new Error(`${path}: ${res.status}`)
}

export const api = {
  status: () => get<StatusResponse>('/api/status'),
  history: () => get<HistoryEvent[]>('/api/history'),
  config: () => get<ConfigResponse>('/api/config'),
  silences: () => get<Silence[]>('/api/silences'),
  createSilence: (monitor: string, duration: string, reason: string) =>
    post<Silence>('/api/silences', { monitor, duration, reason }),
  deleteSilence: (id: string) => del(`/api/silences/${id}`),
  me: () => get<MeResponse>('/api/me'),
  getIncident: (id: string) => get<IncidentResponse>(`/api/incidents/${id}`),
  chatIncident: (id: string, message: string) =>
    fetch(`/api/incidents/${id}/chat`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message }),
    }),
}

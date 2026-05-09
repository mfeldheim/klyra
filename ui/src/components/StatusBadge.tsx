import type { AlarmStatus } from '../api/client'

const colours: Record<AlarmStatus, string> = {
  OK: '#4caf50',
  FIRING: '#f44336',
  UNKNOWN: '#ff9800',
}

export function StatusBadge({ status }: { status: AlarmStatus }) {
  return (
    <span style={{ color: colours[status], fontWeight: 600, fontSize: 11, textTransform: 'uppercase' }}>
      {status}
    </span>
  )
}

import type { AlarmState } from '../api/client'
import { typeIcon, iconFromName } from '../utils/typeIcon'

interface GroupTileProps {
  name: string
  alarms: AlarmState[]
  typeMap: Record<string, string>
  active: boolean
  onClick: () => void
}

function worstStatus(alarms: AlarmState[]): 'firing' | 'pending' | 'unknown' | 'ok' {
  if (alarms.some(a => a.status === 'FIRING')) return 'firing'
  if (alarms.some(a => a.pendingSince)) return 'pending'
  if (alarms.some(a => a.status === 'UNKNOWN')) return 'unknown'
  return 'ok'
}

export function GroupTile({ name, alarms, typeMap, active, onClick }: GroupTileProps) {
  const status = worstStatus(alarms)

  return (
    <button
      type="button"
      className={`group-tile group-tile--${status}${active ? ' active' : ''}`}
      onClick={onClick}
    >
      <div className="group-tile-name">{name}</div>
      <div className="monitor-tiles">
        {alarms.map(a => (
          <div
            key={a.monitorName}
            className={`monitor-tile ${a.status.toLowerCase()}${a.pendingSince && a.status !== 'FIRING' ? ' pending' : ''}`}
            title={a.monitorName}
          >
            {a.icon ? iconFromName(a.icon, 20) : typeIcon(typeMap[a.monitorName] ?? '', 20)}
            <div className="monitor-tile-name">{a.monitorName}</div>
          </div>
        ))}
      </div>
    </button>
  )
}

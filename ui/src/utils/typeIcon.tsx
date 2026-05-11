import type { JSX } from 'react'
import LanguageRounded from '@mui/icons-material/LanguageRounded'
import HexagonRounded from '@mui/icons-material/HexagonRounded'
import LocalFireDepartmentRounded from '@mui/icons-material/LocalFireDepartmentRounded'
import HelpRounded from '@mui/icons-material/HelpRounded'
import LanRounded from '@mui/icons-material/LanRounded'
import VpnLockRounded from '@mui/icons-material/VpnLockRounded'
import StorageRounded from '@mui/icons-material/StorageRounded'
import MemoryRounded from '@mui/icons-material/MemoryRounded'
import LockRounded from '@mui/icons-material/LockRounded'
import WarningAmberRounded from '@mui/icons-material/WarningAmberRounded'
import CloudRounded from '@mui/icons-material/CloudRounded'
import DnsRounded from '@mui/icons-material/DnsRounded'
import CheckCircleOutlineRounded from '@mui/icons-material/CheckCircleOutlineRounded'
import ScheduleRounded from '@mui/icons-material/ScheduleRounded'

const s = (size: number) => ({ fontSize: size, width: size, height: size })

export function typeIcon(type: string, size = 16): JSX.Element {
  switch (type) {
    case 'http': return <LanguageRounded style={s(size)} />
    case 'kubernetes': return <HexagonRounded style={s(size)} />
    case 'prometheus':
    case 'prometheus_scrape': return <LocalFireDepartmentRounded style={s(size)} />
    default: return <HelpRounded style={s(size)} />
  }
}

export function iconFromName(name: string, size = 16): JSX.Element {
  switch (name) {
    case 'StorageRounded': return <StorageRounded style={s(size)} />
    case 'globe': return <LanguageRounded style={s(size)} />
    case '🌐': return <LanguageRounded style={s(size)} />
    case 'kubernetes': return <HexagonRounded style={s(size)} />
    case '☸️': return <HexagonRounded style={s(size)} />
    case 'network': return <LanRounded style={s(size)} />
    case '📡': return <LanRounded style={s(size)} />
    case 'tunnel': return <VpnLockRounded style={s(size)} />
    case '🚇': return <VpnLockRounded style={s(size)} />
    case 'database': return <StorageRounded style={s(size)} />
    case '🗄️': return <StorageRounded style={s(size)} />
    case 'disk': return <StorageRounded style={s(size)} />
    case '💽': return <StorageRounded style={s(size)} />
    case 'memory': return <MemoryRounded style={s(size)} />
    case '💾': return <MemoryRounded style={s(size)} />
    case 'lock': return <LockRounded style={s(size)} />
    case '🔒': return <LockRounded style={s(size)} />
    case 'warning': return <WarningAmberRounded style={s(size)} />
    case '⚠️': return <WarningAmberRounded style={s(size)} />
    case 'fire': return <LocalFireDepartmentRounded style={s(size)} />
    case '🔥': return <LocalFireDepartmentRounded style={s(size)} />
    case 'cloud': return <CloudRounded style={s(size)} />
    case '☁️': return <CloudRounded style={s(size)} />
    case 'server': return <DnsRounded style={s(size)} />
    case '🖥️': return <DnsRounded style={s(size)} />
    case 'check': return <CheckCircleOutlineRounded style={s(size)} />
    case '✅': return <CheckCircleOutlineRounded style={s(size)} />
    case 'clock': return <ScheduleRounded style={s(size)} />
    case '🕐': return <ScheduleRounded style={s(size)} />
    default:
      return <HelpRounded style={s(size)} />
  }
}

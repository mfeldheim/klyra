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

const s = (size: number) => ({ fontSize: size, width: size, height: size })

function emojiIcon(text: string, size: number): JSX.Element {
  return <span style={{ fontSize: size, lineHeight: 1 }}>{text}</span>
}

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
    case 'kubernetes': return <HexagonRounded style={s(size)} />
    case 'network': return <LanRounded style={s(size)} />
    case 'tunnel': return <VpnLockRounded style={s(size)} />
    case 'database': return <StorageRounded style={s(size)} />
    case 'memory': return <MemoryRounded style={s(size)} />
    case 'lock': return <LockRounded style={s(size)} />
    case 'warning': return <WarningAmberRounded style={s(size)} />
    default:
      // Backend can return resolved emoji (e.g. "🗄️") instead of icon names.
      if (name && name.trim() !== '') {
        return emojiIcon(name, size)
      }
      return <HelpRounded style={s(size)} />
  }
}

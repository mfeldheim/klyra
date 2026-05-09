export function typeIcon(type: string, size = 16): JSX.Element {
  switch (type) {
    case 'http':
      return (
        <svg viewBox="0 0 16 16" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.5">
          <circle cx="8" cy="8" r="7" />
          <path d="M8 1c-1.5 2-2.5 4.5-2.5 7s1 5 2.5 7M8 1c1.5 2 2.5 4.5 2.5 7s-1 5-2.5 7M1 8h14" />
        </svg>
      )
    case 'kubernetes':
      return (
        <svg viewBox="0 0 16 16" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.5">
          <path d="M8 1L14.5 4.5v7L8 15 1.5 11.5v-7z" />
          <circle cx="8" cy="8" r="1.5" fill="currentColor" stroke="none" />
        </svg>
      )
    case 'prometheus':
    case 'prometheus_scrape':
      return (
        <svg viewBox="0 0 16 16" width={size} height={size} fill="currentColor">
          <path d="M8 2c0 2.5-3 3.5-3 7a3 3 0 0 0 6 0c0-1.5-1-2.5-1-4 0 0-1 1-1 2.5C8.5 9.5 7 8.5 7 7 7 5.5 8 2 8 2z" />
        </svg>
      )
    default:
      return (
        <svg viewBox="0 0 16 16" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.5">
          <circle cx="8" cy="8" r="7" />
          <text x="8" y="12" textAnchor="middle" fontSize="9">?</text>
        </svg>
      )
  }
}

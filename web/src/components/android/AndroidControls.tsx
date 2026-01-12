interface AndroidControlsProps {
  onBack: () => void
  onHome: () => void
  onRecent: () => void
  disabled?: boolean
}

export function AndroidControls({
  onBack,
  onHome,
  onRecent,
  disabled = false,
}: AndroidControlsProps) {
  const buttonClass = `
    flex items-center justify-center
    w-12 h-12
    rounded-full
    bg-gray-700 hover:bg-gray-600
    text-white
    transition-colors
    disabled:opacity-50 disabled:cursor-not-allowed
    focus:outline-none focus:ring-2 focus:ring-blue-500
  `

  return (
    <div className="flex items-center gap-6">
      {/* Back button */}
      <button
        type="button"
        onClick={onBack}
        disabled={disabled}
        className={buttonClass}
        title="Back"
        aria-label="Back"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          className="w-6 h-6"
        >
          <path d="m15 18-6-6 6-6" />
        </svg>
      </button>

      {/* Home button */}
      <button
        type="button"
        onClick={onHome}
        disabled={disabled}
        className={buttonClass}
        title="Home"
        aria-label="Home"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          className="w-6 h-6"
        >
          <circle cx="12" cy="12" r="8" />
        </svg>
      </button>

      {/* Recent apps button */}
      <button
        type="button"
        onClick={onRecent}
        disabled={disabled}
        className={buttonClass}
        title="Recent Apps"
        aria-label="Recent Apps"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          className="w-6 h-6"
        >
          <rect x="4" y="4" width="16" height="16" rx="2" />
        </svg>
      </button>
    </div>
  )
}

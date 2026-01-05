import { memo, useMemo } from 'react'
import type { LogLevel, LogStream } from '@/types/api'

interface LogLineProps {
  content: string
  level?: LogLevel
  stream?: LogStream
  timestamp?: string
  sequence?: number
  showTimestamp?: boolean
  showLevel?: boolean
}

// ANSI color code mappings
const ANSI_COLORS: Record<number, string> = {
  30: 'text-gray-900',
  31: 'text-red-600',
  32: 'text-green-600',
  33: 'text-yellow-600',
  34: 'text-blue-600',
  35: 'text-purple-600',
  36: 'text-cyan-600',
  37: 'text-gray-300',
  90: 'text-gray-500',
  91: 'text-red-400',
  92: 'text-green-400',
  93: 'text-yellow-400',
  94: 'text-blue-400',
  95: 'text-purple-400',
  96: 'text-cyan-400',
  97: 'text-white',
}

const ANSI_BG_COLORS: Record<number, string> = {
  40: 'bg-gray-900',
  41: 'bg-red-600',
  42: 'bg-green-600',
  43: 'bg-yellow-600',
  44: 'bg-blue-600',
  45: 'bg-purple-600',
  46: 'bg-cyan-600',
  47: 'bg-gray-300',
}

interface ParsedSegment {
  text: string
  classes: string[]
}

function parseAnsi(text: string): ParsedSegment[] {
  const segments: ParsedSegment[] = []
  // Match ANSI escape sequences (ESC[...m)
  // eslint-disable-next-line no-control-regex
  const ansiRegex = /\x1b\[([0-9;]*)m/g

  let currentClasses: string[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null

  while ((match = ansiRegex.exec(text)) !== null) {
    // Add text before this escape sequence
    if (match.index > lastIndex) {
      const textSegment = text.slice(lastIndex, match.index)
      if (textSegment) {
        segments.push({ text: textSegment, classes: [...currentClasses] })
      }
    }

    // Parse the escape codes
    const codes = match[1].split(';').map(Number)

    for (const code of codes) {
      if (code === 0) {
        // Reset
        currentClasses = []
      } else if (code === 1) {
        currentClasses.push('font-bold')
      } else if (code === 3) {
        currentClasses.push('italic')
      } else if (code === 4) {
        currentClasses.push('underline')
      } else if (code >= 30 && code <= 37) {
        // Remove any existing text color and add new one
        currentClasses = currentClasses.filter((c) => !c.startsWith('text-'))
        const colorClass = ANSI_COLORS[code]
        if (colorClass) currentClasses.push(colorClass)
      } else if (code >= 90 && code <= 97) {
        // Bright colors
        currentClasses = currentClasses.filter((c) => !c.startsWith('text-'))
        const colorClass = ANSI_COLORS[code]
        if (colorClass) currentClasses.push(colorClass)
      } else if (code >= 40 && code <= 47) {
        // Background colors
        currentClasses = currentClasses.filter((c) => !c.startsWith('bg-'))
        const bgClass = ANSI_BG_COLORS[code]
        if (bgClass) currentClasses.push(bgClass)
      }
    }

    lastIndex = ansiRegex.lastIndex
  }

  // Add remaining text
  if (lastIndex < text.length) {
    segments.push({ text: text.slice(lastIndex), classes: [...currentClasses] })
  }

  return segments
}

const levelColors: Record<LogLevel, string> = {
  debug: 'text-gray-500',
  info: 'text-blue-500',
  warn: 'text-yellow-500',
  error: 'text-red-500',
}

const streamColors: Record<LogStream, string> = {
  stdout: 'text-gray-400',
  stderr: 'text-red-400',
  system: 'text-purple-400',
}

function LogLine({
  content,
  level = 'info',
  stream = 'stdout',
  timestamp,
  showTimestamp = false,
  showLevel = false,
}: LogLineProps) {
  const segments = useMemo(() => parseAnsi(content), [content])

  return (
    <div className="flex gap-2 font-mono text-sm leading-5 hover:bg-gray-800/50">
      {showTimestamp && timestamp && (
        <span className="flex-shrink-0 text-gray-500 select-none">
          {new Date(timestamp).toLocaleTimeString()}
        </span>
      )}
      {showLevel && (
        <span className={`flex-shrink-0 uppercase text-xs ${levelColors[level]}`}>
          [{level}]
        </span>
      )}
      <span className={`flex-shrink-0 ${streamColors[stream]}`}>
        {stream === 'stderr' ? '!' : stream === 'system' ? '#' : '>'}
      </span>
      <span className="flex-1 whitespace-pre-wrap break-all">
        {segments.map((segment, index) => (
          <span key={index} className={segment.classes.join(' ')}>
            {segment.text}
          </span>
        ))}
      </span>
    </div>
  )
}

export default memo(LogLine)

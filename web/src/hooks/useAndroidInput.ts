import { useCallback, useRef } from 'react'
import type { AndroidInputEvent } from '@/types/api'

interface UseAndroidInputOptions {
  deviceWidth: number
  deviceHeight: number
  onInput: (event: AndroidInputEvent) => void
}

interface UseAndroidInputReturn {
  handleTouchStart: (e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>) => void
  handleTouchMove: (e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>) => void
  handleTouchEnd: (e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>) => void
  handleKeyDown: (e: React.KeyboardEvent<HTMLElement>) => void
  handleKeyUp: (e: React.KeyboardEvent<HTMLElement>) => void
  sendText: (text: string) => void
  sendBack: () => void
  sendHome: () => void
  sendRecent: () => void
}

// Android key codes
const KEYCODE_BACK = 4
const KEYCODE_HOME = 3
const KEYCODE_APP_SWITCH = 187

// Map browser key codes to Android key codes
function mapKeyCode(key: string): number | null {
  const keyMap: Record<string, number> = {
    Enter: 66,
    Backspace: 67,
    Delete: 112,
    Tab: 61,
    Escape: 111,
    ArrowUp: 19,
    ArrowDown: 20,
    ArrowLeft: 21,
    ArrowRight: 22,
    Home: 122,
    End: 123,
    PageUp: 92,
    PageDown: 93,
    ' ': 62,
  }

  // Check direct mapping
  if (keyMap[key]) {
    return keyMap[key]
  }

  // Map alphanumeric keys
  if (key.length === 1) {
    const char = key.toUpperCase()
    if (char >= 'A' && char <= 'Z') {
      return 29 + (char.charCodeAt(0) - 'A'.charCodeAt(0))
    }
    if (char >= '0' && char <= '9') {
      return 7 + (char.charCodeAt(0) - '0'.charCodeAt(0))
    }
  }

  return null
}

export function useAndroidInput(options: UseAndroidInputOptions): UseAndroidInputReturn {
  const { deviceWidth, deviceHeight, onInput } = options

  const isMouseDown = useRef(false)
  const lastPosition = useRef<{ x: number; y: number } | null>(null)

  // Convert element coordinates to device coordinates
  const toDeviceCoords = useCallback(
    (
      clientX: number,
      clientY: number,
      element: HTMLElement
    ): { x: number; y: number } => {
      const rect = element.getBoundingClientRect()
      const x = ((clientX - rect.left) / rect.width) * deviceWidth
      const y = ((clientY - rect.top) / rect.height) * deviceHeight
      return {
        x: Math.max(0, Math.min(deviceWidth, Math.round(x))),
        y: Math.max(0, Math.min(deviceHeight, Math.round(y))),
      }
    },
    [deviceWidth, deviceHeight]
  )

  // Get position from event
  const getPosition = useCallback(
    (
      e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>
    ): { x: number; y: number } | null => {
      const target = e.currentTarget

      if ('touches' in e) {
        // Touch event
        if (e.touches.length > 0) {
          return toDeviceCoords(e.touches[0].clientX, e.touches[0].clientY, target)
        }
        if (e.changedTouches.length > 0) {
          return toDeviceCoords(
            e.changedTouches[0].clientX,
            e.changedTouches[0].clientY,
            target
          )
        }
      } else {
        // Mouse event
        return toDeviceCoords(e.clientX, e.clientY, target)
      }

      return null
    },
    [toDeviceCoords]
  )

  const handleTouchStart = useCallback(
    (e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>) => {
      const pos = getPosition(e)
      if (!pos) return

      if (!('touches' in e)) {
        isMouseDown.current = true
      }

      lastPosition.current = pos
      onInput({
        type: 'touch',
        action: 'down',
        x: pos.x,
        y: pos.y,
      })
    },
    [getPosition, onInput]
  )

  const handleTouchMove = useCallback(
    (e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>) => {
      // For mouse events, only process if mouse is down
      if (!('touches' in e) && !isMouseDown.current) {
        return
      }

      const pos = getPosition(e)
      if (!pos) return

      // Throttle move events - only send if position changed significantly
      if (lastPosition.current) {
        const dx = Math.abs(pos.x - lastPosition.current.x)
        const dy = Math.abs(pos.y - lastPosition.current.y)
        if (dx < 2 && dy < 2) {
          return
        }
      }

      lastPosition.current = pos
      onInput({
        type: 'touch',
        action: 'move',
        x: pos.x,
        y: pos.y,
      })
    },
    [getPosition, onInput]
  )

  const handleTouchEnd = useCallback(
    (e: React.TouchEvent<HTMLElement> | React.MouseEvent<HTMLElement>) => {
      const pos = getPosition(e) || lastPosition.current
      if (!pos) return

      if (!('touches' in e)) {
        isMouseDown.current = false
      }

      lastPosition.current = null
      onInput({
        type: 'touch',
        action: 'up',
        x: pos.x,
        y: pos.y,
      })
    },
    [getPosition, onInput]
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLElement>) => {
      // Prevent default for keys we handle
      const keyCode = mapKeyCode(e.key)
      if (keyCode !== null) {
        e.preventDefault()
        onInput({
          type: 'key',
          key_code: keyCode,
          key_action: 'down',
        })
      }
    },
    [onInput]
  )

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent<HTMLElement>) => {
      const keyCode = mapKeyCode(e.key)
      if (keyCode !== null) {
        e.preventDefault()
        onInput({
          type: 'key',
          key_code: keyCode,
          key_action: 'up',
        })
      }
    },
    [onInput]
  )

  const sendText = useCallback(
    (text: string) => {
      onInput({
        type: 'text',
        text,
      })
    },
    [onInput]
  )

  const sendBack = useCallback(() => {
    onInput({ type: 'key', key_code: KEYCODE_BACK, key_action: 'down' })
    onInput({ type: 'key', key_code: KEYCODE_BACK, key_action: 'up' })
  }, [onInput])

  const sendHome = useCallback(() => {
    onInput({ type: 'key', key_code: KEYCODE_HOME, key_action: 'down' })
    onInput({ type: 'key', key_code: KEYCODE_HOME, key_action: 'up' })
  }, [onInput])

  const sendRecent = useCallback(() => {
    onInput({ type: 'key', key_code: KEYCODE_APP_SWITCH, key_action: 'down' })
    onInput({ type: 'key', key_code: KEYCODE_APP_SWITCH, key_action: 'up' })
  }, [onInput])

  return {
    handleTouchStart,
    handleTouchMove,
    handleTouchEnd,
    handleKeyDown,
    handleKeyUp,
    sendText,
    sendBack,
    sendHome,
    sendRecent,
  }
}

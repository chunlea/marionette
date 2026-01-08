import { useRef, useEffect, useCallback, useState } from 'react'
import type { WSFrameMessage, WSInputEvent, WSMouseEvent, WSKeyboardEvent } from './types'

interface StreamCanvasProps {
  frame: WSFrameMessage | null
  width?: number
  height?: number
  onInput?: (event: WSInputEvent) => void
  interactive?: boolean
  className?: string
}

export function StreamCanvas({
  frame,
  width,
  height,
  onInput,
  interactive = true,
  className = '',
}: StreamCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const [canvasSize, setCanvasSize] = useState({ width: 0, height: 0 })
  const [scale, setScale] = useState(1)

  // Calculate scale for coordinate translation
  const updateScale = useCallback(() => {
    if (!containerRef.current || !frame) return

    const container = containerRef.current
    const containerWidth = container.clientWidth
    const containerHeight = container.clientHeight

    // Maintain aspect ratio
    const frameAspect = frame.width / frame.height
    const containerAspect = containerWidth / containerHeight

    let displayWidth: number
    let displayHeight: number

    if (containerAspect > frameAspect) {
      // Container is wider, fit to height
      displayHeight = containerHeight
      displayWidth = displayHeight * frameAspect
    } else {
      // Container is taller, fit to width
      displayWidth = containerWidth
      displayHeight = displayWidth / frameAspect
    }

    setCanvasSize({ width: displayWidth, height: displayHeight })
    setScale(displayWidth / frame.width)
  }, [frame])

  // Resize observer
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const resizeObserver = new ResizeObserver(() => {
      updateScale()
    })

    resizeObserver.observe(container)
    return () => resizeObserver.disconnect()
  }, [updateScale])

  // Render frame to canvas
  useEffect(() => {
    if (!frame || !canvasRef.current) return

    const canvas = canvasRef.current
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    // Set canvas dimensions to match frame
    canvas.width = frame.width
    canvas.height = frame.height

    // Decode base64 and draw
    const img = new Image()
    img.onload = () => {
      ctx.drawImage(img, 0, 0)
    }
    img.src = `data:image/${frame.format};base64,${frame.data}`
  }, [frame])

  // Translate screen coordinates to viewport coordinates
  const translateCoordinates = useCallback(
    (clientX: number, clientY: number): { x: number; y: number } => {
      if (!canvasRef.current || !frame) return { x: 0, y: 0 }

      const rect = canvasRef.current.getBoundingClientRect()
      const x = (clientX - rect.left) / scale
      const y = (clientY - rect.top) / scale

      return { x: Math.round(x), y: Math.round(y) }
    },
    [scale, frame]
  )

  // Mouse event handlers
  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (!interactive || !onInput) return

      const coords = translateCoordinates(e.clientX, e.clientY)
      const button = getMouseButton(e.button)

      const mouseEvent: WSMouseEvent = {
        x: coords.x,
        y: coords.y,
        button,
      }

      onInput({
        event_type: 'mouseDown',
        mouse: mouseEvent,
      })
    },
    [interactive, onInput, translateCoordinates]
  )

  const handleMouseUp = useCallback(
    (e: React.MouseEvent) => {
      if (!interactive || !onInput) return

      const coords = translateCoordinates(e.clientX, e.clientY)
      const button = getMouseButton(e.button)

      onInput({
        event_type: 'mouseUp',
        mouse: {
          x: coords.x,
          y: coords.y,
          button,
        },
      })
    },
    [interactive, onInput, translateCoordinates]
  )

  const handleMouseMove = useCallback(
    (e: React.MouseEvent) => {
      if (!interactive || !onInput) return

      // Only send move events when button is pressed (drag)
      if (e.buttons === 0) return

      const coords = translateCoordinates(e.clientX, e.clientY)

      onInput({
        event_type: 'mouseMove',
        mouse: {
          x: coords.x,
          y: coords.y,
        },
      })
    },
    [interactive, onInput, translateCoordinates]
  )

  const handleWheel = useCallback(
    (e: React.WheelEvent) => {
      if (!interactive || !onInput) return

      e.preventDefault()
      const coords = translateCoordinates(e.clientX, e.clientY)

      onInput({
        event_type: 'mouseWheel',
        mouse: {
          x: coords.x,
          y: coords.y,
          delta_x: e.deltaX,
          delta_y: e.deltaY,
        },
      })
    },
    [interactive, onInput, translateCoordinates]
  )

  // Keyboard event handlers
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!interactive || !onInput) return

      // Prevent default for most keys to capture them
      if (!e.metaKey && !e.ctrlKey) {
        e.preventDefault()
      }

      const keyEvent: WSKeyboardEvent = {
        key: e.key,
        code: e.code,
        text: e.key.length === 1 ? e.key : undefined,
      }

      onInput({
        event_type: 'keyDown',
        keyboard: keyEvent,
      })
    },
    [interactive, onInput]
  )

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent) => {
      if (!interactive || !onInput) return

      onInput({
        event_type: 'keyUp',
        keyboard: {
          key: e.key,
          code: e.code,
        },
      })
    },
    [interactive, onInput]
  )

  return (
    <div
      ref={containerRef}
      className={`relative flex items-center justify-center bg-black ${className}`}
      style={{ width: width || '100%', height: height || '100%' }}
    >
      <canvas
        ref={canvasRef}
        style={{
          width: canvasSize.width || '100%',
          height: canvasSize.height || '100%',
        }}
        onMouseDown={handleMouseDown}
        onMouseUp={handleMouseUp}
        onMouseMove={handleMouseMove}
        onWheel={handleWheel}
        onKeyDown={handleKeyDown}
        onKeyUp={handleKeyUp}
        tabIndex={interactive ? 0 : -1}
        className={interactive ? 'cursor-pointer focus:outline-none' : ''}
      />
    </div>
  )
}

function getMouseButton(button: number): 'left' | 'right' | 'middle' {
  switch (button) {
    case 0:
      return 'left'
    case 1:
      return 'middle'
    case 2:
      return 'right'
    default:
      return 'left'
  }
}

export default StreamCanvas

// Input event forwarder for desktop streaming
// Captures keyboard and mouse events and forwards them via WebRTC data channel

import type { InputEvent, KeyboardInputEvent, MouseInputEvent, WheelInputEvent } from '@/types/stream'

export interface InputForwarderOptions {
  videoElement: HTMLVideoElement
  dataChannel: RTCDataChannel | null
  enabled?: boolean
  onInputSent?: (event: InputEvent) => void
}

export class InputForwarder {
  private videoElement: HTMLVideoElement
  private dataChannel: RTCDataChannel | null
  private enabled: boolean
  private onInputSent?: (event: InputEvent) => void
  private boundHandlers: {
    keydown: (e: KeyboardEvent) => void
    keyup: (e: KeyboardEvent) => void
    mousemove: (e: MouseEvent) => void
    mousedown: (e: MouseEvent) => void
    mouseup: (e: MouseEvent) => void
    wheel: (e: WheelEvent) => void
    contextmenu: (e: Event) => void
  }

  constructor(options: InputForwarderOptions) {
    this.videoElement = options.videoElement
    this.dataChannel = options.dataChannel
    this.enabled = options.enabled ?? true
    this.onInputSent = options.onInputSent

    // Bind handlers
    this.boundHandlers = {
      keydown: this.handleKeyDown.bind(this),
      keyup: this.handleKeyUp.bind(this),
      mousemove: this.handleMouseMove.bind(this),
      mousedown: this.handleMouseDown.bind(this),
      mouseup: this.handleMouseUp.bind(this),
      wheel: this.handleWheel.bind(this),
      contextmenu: this.handleContextMenu.bind(this),
    }
  }

  attach(): void {
    // Video element events (mouse)
    this.videoElement.addEventListener('mousemove', this.boundHandlers.mousemove)
    this.videoElement.addEventListener('mousedown', this.boundHandlers.mousedown)
    this.videoElement.addEventListener('mouseup', this.boundHandlers.mouseup)
    this.videoElement.addEventListener('wheel', this.boundHandlers.wheel, { passive: false })
    this.videoElement.addEventListener('contextmenu', this.boundHandlers.contextmenu)

    // Document events (keyboard) - need to focus the video element first
    document.addEventListener('keydown', this.boundHandlers.keydown)
    document.addEventListener('keyup', this.boundHandlers.keyup)

    // Make video focusable
    this.videoElement.tabIndex = 0
  }

  detach(): void {
    this.videoElement.removeEventListener('mousemove', this.boundHandlers.mousemove)
    this.videoElement.removeEventListener('mousedown', this.boundHandlers.mousedown)
    this.videoElement.removeEventListener('mouseup', this.boundHandlers.mouseup)
    this.videoElement.removeEventListener('wheel', this.boundHandlers.wheel)
    this.videoElement.removeEventListener('contextmenu', this.boundHandlers.contextmenu)

    document.removeEventListener('keydown', this.boundHandlers.keydown)
    document.removeEventListener('keyup', this.boundHandlers.keyup)
  }

  setDataChannel(channel: RTCDataChannel | null): void {
    this.dataChannel = channel
  }

  setEnabled(enabled: boolean): void {
    this.enabled = enabled
  }

  private sendInput(event: InputEvent): void {
    if (!this.enabled || !this.dataChannel || this.dataChannel.readyState !== 'open') {
      return
    }

    try {
      this.dataChannel.send(JSON.stringify(event))
      this.onInputSent?.(event)
    } catch (error) {
      console.error('Failed to send input event:', error)
    }
  }

  private handleKeyDown(e: KeyboardEvent): void {
    // Only forward if video element or its container is focused
    if (!this.isVideoFocused()) return

    e.preventDefault()
    e.stopPropagation()

    const event: KeyboardInputEvent = {
      type: 'keydown',
      key: e.key,
      code: e.code,
      modifiers: {
        ctrl: e.ctrlKey,
        alt: e.altKey,
        shift: e.shiftKey,
        meta: e.metaKey,
      },
    }

    this.sendInput(event)
  }

  private handleKeyUp(e: KeyboardEvent): void {
    if (!this.isVideoFocused()) return

    e.preventDefault()
    e.stopPropagation()

    const event: KeyboardInputEvent = {
      type: 'keyup',
      key: e.key,
      code: e.code,
      modifiers: {
        ctrl: e.ctrlKey,
        alt: e.altKey,
        shift: e.shiftKey,
        meta: e.metaKey,
      },
    }

    this.sendInput(event)
  }

  private handleMouseMove(e: MouseEvent): void {
    const coords = this.getRelativeCoordinates(e)
    if (!coords) return

    const event: MouseInputEvent = {
      type: 'mousemove',
      ...coords,
    }

    this.sendInput(event)
  }

  private handleMouseDown(e: MouseEvent): void {
    // Focus video on click
    this.videoElement.focus()

    const coords = this.getRelativeCoordinates(e)
    if (!coords) return

    e.preventDefault()

    const event: MouseInputEvent = {
      type: 'mousedown',
      ...coords,
      button: e.button,
    }

    this.sendInput(event)
  }

  private handleMouseUp(e: MouseEvent): void {
    const coords = this.getRelativeCoordinates(e)
    if (!coords) return

    const event: MouseInputEvent = {
      type: 'mouseup',
      ...coords,
      button: e.button,
    }

    this.sendInput(event)
  }

  private handleWheel(e: WheelEvent): void {
    e.preventDefault()

    const coords = this.getRelativeCoordinates(e)
    if (!coords) return

    const event: WheelInputEvent = {
      type: 'wheel',
      ...coords,
      deltaX: e.deltaX,
      deltaY: e.deltaY,
    }

    this.sendInput(event)
  }

  private handleContextMenu(e: Event): void {
    // Prevent context menu on video
    e.preventDefault()
  }

  private isVideoFocused(): boolean {
    return (
      document.activeElement === this.videoElement ||
      this.videoElement.contains(document.activeElement)
    )
  }

  private getRelativeCoordinates(e: MouseEvent): { x: number; y: number } | null {
    const rect = this.videoElement.getBoundingClientRect()

    // Get the displayed video dimensions
    const videoWidth = this.videoElement.videoWidth
    const videoHeight = this.videoElement.videoHeight

    if (videoWidth === 0 || videoHeight === 0) {
      return null
    }

    // Calculate the scaling factor (video might be scaled to fit)
    const scaleX = videoWidth / rect.width
    const scaleY = videoHeight / rect.height

    // Calculate relative position in video coordinates
    const x = Math.round((e.clientX - rect.left) * scaleX)
    const y = Math.round((e.clientY - rect.top) * scaleY)

    // Clamp to video bounds
    return {
      x: Math.max(0, Math.min(x, videoWidth)),
      y: Math.max(0, Math.min(y, videoHeight)),
    }
  }
}

// Helper to check if pointer lock is available
export function isPointerLockSupported(): boolean {
  return 'pointerLockElement' in document
}

// Request pointer lock on an element
export function requestPointerLock(element: HTMLElement): Promise<void> {
  return new Promise((resolve, reject) => {
    const onLockChange = () => {
      if (document.pointerLockElement === element) {
        document.removeEventListener('pointerlockchange', onLockChange)
        resolve()
      }
    }

    const onLockError = () => {
      document.removeEventListener('pointerlockerror', onLockError)
      reject(new Error('Pointer lock request failed'))
    }

    document.addEventListener('pointerlockchange', onLockChange)
    document.addEventListener('pointerlockerror', onLockError)

    element.requestPointerLock()
  })
}

// Exit pointer lock
export function exitPointerLock(): void {
  if (document.pointerLockElement) {
    document.exitPointerLock()
  }
}

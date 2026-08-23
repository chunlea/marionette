import type { AndroidDevice } from '@/types/stream'

interface DeviceSelectorProps {
  devices: AndroidDevice[]
  selectedSerial?: string
  onSelect: (device: AndroidDevice) => void
  isLoading?: boolean
  disabled?: boolean
}

export function DeviceSelector({
  devices,
  selectedSerial,
  onSelect,
  isLoading = false,
  disabled = false,
}: DeviceSelectorProps) {
  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-gray-400">
        <div className="w-4 h-4 border-2 border-gray-400 border-t-transparent rounded-full animate-spin" />
        <span>Loading devices...</span>
      </div>
    )
  }

  if (devices.length === 0) {
    return (
      <div className="text-gray-400">
        No devices connected
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <label htmlFor="device-select" className="text-sm font-medium text-gray-300">
        Select Device
      </label>
      <select
        id="device-select"
        value={selectedSerial || ''}
        onChange={(e) => {
          const device = devices.find((d) => d.serial === e.target.value)
          if (device) {
            onSelect(device)
          }
        }}
        disabled={disabled}
        className="
          px-3 py-2
          bg-gray-700 border border-gray-600
          rounded-md
          text-white
          focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent
          disabled:opacity-50 disabled:cursor-not-allowed
        "
      >
        <option value="" disabled>
          Choose a device...
        </option>
        {devices.map((device) => (
          <option key={device.serial} value={device.serial}>
            {device.model || device.serial}
            {device.state !== 'device' && ` (${device.state})`}
          </option>
        ))}
      </select>

      {/* Device info */}
      {selectedSerial && (
        <div className="text-xs text-gray-500">
          {(() => {
            const device = devices.find((d) => d.serial === selectedSerial)
            if (!device) return null
            return (
              <span>
                Serial: {device.serial}
                {device.product && ` | Product: ${device.product}`}
              </span>
            )
          })()}
        </div>
      )}
    </div>
  )
}

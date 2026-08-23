// Build-time feature flags.
//
// The streaming subsystem — desktop WebRTC and Android device mirroring — is
// frozen (decision D1). Its server side is gated behind configuration, its
// Android endpoints were never implemented at all, and its WebRTC signalling
// route sits behind the admin API's basic auth, which a browser cannot supply
// on a WebSocket handshake. The UI stays compiled so unfreezing it is a config
// change rather than an archaeology exercise, but it is off by default: a
// deployed dashboard should not offer buttons that lead to a 404.
//
// Turn it on for development with VITE_ENABLE_STREAMING=true.
export const streamingEnabled = import.meta.env.VITE_ENABLE_STREAMING === 'true'

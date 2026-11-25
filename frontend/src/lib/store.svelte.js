import * as Go from '../../wailsjs/go/main/App.js'
import { EventsOn } from '../../wailsjs/runtime/runtime.js'

export { Go }

export const app = $state({
  page: 'vaults',
  status: { connected: false, pin: {}, soPin: {}, dkek: {}, agent: {} },
  devices: [],
  keyMode: 1,
  toasts: [],
  progress: null,
  logs: [],
  dialog: null,
  pinPrompt: null,
  openRequest: null,
  ports: [],
  flashPort: '',
  flashFirmware: '',
})

let toastId = 0
export function toast(msg, kind = 'ok') {
  const id = ++toastId
  app.toasts.push({ id, msg: String(msg).replace(/^Error: /, ''), kind })
  setTimeout(() => { app.toasts = app.toasts.filter((t) => t.id !== id) }, kind === 'err' ? 7000 : 3500)
}

export async function act(fn, ok) {
  try {
    const r = await fn()
    if (ok) toast(ok)
    return r === undefined || r === null ? true : r
  } catch (e) {
    const msg = String(e?.message ?? e)
    if (msg !== 'cancelled') toast(cleanError(msg), 'err')
    throw e
  } finally {
    finishProgress()
  }
}

function finishProgress() {
  if (!app.progress) return
  app.progress = { ...app.progress, done: 1, total: 1 }
  setTimeout(() => { app.progress = null }, 500)
}

export async function run(fn, ok) {
  try { return await act(fn, ok) } catch { return undefined }
}

export async function refreshStatus() {
  try {
    app.status = await Go.GetStatus()
    app.devices = (await Go.ListVaultDevices()) ?? []
    app.keyMode = (await Go.GetSettings())?.keyMode === 2 ? 2 : 1
    app.ports = (await Go.ListSerialPorts().catch(() => [])) ?? []
  } catch {}
}

export const shortPort = (p) => (p ?? '').replace(/^\/dev\/(cu\.|tty\.?)?/, '')

export const activeDevice = () => app.devices.find((d) => d.active)
export async function selectBoardByPort(port) {
  const dev = app.devices.find((d) => d.port === port)
  if (!dev || dev.active) return
  if (await run(() => Go.SetActiveVaultDevice(dev.id))) await refreshStatus()
}

export function ask({ title, message, confirmText = 'Confirm', danger = false, typeToConfirm = '' }) {
  return new Promise((resolve) => {
    app.dialog = { title, message, confirmText, danger, typeToConfirm, resolve }
  })
}

export function promptText(title, value = '', message = '', { secret = false, confirmText = 'Save' } = {}) {
  return new Promise((resolve) => {
    app.dialog = { title, message, confirmText, input: value, secret, resolve }
  })
}

export function fmtBytes(n) {
  if (!n && n !== 0) return ''
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(1) + ' MB'
}

let started = false
export function startEvents() {
  if (started) return
  started = true
  EventsOn('log', (e) => {
    app.logs.push(e)
    if (app.logs.length > 1000) app.logs.splice(0, app.logs.length - 1000)
  })
  EventsOn('progress', (p) => { app.progress = p })
  EventsOn('device:changed', () => refreshStatus())
  EventsOn('devices:changed', () => refreshStatus())
  EventsOn('vault:open-file', (path) => { app.page = 'vaults'; app.openRequest = path })
  EventsOn('app:navigate', (page) => { if (page) app.page = page })
  Go.TakePendingVaultOpen().then((p) => { if (p) { app.page = 'vaults'; app.openRequest = p } })
  EventsOn('agent:needpin', (k) => {
    app.pinPrompt = { reason: `SSH wants to use key ${k}. Unlock your ESP Crypt.` }
  })
  EventsOn('agent:needdevice', (e) => {
    app.pinPrompt = { reason: `SSH wants to use key ${e.comment}. 2-ESP mode needs ${e.label} unlocked too.`, deviceId: e.deviceId, label: e.label }
  })
}

function requireUnlock(reason = '') {
  if (app.status.pin?.verified) return Promise.resolve(true)
  return new Promise((resolve) => { app.pinPrompt = { reason, resolve } })
}

function requireDeviceUnlock(dev, reason = '') {
  if (dev.unlocked) return Promise.resolve(true)
  if (dev.active) return requireUnlock(reason)
  return new Promise((resolve) => { app.pinPrompt = { reason: reason.charAt(0).toUpperCase() + reason.slice(1), deviceId: dev.id, label: dev.label, resolve } })
}

function cleanError(msg) {
  return String(msg).replace(/^(VAULT_DEVICE_MISSING|VAULT_LOCKED|VAULT_DEVICE_MISMATCH|PASSWORD_REQUIRED|VAULT_DEVICE_LOCKED\[[^\]]*\]): /, '')
}

export async function withVaultPIN(fn, ok, reason = 'Unlock your ESP Crypt to continue.', attempts = 3) {
  try {
    const r = await fn()
    if (ok) toast(ok)
    return r ?? true
  } catch (e) {
    const msg = String(e?.message ?? e)
    if (msg.startsWith('VAULT_LOCKED') && attempts > 0) {
      if (await requireUnlock(reason)) return withVaultPIN(fn, ok, reason, attempts - 1)
      return undefined
    }
    const locked = msg.match(/^VAULT_DEVICE_LOCKED\[([^\]]*)\]: (.*)$/)
    if (locked && attempts > 0) {
      await refreshStatus()
      const dev = app.devices.find((d) => d.key === locked[1])
      if (dev && (await requireDeviceUnlock(dev, locked[2]))) return withVaultPIN(fn, ok, reason, attempts - 1)
      if (!dev) toast(cleanError(msg), 'err')
      return undefined
    }
    if (msg !== 'cancelled' && !msg.startsWith('PASSWORD_REQUIRED')) toast(cleanError(msg), 'err')
    if (msg.startsWith('PASSWORD_REQUIRED')) throw e
    return undefined
  } finally {
    finishProgress()
  }
}

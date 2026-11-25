<script>
  import { onMount } from 'svelte'
  import { app, Go, run, ask, activeDevice, selectBoardByPort } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'
  import StorageBar from '../lib/StorageBar.svelte'

  let devices = $state([])
  let port = $state('')
  let creds = $state([])
  let loaded = $state(false)
  let busy = $state('')
  let pin = $state({ old: '', first: '', repeat: '' })
  let dev = $derived(devices.find((d) => d.port === port))
  let st = $derived(dev?.status)

  async function loadDevices() {
    devices = (await Go.ListPasskeyDevices().catch(() => [])) ?? []
    if (!devices.some((d) => d.port === port)) port = preferredPort()
    loaded = true
  }
  function preferredPort() {
    const a = activeDevice()?.port
    return devices.some((d) => d.port === a) ? a : devices[0]?.port ?? ''
  }
  let lastActive = ''
  $effect(() => {
    const a = activeDevice()?.port ?? ''
    if (a !== lastActive) {
      lastActive = a
      if (devices.some((d) => d.port === a)) port = a
    }
  })
  function pick(e) {
    port = e.currentTarget.value
    selectBoardByPort(port)
  }

  async function loadCreds() {
    creds = port && !dev?.error ? ((await Go.ListPasskeys(port).catch(() => [])) ?? []) : []
    creds.sort((a, b) => b.created - a.created)
  }
  onMount(() => {
    loadDevices()
    const t = setInterval(loadDevices, 4000)
    return () => clearInterval(t)
  })
  $effect(() => { port; st?.credentials; loadCreds() })

  async function withBusy(name, fn, ok) {
    busy = name
    const r = await run(fn, ok)
    busy = ''
    await loadDevices()
    await loadCreds()
    return r
  }

  async function del(c) {
    if (!(await ask({ title: `Delete passkey for ${c.rpId}?`, danger: true, confirmText: 'Delete passkey',
      message: `${c.userName || c.displayName || 'This account'} on ${c.rpId} can no longer sign in with this security key. The site may still list the key; remove it there too.` }))) return
    await withBusy('del', () => Go.DeletePasskey(port, c.id), 'Passkey deleted')
  }

  async function savePIN(e) {
    e.preventDefault()
    if (pin.first !== pin.repeat) return run(() => Promise.reject('The PINs do not match'))
    const ok = await withBusy('pin', () => (st.pinSet ? Go.ChangePasskeyPIN(port, pin.old, pin.first) : Go.SetPasskeyPIN(port, pin.first)), st.pinSet ? 'PIN changed' : 'PIN set')
    if (ok) pin = { old: '', first: '', repeat: '' }
  }

  async function toggleTouch() {
    const mode = st.touch === 'button' ? 'auto' : 'button'
    await withBusy('touch', () => Go.SetPasskeyTouch(port, mode), mode === 'auto' ? 'Button press is now optional' : 'Button press required')
  }

  async function reset() {
    if (!(await ask({ title: 'Erase all passkeys?', danger: true, confirmText: 'Erase, then press BOOT', typeToConfirm: 'RESET',
      message: 'Every passkey and the PIN on this key are erased for good. Accounts that use it for sign-in or 2FA need another method. Press the BOOT button on the board within 30 seconds to confirm.' }))) return
    await withBusy('reset', () => Go.ResetPasskeyDevice(port), 'Security key erased')
  }

  const portLabel = (p) => (p ?? '').replace(/^(wifi|ble):\/\//, '')
  const fmtId = (d) => d.status?.deviceId ? d.status.deviceId.slice(-6) : d.port
</script>

{#if !loaded}
  <div class="card text-sm text-neutral-400">Looking for security keys…</div>
{:else if !devices.length}
  <section class="card flex flex-col items-center py-12 text-center">
    <div class="mb-4 grid h-16 w-16 place-items-center rounded-3xl bg-violet-500/10 text-violet-300"><Icon name="key" class="h-8 w-8" /></div>
    <div class="text-lg font-semibold">No ESP32 security key connected</div>
    <p class="mt-2 max-w-lg text-sm text-neutral-400">Install the Passkey or Vault + Passkey firmware on an ESP32-S3 (for example an ESP32-S3-Zero or XIAO ESP32-S3). It becomes a FIDO2 security key that Windows, macOS and browsers use on their own for passkeys and two-factor sign-in. Confirm each sign-in with the BOOT button.</p>
    <button class="btn btn-primary mt-5" onclick={() => { app.flashFirmware = 'passkey'; app.page = 'esp32' }}><Icon name="bolt" /> Install Passkey firmware</button>
  </section>
{:else}
  <section class="card mb-5">
    <div class="flex flex-wrap items-center gap-4">
      <div class="grid h-12 w-12 shrink-0 place-items-center rounded-2xl bg-violet-500/15 text-violet-300"><Icon name="key" class="h-6 w-6" /></div>
      <div class="min-w-56 flex-1">
        <div class="font-medium">{dev?.error ? `Security key on ${port}` : `ESP32-S3 security key · ${fmtId(dev)}${st.combined ? ' · also an ESP Crypt' : ''}`}</div>
        <div class="text-xs text-neutral-400">
          {#if dev?.error}<span class="text-amber-300">{dev.error}</span>{:else}{st.combined ? 'Vault + Passkey firmware' : `Passkey firmware ${st.version}`} · {port} · {st.credentials} of {st.maxCredentials} passkeys · PIN {st.pinSet ? `set (${st.pinRetries} tries left)` : 'not set'}{/if}
        </div>
      </div>
      {#if devices.length > 1}
        <select class="input w-56" value={port} onchange={pick} aria-label="Security key">
          {#each devices as d (d.port)}<option value={d.port}>{fmtId(d)}</option>{/each}
        </select>
      {/if}
      <button class="btn btn-sm" disabled={!!dev?.error} onclick={() => run(() => Go.WinkPasskeyDevice(port))}><Icon name="bolt" /> Blink LED</button>
    </div>
  </section>

  {#if st && !dev.error}
    <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_380px]">
      <section class="card min-w-0">
        <div class="card-title"><Icon name="key" /> Passkeys on this key</div>
        <div class="divide-y divide-neutral-700/50">
          {#each creds as c (c.id)}
            <div class="flex items-center gap-4 py-3">
              <div class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-neutral-700/60 text-violet-300"><Icon name="key" /></div>
              <div class="min-w-0 flex-1">
                <div class="truncate text-sm font-medium">{c.rpId}</div>
                <div class="truncate text-xs text-neutral-500">{c.displayName || c.userName || 'account'}{c.userName && c.displayName && c.userName !== c.displayName ? ` · ${c.userName}` : ''}</div>
              </div>
              <button class="btn btn-sm btn-ghost text-rose-300" title="Delete" disabled={!!busy} onclick={() => del(c)}><Icon name="trash" /></button>
            </div>
          {:else}
            <p class="py-6 text-center text-sm text-neutral-500">No passkeys yet. Create one on a site that supports passkeys (choose "security key" or "USB key"); it appears here.</p>
          {/each}
        </div>
        <p class="hint mt-3">Only passkeys (discoverable credentials) are listed. Keys used as a second factor next to a password are not stored on the key and work without being listed.</p>
      </section>

      <aside class="space-y-5">
        <form class="card space-y-3" onsubmit={savePIN}>
          <div class="card-title"><Icon name="shield" /> {st.pinSet ? 'Change PIN' : 'Set a PIN'}</div>
          <p class="text-xs text-neutral-400">{st.pinSet ? 'Sites and Windows ask for this PIN before using the key.' : 'Passkeys need a PIN: Windows and most sites ask for one. Without a PIN, anyone with the key and a button press can sign in.'}</p>
          {#if st.pinSet}<input class="input" type="password" autocomplete="off" placeholder="Current PIN" bind:value={pin.old} />{/if}
          <input class="input" type="password" autocomplete="off" placeholder="New PIN (4–63 characters)" bind:value={pin.first} />
          <input class="input" type="password" autocomplete="off" placeholder="Repeat new PIN" bind:value={pin.repeat} />
          <button class="btn btn-primary" disabled={!!busy || pin.first.length < 4 || (st.pinSet && !pin.old)}>{busy === 'pin' ? 'Saving…' : st.pinSet ? 'Change PIN' : 'Set PIN'}</button>
          {#if st.pinSet && st.pinRetries <= 3}<p class="text-xs text-amber-300">{st.pinRetries} tries left before the key locks; a locked key can only be reset.</p>{/if}
        </form>

        <div class="card">
          <div class="card-title"><Icon name="bolt" /> Confirmation</div>
          <button type="button" role="switch" aria-checked={st.touch === 'button'} class="flex w-full items-center gap-3 text-left" disabled={!!busy} onclick={toggleTouch}>
            <span class="relative h-6 w-11 shrink-0 rounded-full transition {st.touch === 'button' ? 'bg-violet-500' : 'bg-neutral-600'}">
              <span class="absolute top-0.5 h-5 w-5 rounded-full bg-white transition-all {st.touch === 'button' ? 'left-[22px]' : 'left-0.5'}"></span>
            </span>
            <span class="text-sm"><span class="block font-medium">Press BOOT to sign in</span><span class="text-xs text-neutral-400">{st.touch === 'button' ? 'On: every sign-in waits for the button' : 'Off: sign-ins go through without a press'}</span></span>
          </button>
          <p class="hint mt-3">{st.touch === 'button' ? `The LED flashes white while the key waits for the button${st.rgbLed ? '' : ' (a plain LED blinks)'}.` : 'Turn it off for boards without a BOOT button. The PIN, if set, is still asked; anyone with the key and the PIN can sign in without touching it.'}</p>
        </div>

        <div class="card border-rose-500/20">
          <div class="card-title text-rose-200"><Icon name="trash" /> Reset</div>
          <p class="mb-3 text-xs text-neutral-400">Erases every passkey and the PIN. Use it when the PIN is locked or before giving the key away.</p>
          <button class="btn btn-sm text-rose-300" disabled={!!busy} onclick={reset}>{busy === 'reset' ? 'Press BOOT on the board…' : 'Erase security key'}</button>
        </div>

        <div class="card">
          <div class="card-title"><Icon name="database" /> Storage</div>
          <StorageBar storage={st.storage} items={[{ label: 'Passkeys', used: st.credentials, max: st.maxCredentials }]} />
          <p class="hint mt-3">A passkey takes about {Math.ceil((st.credentialBytes || 400) / 32) * 32 + 64} bytes.{st.combined ? ' This board also runs the Vault: keyfiles and SSH keys use the same storage.' : ''} Keys used only as a second factor take no storage.</p>
        </div>

        <div class="card text-xs text-neutral-400">
          <div class="mb-1 font-medium text-neutral-200">Good to know</div>
          The private keys are derived on the chip and never leave it. Like the Vault firmware, this reflashable profile does not encrypt the flash with eFuses: someone who takes the board and reads its flash could copy the keys. Keep the key with you, and set a PIN.
        </div>
      </aside>
    </div>
  {/if}
{/if}

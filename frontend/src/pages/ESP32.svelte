<script>
  import { onMount } from 'svelte'
  import { app, Go, run, ask, refreshStatus, selectBoardByPort, shortPort } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let ports = $state([])
  let opt = $state({ port: '', reset: 'auto', baud: 460800 })
  let chip = $state(null)
  let busy = $state('')
  let bundled = $state([])
  let customFactory = $state('')
  let customOta = $state('')
  let showAdvanced = $state(false)
  let firmware = $state('vault')
  let passkeyTouch = $state(true)
  let st = $derived(app.status)
  let selected = $derived(ports.find((p) => p.name === opt.port))
  let noPort = $derived(!opt.port || !!busy)
  let factoryBlocked = $derived(!!chip && (!chip.securityInfoAvailable || chip.secureBoot || chip.flashEncrypted))
  let registered = $derived(app.devices.find((device) => device.port === opt.port))
  let activeDevice = $derived(app.devices.find((d) => d.active))
  let activeBundle = $derived(bundled.find((b) => b.firmware === 'vault' && b.chip === activeDevice?.chip))
  let fwBundles = $derived(bundled.filter((b) => b.firmware === firmware))
  let chipBundle = $derived(chip ? fwBundles.find((b) => b.chip === chip.chip) : null)
  let passkeyBlocked = $derived(firmware !== 'vault' && chip && chip.chip !== 'ESP32-S3')
  let fwName = $derived(firmware === 'passkey' ? 'Passkey' : firmware === 'vaultkey' ? 'Vault + Passkey' : 'Vault')
  const fileName = (p) => p.split(/[\\/]/).pop()
  const describe = (b) => `${b.chip}${b.version ? ' · ' + b.version : ''}${b.built ? ' · ' + b.built : ''}`

  let missingSince = 0
  async function loadPorts() {
    const list = (await Go.ListSerialPorts().catch(() => [])) ?? []
    const gone = opt.port && !list.some((p) => p.name === opt.port)
    if (gone && (busy || Date.now() - (missingSince ||= Date.now()) < 6000)) {
      const keep = ports.find((p) => p.name === opt.port)
      ports = keep ? [...list, keep] : list
      return
    }
    if (!gone) missingSince = 0
    ports = list
    if (busy) return
    if (app.flashPort && ports.some((p) => p.name === app.flashPort)) {
      if (opt.port !== app.flashPort) opt.port = app.flashPort
      app.flashPort = ''
    }
    const fresh = ports.find((p) => p.likely && !p.passkey && !app.devices.some((d) => d.port === p.name))
    const active = activeDevice?.port
    const activeListed = ports.some((p) => p.name === active) ? active : ''
    if (!opt.port || !ports.some((p) => p.name === opt.port)) opt.port = fresh?.name || activeListed || ports.find((p) => p.likely)?.name || ''
  }
  let lastActive = ''
  $effect(() => {
    const a = activeDevice?.port ?? ''
    if (a !== lastActive) {
      lastActive = a
      if (a && !busy && ports.some((p) => p.name === a)) opt.port = a
    }
  })
  function pickPort(e) {
    opt.port = e.currentTarget.value
    selectBoardByPort(opt.port)
  }
  let chipPort = ''
  $effect(() => { if (opt.port !== chipPort) chip = null })
  onMount(() => {
    if (app.flashFirmware) { firmware = app.flashFirmware; app.flashFirmware = '' }
    loadPorts()
    Go.ListBundledFirmware().then((b) => (bundled = b ?? [])).catch(() => {})
    const t = setInterval(loadPorts, 2500)
    return () => clearInterval(t)
  })

  async function withBusy(name, fn, ok) {
    busy = name
    const r = await run(fn, ok)
    busy = ''
    return r
  }

  async function identify() {
    const port = opt.port
    const result = await withBusy('identify', () => Go.ESPChipInfo($state.snapshot(opt)))
    if (result && result !== true && opt.port === port) { chipPort = port; chip = result }
  }

  async function chooseFirmware(kind) {
    const path = await run(() => Go.PickFirmwareFile())
    if (path && path !== true) {
      if (kind === 'factory') customFactory = path
      else customOta = path
    }
  }

  async function connect() {
    const action = registered
      ? () => Go.SetActiveVaultDevice(registered.id)
      : () => Go.ConnectVaultDevice(opt.port)
    const message = registered ? 'Active ESP Crypt selected' : 'ESP Crypt registered'
    if (await withBusy('connect', action, message)) await refreshStatus()
  }

  async function flashFactory() {
    const what = customFactory ? `The custom image ${fileName(customFactory)}` : `The latest ${fwName} firmware`
    const hasData = registered || selected?.passkey
    const erases = registered
      ? `${opt.port} already runs Vault firmware (${registered.label}). Its keyfiles, SSH keys and passphrase are erased for good.`
      : selected?.passkey
        ? `${opt.port} runs the Passkey firmware. Its passkeys and PIN are erased for good; sites that use them will no longer accept this key.`
        : 'Everything on the board is erased.'
    const role = firmware === 'passkey'
      ? 'Afterwards the board is a FIDO2 security key that Windows, macOS and browsers recognise on their own.'
      : firmware === 'vaultkey'
        ? 'Afterwards the board is both an ESP Crypt for this app and a FIDO2 security key for Windows, macOS and browsers.'
        : 'Afterwards the board connects to this app as an ESP Crypt.'
    const confirmed = await ask({ title: `Install ${fwName} firmware on ${opt.port}`, danger: true, confirmText: 'Erase and install', typeToConfirm: hasData ? 'ERASE FLASH' : '',
      message: `${what} is installed on the ESP32 on ${opt.port}. ${erases} ${role} No eFuses are programmed, so the board stays reflashable.` })
    if (!confirmed) return
    const ok = await withBusy('factory', () => Go.FlashFirmware({ ...$state.snapshot(opt), firmware, file: customFactory }),
      firmware === 'passkey' ? 'Passkey firmware installed; the board restarts as a security key' : `${fwName} firmware installed; the board restarts and connects`)
    if (ok) {
      chip = null
      if (firmware !== 'vault' && !passkeyTouch) applyTouchDefault()
    }
    await refreshStatus()
  }

  async function applyTouchDefault() {
    for (let i = 0; i < 20; i++) {
      await new Promise((r) => setTimeout(r, 1500))
      const keys = (await Go.ListPasskeyDevices().catch(() => [])) ?? []
      const fresh = keys.find((k) => !k.error && k.status?.credentials === 0 && k.status?.touch === 'button')
      if (fresh) {
        await run(() => Go.SetPasskeyTouch(fresh.port, 'auto'), 'Security key ready: button press is optional')
        return
      }
    }
  }

  async function updateFirmware() {
    const what = customOta ? fileName(customOta) : `the latest firmware${activeBundle?.version ? ' ' + activeBundle.version : ''}`
    if (!(await ask({ title: `Update ${activeDevice?.label ?? 'ESP Crypt'}`, confirmText: 'Update',
      message: `Install ${what} on the active ESP Crypt. Keyfiles, SSH keys and the passphrase are kept. The board checks the signature, restarts and reconnects; open vaults are locked first.` }))) return
    await withBusy('ota', () => Go.UpdateVaultFirmware(customOta), 'Update accepted; the board is restarting')
    await refreshStatus()
  }
</script>

<section class="card mb-5">
  <div class="flex flex-wrap items-center gap-4">
    <div class="grid h-11 w-11 place-items-center rounded-xl {selected?.likely ? 'bg-violet-500/15 text-violet-300' : 'bg-neutral-700/60 text-neutral-400'}"><Icon name="usb" class="h-5 w-5" /></div>
    <div class="min-w-56 flex-1">
      <div class="font-medium">{#if registered}{registered.chip} · {registered.serial ? registered.serial.slice(-6) : registered.port} runs Vault firmware{:else if selected?.passkey}ESP32-S3 on {shortPort(selected.name)} runs Passkey firmware{:else if selected}ESP32 on {shortPort(selected.name)} · no firmware from this app yet{:else}No ESP32 connected{/if}</div>
      <div class="text-xs text-neutral-400">{#if selected}{selected.kind}{:else}Plug in an ESP32-C3, ESP32-C6 or ESP32-S3 board by USB.{/if}{#if chip} · {chip.chip}, {chip.flashMb} MB flash{#if chip.securityInfoAvailable} · Secure Boot {chip.secureBoot ? 'on' : 'off'}, flash encryption {chip.flashEncrypted ? 'on' : 'off'}{/if}{/if}</div>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <select class="input w-60" value={opt.port} onchange={pickPort} aria-label="Serial port" title={opt.port}>
        <option value="">{ports.length ? 'Choose port' : 'No ports found'}</option>
        {#each ports as p}<option value={p.name} title={p.name}>{shortPort(p.name)} · {app.devices.find((d) => d.port === p.name)?.label ?? p.kind}</option>{/each}
      </select>
      <button class="btn btn-sm" disabled={noPort} onclick={identify}><Icon name="chip" /> {busy === 'identify' ? 'Reading…' : 'Identify'}</button>
      {#if registered}<button class="btn btn-sm" disabled={noPort || registered.active} onclick={connect}>{registered.active ? 'Active board' : 'Make active'}</button>{/if}
    </div>
  </div>
</section>

<div class="grid gap-5 xl:grid-cols-2">
  <section class="card">
    <div class="card-title"><Icon name="bolt" /> Install firmware</div>
    <div class="mb-4 grid gap-2 sm:grid-cols-3">
      <button type="button" class="rounded-xl border p-3 text-left transition {firmware === 'vault' ? 'border-violet-500/60 bg-violet-500/10' : 'border-neutral-700/50 hover:border-neutral-500/60'}" onclick={() => (firmware = 'vault')}>
        <div class="flex items-center gap-2 text-sm font-medium"><Icon name="lock" class="h-4 w-4 text-violet-300" /> Vault</div>
        <p class="mt-1 text-xs text-neutral-400">VeraCrypt keyfiles and SSH keys with this app. ESP32-C3, ESP32-C6 and ESP32-S3.</p>
      </button>
      <button type="button" class="rounded-xl border p-3 text-left transition {firmware === 'passkey' ? 'border-violet-500/60 bg-violet-500/10' : 'border-neutral-700/50 hover:border-neutral-500/60'}" onclick={() => (firmware = 'passkey')}>
        <div class="flex items-center gap-2 text-sm font-medium"><Icon name="key" class="h-4 w-4 text-violet-300" /> Passkey</div>
        <p class="mt-1 text-xs text-neutral-400">FIDO2 security key for passkeys and 2FA, used natively by Windows, macOS and browsers. ESP32-S3 only.</p>
      </button>
      <button type="button" class="rounded-xl border p-3 text-left transition {firmware === 'vaultkey' ? 'border-violet-500/60 bg-violet-500/10' : 'border-neutral-700/50 hover:border-neutral-500/60'}" onclick={() => (firmware = 'vaultkey')}>
        <div class="flex items-center gap-2 text-sm font-medium"><Icon name="lock" class="h-4 w-4 text-violet-300" /><Icon name="key" class="-ml-1 h-4 w-4 text-violet-300" /> Vault + Passkey</div>
        <p class="mt-1 text-xs text-neutral-400">Both on one board: the Vault and a USB security key share its flash storage. Needs USB OTG: ESP32-S3 only.</p>
      </button>
    </div>
    {#if firmware !== 'vault'}
      <label class="check mb-4"><input type="checkbox" class="shrink-0" bind:checked={passkeyTouch} /> <span>Require a BOOT button press for every sign-in <span class="text-neutral-500">(turn off for boards without a button; changeable later on the Passkeys page)</span></span></label>
    {/if}
    <p class="mb-4 text-sm text-neutral-400">A board runs one firmware at a time; switching erases it. The app installs the latest firmware and detects the chip type automatically.</p>
    <div class="mb-4 rounded-lg bg-neutral-900/60 p-3 text-xs text-neutral-400">
      {#if customFactory}
        <div class="flex flex-wrap items-center justify-between gap-2"><span class="truncate" title={customFactory}>Custom image: <span class="text-neutral-200">{fileName(customFactory)}</span></span><button class="btn btn-sm btn-ghost" onclick={() => (customFactory = '')}>Use latest</button></div>
      {:else if fwBundles.length}
        <div class="mb-1 text-neutral-500">Latest {fwName} firmware</div>
        {#each fwBundles as b}<div class={chipBundle === b ? 'text-violet-300' : 'text-neutral-300'}>{describe(b)}</div>{/each}
      {:else}
        <div class="text-amber-300">The firmware could not be downloaded. Connect to the internet once, or choose an image under Advanced.</div>
      {/if}
    </div>
    <button class="btn btn-primary" disabled={noPort || factoryBlocked || passkeyBlocked || (!fwBundles.length && !customFactory) || (chip && !chipBundle && !customFactory)} onclick={flashFactory}><Icon name="bolt" /> {busy === 'factory' ? 'Installing…' : `Install ${fwName} firmware${opt.port ? ' on ' + shortPort(opt.port) : ''}`}</button>
    {#if passkeyBlocked}<p class="mt-2 text-sm text-amber-300">{chip.chip} has no USB OTG, so it cannot be a security key. The Passkey firmware needs an ESP32-S3 (for example ESP32-S3-Zero or XIAO ESP32-S3).</p>
    {:else if chip && !chipBundle && !customFactory}<p class="mt-2 text-sm text-amber-300">{chip.chip} is not supported; the Vault firmware runs on ESP32-C3, ESP32-C6 and ESP32-S3.</p>{/if}
    {#if factoryBlocked}<p class="mt-2 text-sm text-rose-300">This chip has hardware Secure Boot or flash encryption enabled (or its state could not be read), so it cannot be erased and reflashed here.</p>{/if}
    {#if registered}<p class="mt-2 text-sm text-amber-200">{shortPort(opt.port)} already runs Vault firmware. Installing erases its keyfiles and SSH keys{firmware === 'vault' ? '; use Update to keep them' : ''}.</p>
    {:else if selected?.passkey}<p class="mt-2 text-sm text-amber-200">{shortPort(opt.port)} runs the Passkey firmware. Installing erases its passkeys; the app restarts it into flash mode first.</p>{/if}
  </section>

  <section class="card">
    <div class="card-title"><Icon name="upload" /> Update Vault firmware</div>
    <p class="mb-4 text-sm text-neutral-400">Updates the active ESP Crypt to the firmware in this app. Keyfiles, SSH keys and the passphrase are kept.</p>
    <div class="mb-4 rounded-lg bg-neutral-900/60 p-3 text-xs text-neutral-400">
      {#if !activeDevice}
        Connect a board that runs Vault firmware and make it active.
      {:else if customOta}
        <div class="flex flex-wrap items-center justify-between gap-2"><span class="truncate" title={customOta}>Custom update: <span class="text-neutral-200">{fileName(customOta)}</span></span><button class="btn btn-sm btn-ghost" onclick={() => (customOta = '')}>Use latest</button></div>
      {:else if activeBundle}
        <div class="text-neutral-500">{activeDevice.label}{st.firmwareBuilt ? ` · runs firmware built ${st.firmwareBuilt}` : ' · runs older firmware'}</div><div class="text-neutral-300">Update to {describe(activeBundle)}</div>
      {:else}
        <span class="text-amber-300">No update available for {activeDevice.chip}.</span>
      {/if}
    </div>
    <button class="btn btn-primary" disabled={!st.connected || !!busy || (!activeBundle && !customOta)} onclick={updateFirmware}><Icon name="upload" /> {busy === 'ota' ? 'Updating…' : 'Update firmware'}</button>
  </section>
</div>

<section class="card mt-5">
  <button class="flex w-full items-center gap-2 text-left text-sm font-medium" onclick={() => (showAdvanced = !showAdvanced)}><Icon name="shield" /> Advanced {showAdvanced ? '▾' : '▸'}</button>
  {#if showAdvanced}
    <div class="mt-4 grid gap-4 md:grid-cols-2">
      <div>
        <div class="label">Custom factory image (*-vault-factory.bin)</div>
        <button class="btn btn-sm" onclick={() => chooseFirmware('factory')}>Choose file…</button>
      </div>
      <div>
        <div class="label">Custom signed update (*-vault-ota.bin)</div>
        <button class="btn btn-sm" onclick={() => chooseFirmware('ota')}>Choose file…</button>
      </div>
      <div>
        <label class="label" for="reset">Bootloader reset method</label>
        <select id="reset" class="input input-sm w-60" bind:value={opt.reset}>
          <option value="auto">Automatic</option><option value="usbjtag">Native USB</option><option value="classic">USB-UART DTR/RTS</option><option value="manual">Already in download mode</option>
        </select>
      </div>
      {#if !registered && selected}
        <div>
          <div class="label">Register this port by hand</div>
          <button class="btn btn-sm" disabled={noPort} onclick={connect}>Register board</button>
        </div>
      {/if}
    </div>
    <ul class="mt-4 list-disc space-y-1.5 pl-5 text-xs text-neutral-400">
      <li>No eFuses are programmed, and hardware Secure Boot and flash encryption stay off, so every board remains reflashable through ROM download mode.</li>
      <li>Updates are signed with an RSA-3072 key; the board refuses updates with another signature. A full install works regardless, but erases the board.</li>
    </ul>
  {/if}
</section>

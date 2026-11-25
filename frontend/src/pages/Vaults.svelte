<script>
  import { onMount } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime.js'
  import { app, Go, run, ask, promptText, withVaultPIN, refreshStatus, fmtBytes, toast } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let info = $state(null)
  let vaults = $state([])
  let keyfiles = $state([])
  let busyId = $state('')
  let installing = $state(false)
  let pin = $state('')
  let unlocking = $state(false)

  let showCreate = $state(false)
  let showAdvanced = $state(false)
  let usePassword = $state(false)
  let twoKey = $state(false)
  let secondId = $state('')
  let otherDevices = $derived(app.devices.filter((d) => !d.active))
  let secondDev = $derived(otherDevices.find((d) => d.id === secondId))
  const shortLabel = (d) => `${d.chip} · ${d.serial ? d.serial.slice(-6) : d.port}`
  let form = $state({ name: '', size: 1024, customSize: 2048, password: '', password2: '', path: '', encryption: 'AES', hash: 'sha512', filesystem: 'exFAT', pim: 0, fullFormat: false })
  const sizes = [[100, '100 MB'], [1024, '1 GB'], [5120, '5 GB'], [20480, '20 GB'], [0, 'Custom']]

  let chooser = $state(null)

  async function load() {
    info = await Go.VCInfo()
    vaults = (await Go.ListVaults()) ?? []
    keyfiles = app.status.connected ? ((await Go.ListDataObjects().catch(() => [])) ?? []) : []
  }
  onMount(() => {
    load()
    const off = EventsOn('vaults:changed', load)
    const t = setInterval(load, 10000)
    return () => { off(); clearInterval(t) }
  })
  let deviceState = $derived(app.devices.map((d) => `${d.id}:${d.unlocked}`).join(','))
  $effect(() => { app.status.connected; app.status.pin?.verified; deviceState; load() })

  let st = $derived(app.status)
  let sizeMB = $derived(form.size === 0 ? +form.customSize : form.size)

  async function unlockDevice(e) {
    e.preventDefault()
    unlocking = true
    await run(() => Go.Login(pin))
    pin = ''
    unlocking = false
    await refreshStatus()
    load()
  }

  async function install() {
    installing = true
    await run(() => Go.VCInstall(), 'Vault engine installed')
    installing = false
    load()
  }

  async function unlock(v) {
    let pw = ''
    if (v.hasPassword) {
      pw = await promptText(`Unlock ${v.name}`, '', 'This vault also has a password.', { secret: true, confirmText: 'Unlock' })
      if (pw === null) return
    }
    busyId = v.id
    await withVaultPIN(() => Go.UnlockVault(v.id, pw), `${v.name} is unlocked`, `Unlock your ESP Crypt to open ${v.name}.`).catch(() => {})
    busyId = ''
    load()
  }
  async function lock(v) {
    busyId = v.id
    await run(() => Go.LockVault(v.id), `${v.name} is locked`)
    busyId = ''
    load()
  }

  function openCreate() {
    form = { ...form, name: '', password: '', password2: '', path: '' }
    usePassword = false
    showAdvanced = false
    twoKey = app.keyMode === 2
    secondId = (app.devices.find((d) => d.second) ?? otherDevices[0])?.id ?? ''
    showCreate = true
  }
  async function pickLocation() {
    const p = await run(() => Go.PickVaultFile(true, form.name || 'Vault'))
    if (p && p !== true) form.path = p
  }
  async function create(e) {
    e.preventDefault()
    if (usePassword && form.password !== form.password2) return run(() => Promise.reject('The passwords do not match'))
    const req = { name: form.name, path: form.path, sizeMb: sizeMB, password: usePassword ? form.password : '',
      encryption: form.encryption, hash: form.hash, filesystem: form.filesystem, pim: +form.pim, fullFormat: form.fullFormat,
      keyCount: twoKey ? 2 : 1, secondDevice: twoKey ? secondId : '' }
    busyId = 'create'
    const r = await withVaultPIN(() => Go.CreateVault(req), `Vault "${form.name}" created and unlocked`, twoKey ? 'Unlock your active ESP Crypt to create the first keyfile.' : 'Unlock your ESP Crypt to create the vault keyfile.').catch(() => {})
    busyId = ''
    if (r) showCreate = false
    load()
  }

  async function openFile() {
    const p = await run(() => Go.PickVaultFile(false, ''))
    if (p && p !== true) await openRequested(p)
  }
  async function openRequested(path) {
    const list = (await Go.ListVaults()) ?? []
    const v = list.find((x) => x.path === path)
    if (v?.mounted) { Go.OpenPath(v.mountPoint); return }
    if (v) { await unlock(v); return }
    await tryOpen(path, '', -1)
  }
  async function tryOpen(path, pw, keyfileId) {
    busyId = 'open'
    const r = await withVaultPIN(() => Go.OpenVaultFile(path, pw, keyfileId, !!pw), null, 'Unlock your ESP Crypt to open the vault.').catch(() => undefined)
    busyId = ''
    load()
    if (r?.choices?.length) { chooser = { path, pw, choices: r.choices, pick: r.choices[0].id }; return }
    if (r?.vault) { toast(`${r.vault.name} is unlocked`); return }
    if (!pw && app.status.pin?.verified) {
      const again = await promptText('Does this vault have a password?', '', 'It could not be opened with the ESP Crypt keyfile alone. Enter its password to try again.', { secret: true, confirmText: 'Unlock' })
      if (again) await tryOpen(path, again, keyfileId)
    }
  }

  async function rename(v) {
    const n = await promptText('Rename vault', v.name)
    if (n && n !== v.name) { await run(() => Go.RenameVault(v.id, n)); load() }
  }
  async function forget(v) {
    if (!(await ask({ title: `Remove ${v.name} from the list?`, confirmText: 'Remove from list',
      message: 'The vault file and its keyfile on the ESP Crypt are kept. You can add it again later with "Add existing".' }))) return
    await run(() => Go.ForgetVault(v.id), 'Removed from list')
    load()
  }
  async function changePassword(v) {
    let old = ''
    if (v.hasPassword) {
      old = await promptText('Current password', '', `Enter the current password of ${v.name}.`, { secret: true, confirmText: 'Next' })
      if (old === null) return
    }
    const nw = await promptText(v.hasPassword ? 'New password' : 'Add a password', '', 'Leave empty to remove the password (the ESP Crypt keyfile still protects the vault).', { secret: true, confirmText: 'Next' })
    if (nw === null) return
    if (nw) {
      const again = await promptText('Repeat new password', '', '', { secret: true, confirmText: 'Change password' })
      if (again !== nw) return run(() => Promise.reject('The passwords do not match'))
    }
    busyId = v.id
    await withVaultPIN(() => Go.ChangeVaultPassword(v.id, old, nw), nw ? 'Password changed' : 'Password removed').catch(() => {})
    busyId = ''
    load()
  }

  let openCount = $derived(vaults.filter((v) => v.mounted).length)

  $effect(() => {
    const p = app.openRequest
    if (!p) return
    app.openRequest = null
    openRequested(p)
  })
</script>

{#if !info}
  <div class="card text-sm text-neutral-400">Loading…</div>
{:else}
  {#if !info.installed}
    <div class="card mb-5 flex items-center gap-5">
      <div class="grid h-12 w-12 shrink-0 place-items-center rounded-2xl bg-neutral-700/60"><Icon name="download" class="h-6 w-6" /></div>
      <div class="flex-1">
        <div class="font-medium">{info.broken ? 'Repair the vault engine' : 'One-time setup: install the vault engine'}</div>
        {#if info.broken}<p class="mt-1 text-sm text-amber-200">{info.brokenReason}. Repairing installs the FUSE-T edition, which needs no kernel extension.</p>{/if}
        <p class="mt-1 text-sm text-neutral-400">Vaults use VeraCrypt for encryption. The app downloads the official version and installs it silently{info.os === 'windows' ? ' (Windows asks for permission once)' : ' (FUSE-T edition, no kernel extension; macOS asks for your password once)'}. The installer's digital signature is checked first.</p>
      </div>
      <button class="btn btn-primary" disabled={installing} onclick={install}>{installing ? 'Installing…' : info.broken ? 'Repair' : 'Install'}</button>
    </div>
  {:else if info.fuse === 'macfuse'}
    <div class="callout callout-warn mb-5 flex items-center gap-4">
      <Icon name="disk" class="h-5 w-5 shrink-0" />
      <div class="flex-1 text-sm">Your VeraCrypt uses macFUSE. A wrong password then makes macOS show "VeraCrypt quit unexpectedly" (harmless). The FUSE-T edition doesn't have this problem.</div>
      <button class="btn btn-sm" disabled={installing} onclick={install}>{installing ? 'Installing…' : 'Switch to FUSE-T'}</button>
    </div>
  {/if}

  {#if !st.connected}
    <div class="callout callout-info mb-5 flex items-center gap-4">
      <Icon name="usb" class="h-5 w-5 shrink-0" />
      <div class="flex-1"><span class="font-medium">Connect your custom ESP Crypt.</span> Vaults can only be created or unlocked while the custom firmware is connected.</div>
      <button class="btn btn-sm" onclick={() => (app.page = 'dashboard')}>Setup</button>
    </div>
  {:else if st.pin?.notInitialized}
    <div class="callout callout-warn mb-5 flex items-center gap-4">
      <Icon name="shield" class="h-5 w-5 shrink-0" />
      <div class="flex-1"><span class="font-medium">Vault storage or passphrase is not set up yet.</span> Finish the steps on the Security page first.</div>
      <button class="btn btn-sm btn-primary" onclick={() => (app.page = 'security')}>Set up passphrase</button>
    </div>
  {:else if !st.pin?.verified}
    <form class="card mb-5 flex flex-wrap items-center gap-4" onsubmit={unlockDevice}>
      <div class="grid h-10 w-10 place-items-center rounded-xl bg-amber-500/15 text-amber-300"><Icon name="lock" class="h-5 w-5" /></div>
      <div class="min-w-48 flex-1">
        <div class="font-medium">Your ESP Crypt is locked</div>
        <div class="text-xs text-neutral-400">Enter your Vault passphrase to use your vaults.{st.pin?.retries >= 0 ? ` ${st.pin.retries} attempts left.` : ''}</div>
      </div>
      <input class="input w-60" type="password" placeholder="Vault passphrase" bind:value={pin} autocomplete="off" />
      <button class="btn btn-primary" disabled={!pin || unlocking}><Icon name="unlock" /> {unlocking ? 'Unlocking…' : 'Unlock'}</button>
    </form>
  {/if}

  <div class="mb-4 flex flex-wrap items-center gap-2">
    <h2 class="mr-auto text-base font-semibold">My vaults <span class="ml-1 text-sm font-normal text-neutral-500">{vaults.length ? `${vaults.length} · ${openCount} open` : ''}</span></h2>
    {#if openCount > 1}<button class="btn" onclick={async () => { await run(() => Go.LockAllVaults(), 'All vaults locked'); load() }}><Icon name="lock" /> Lock all</button>{/if}
    <button class="btn" disabled={!st.connected || busyId === 'open'} onclick={openFile}><Icon name="folder" /> {busyId === 'open' ? 'Opening…' : 'Open vault file…'}</button>
    <button class="btn btn-primary" disabled={!info.installed || !st.connected || st.pin?.notInitialized} onclick={openCreate}><Icon name="plus" /> New vault</button>
  </div>

  {#if vaults.length === 0}
    <div class="card flex flex-col items-center py-12 text-center">
      <div class="mb-4 grid h-16 w-16 place-items-center rounded-3xl bg-violet-500/10 text-violet-300"><Icon name="lock" class="h-8 w-8" /></div>
      <div class="text-lg font-semibold">Create your first vault</div>
      <p class="mt-2 max-w-md text-sm text-neutral-400">A vault is an encrypted drive. Its keyfile is stored in the ESP32's passphrase-encrypted container, so the vault opens only while your device is connected and unlocked.</p>
      <button class="btn btn-primary mt-5" disabled={!info.installed || !st.connected || st.pin?.notInitialized} onclick={openCreate}><Icon name="plus" /> New vault</button>
    </div>
  {:else}
    <div class="card overflow-hidden p-0">
      <div class="grid grid-cols-[minmax(0,1fr)_96px_auto] xl:grid-cols-[minmax(0,1fr)_96px_110px_80px_auto] items-center gap-4 border-b border-neutral-700/50 px-5 py-2.5 text-[11px] font-semibold tracking-wider text-neutral-500 uppercase">
        <span>Vault</span><span>Status</span><span class="hidden xl:block">Protection</span><span class="hidden xl:block">Size</span><span class="text-right">Actions</span>
      </div>
      {#each vaults as v (v.id)}
        <div class="grid grid-cols-[minmax(0,1fr)_96px_auto] xl:grid-cols-[minmax(0,1fr)_96px_110px_80px_auto] items-center gap-4 border-b border-neutral-700/40 px-5 py-3 last:border-0 hover:bg-neutral-700/20">
          <div class="flex min-w-0 items-center gap-3">
            <div class="grid h-9 w-9 shrink-0 place-items-center rounded-xl {v.mounted ? 'bg-violet-500/15 text-violet-300' : 'bg-neutral-700/60 text-neutral-300'}">
              <Icon name={v.mounted ? 'unlock' : 'lock'} class="h-4.5 w-4.5" />
            </div>
            <div class="min-w-0">
              <div class="truncate text-sm font-medium">{v.name}</div>
              <div class="truncate text-xs text-neutral-500" title={v.path}>
                {#if !v.exists}<span class="text-amber-300">file missing · </span>{:else if v.twoKey && !v.mounted && v.missing?.length}<span class="text-amber-300">needs {v.missing.join(' + ')} · </span>{:else if !v.twoKey && st.connected && st.pin?.verified && !v.keyfileOnDevice}<span class="text-amber-300">keyfile not on this device · </span>{/if}{v.mounted ? v.mountPoint : v.path}
              </div>
            </div>
          </div>
          <div>{#if v.mounted}<span class="badge badge-ok">Unlocked</span>{:else}<span class="badge badge-muted">Locked</span>{/if}</div>
          <div class="hidden text-xs text-neutral-400 xl:block" title={v.twoKey ? `Keyfiles on ${v.deviceSerial} and ${v.secondDeviceSerial}` : ''}>{v.twoKey ? (v.hasPassword ? '2 ESPs + password' : '2 ESPs') : v.hasPassword ? 'ESP32 + password' : 'ESP32 keyfile'}</div>
          <div class="hidden text-xs text-neutral-400 xl:block">{v.exists ? fmtBytes(v.size) : '–'}</div>
          <div class="flex items-center justify-end gap-1">
            {#if v.mounted}
              <button class="btn btn-sm btn-primary" onclick={() => Go.OpenPath(v.mountPoint)}><Icon name="folder" /> Open</button>
              <button class="btn btn-sm" disabled={busyId === v.id} onclick={() => lock(v)}><Icon name="lock" /> {busyId === v.id ? 'Locking…' : 'Lock'}</button>
              {#if info.canChangePassword}<button class="btn btn-sm btn-ghost" title={v.hasPassword ? 'Change password' : 'Add a password'} disabled={busyId === v.id} onclick={() => changePassword(v)}><Icon name="key" /></button>{/if}
            {:else}
              <button class="btn btn-sm btn-primary" disabled={busyId === v.id || !v.exists || !st.connected || !info.installed} onclick={() => unlock(v)}>
                <Icon name="unlock" /> {busyId === v.id ? 'Unlocking…' : 'Unlock'}</button>
            {/if}
            <button class="btn btn-sm btn-ghost" title="Show file in {info.os === 'windows' ? 'Explorer' : 'Finder'}" onclick={() => Go.RevealVault(v.id)}><Icon name="file" /></button>
            <button class="btn btn-sm btn-ghost" title="Rename" onclick={() => rename(v)}><Icon name="edit" /></button>
            <button class="btn btn-sm btn-ghost" title="Remove from list" onclick={() => forget(v)}><Icon name="x" /></button>
          </div>
        </div>
      {/each}
    </div>
    <p class="hint mt-4">When the device disconnects, the app detects it and locks open vaults automatically. 2-ESP vaults also lock when either of their chips is locked, switched or unplugged.</p>
  {/if}
{/if}

{#if showCreate}
  <div class="fixed inset-0 z-30 grid place-items-center bg-black/60 p-6 backdrop-blur-sm">
    <form class="card max-h-full w-[34rem] overflow-y-auto" onsubmit={create}>
      <div class="card-title"><Icon name="plus" /> New vault</div>
      <div class="space-y-4">
        <div><label class="label" for="vn">Name</label>
          <!-- svelte-ignore a11y_autofocus -->
          <input id="vn" class="input" bind:value={form.name} placeholder="e.g. Personal documents" autofocus /></div>
        <div>
          <div class="label">Size</div>
          <div class="flex flex-wrap gap-2">
            {#each sizes as [mb, l]}
              <button type="button" class="tab border border-neutral-600/60 {form.size === mb ? 'tab-active border-violet-500/60' : ''}" onclick={() => (form.size = mb)}>{l}</button>
            {/each}
            {#if form.size === 0}<input class="input w-32" type="number" min="5" bind:value={form.customSize} /><span class="self-center text-sm text-neutral-400">MB</span>{/if}
          </div>
        </div>
        <div class="rounded-xl border border-neutral-700/50 bg-neutral-900/40 p-4">
          <div class="flex items-start gap-3">
            <Icon name="shield" class="mt-0.5 h-5 w-5 text-violet-300" />
            {#if twoKey}
              <div class="text-sm"><span class="font-medium">Protected by two ESP Crypts.</span> <span class="text-neutral-400">Each chip generates its own keyfile. The vault opens only while both chips are connected and unlocked, and locks when either one is locked or unplugged.</span></div>
            {:else}
              <div class="text-sm"><span class="font-medium">Protected by your ESP Crypt.</span> <span class="text-neutral-400">A unique keyfile is generated in the passphrase-encrypted device container. The vault can't be opened without the connected device and its passphrase.</span></div>
            {/if}
          </div>
          <label class="check mt-3"><input type="checkbox" bind:checked={twoKey} /> Require two ESP Crypts (2-ESP mode)</label>
          {#if twoKey}
            <div class="mt-3 space-y-2 rounded-lg border border-neutral-700/50 p-3 text-sm">
              <div class="flex flex-wrap items-center justify-between gap-3"><span class="text-neutral-400">Key 1</span><span>{app.devices.find((d) => d.active) ? shortLabel(app.devices.find((d) => d.active)) : 'no active chip'} <span class="text-neutral-500">(active)</span></span></div>
              <div class="flex flex-wrap items-center justify-between gap-3">
                <label class="text-neutral-400" for="v2">Key 2</label>
                {#if otherDevices.length}
                  <select id="v2" class="input w-56 text-xs" bind:value={secondId}>
                    {#each otherDevices as d (d.id)}<option value={d.id}>{shortLabel(d)}{d.unlocked ? '' : ' · locked'}</option>{/each}
                  </select>
                {:else}
                  <span class="text-amber-300">Connect a second ESP Crypt</span>
                {/if}
              </div>
              {#if secondDev && !secondDev.unlocked}<p class="hint">You'll be asked for the passphrase of {shortLabel(secondDev)} when you create the vault.</p>{/if}
            </div>
          {/if}
          <label class="check mt-3"><input type="checkbox" bind:checked={usePassword} /> Also require a password (optional)</label>
          {#if usePassword}
            <div class="mt-3 grid gap-2 sm:grid-cols-2">
              <input class="input" type="password" placeholder="Password" autocomplete="new-password" bind:value={form.password} />
              <input class="input" type="password" placeholder="Repeat password" autocomplete="new-password" bind:value={form.password2} />
            </div>
          {/if}
        </div>
        <div>
          <div class="label">Location</div>
          <div class="flex items-center gap-2">
            <div class="input flex flex-1 items-center truncate text-xs text-neutral-400">{form.path || `Documents/Vaults/${(form.name || 'Vault').replace(/[\\/:*?"<>|]/g, '_')}.hc32`}</div>
            <button type="button" class="btn" onclick={pickLocation}>Change…</button>
          </div>
        </div>
        <button type="button" class="text-xs text-neutral-400 hover:text-neutral-200" onclick={() => (showAdvanced = !showAdvanced)}>{showAdvanced ? '▾' : '▸'} Advanced options</button>
        {#if showAdvanced}
          <div class="grid gap-3 sm:grid-cols-2">
            <div><label class="label" for="ve">Encryption</label><select id="ve" class="input" bind:value={form.encryption}>{#each info.encryptions as x}<option value={x}>{x}</option>{/each}</select></div>
            <div><label class="label" for="vh">Hash (PRF)</label><select id="vh" class="input" bind:value={form.hash}>{#each info.hashes as x}<option value={x}>{x}</option>{/each}</select></div>
            <div><label class="label" for="vf">File system</label><select id="vf" class="input" bind:value={form.filesystem}>{#each info.filesystems as x}<option value={x}>{x}</option>{/each}</select></div>
            {#if info.canCreatePim}<div><label class="label" for="vp">PIM (0 = default)</label><input id="vp" class="input" type="number" min="0" bind:value={form.pim} /></div>{/if}
            <label class="check sm:col-span-2"><input type="checkbox" bind:checked={form.fullFormat} /> Full format (fills the vault with random data; slower, hides how much is used)</label>
          </div>
        {/if}
      </div>
      <div class="mt-6 flex justify-end gap-2">
        <button type="button" class="btn btn-ghost" onclick={() => (showCreate = false)}>Cancel</button>
        <button class="btn btn-primary" disabled={busyId === 'create' || !form.name || sizeMB < 5 || (usePassword && !form.password) || (twoKey && !secondDev)}>
          {busyId === 'create' ? 'Creating…' : 'Create vault'}</button>
      </div>
    </form>
  </div>
{/if}

{#if chooser}
  <div class="fixed inset-0 z-30 grid place-items-center bg-black/60 p-6 backdrop-blur-sm">
    <form class="card w-[30rem]" onsubmit={(e) => { e.preventDefault(); const c = chooser; chooser = null; tryOpen(c.path, c.pw, +c.pick) }}>
      <div class="card-title">Which keyfile belongs to this vault?</div>
      <p class="hint mb-4">Your ESP Crypt holds several keyfiles and the app could not tell which one opens <span class="text-neutral-200">{chooser.path.split(/[\\/]/).pop()}</span>.</p>
      <div class="space-y-1.5">
        {#each chooser.choices as c}
          <label class="check rounded-lg border border-neutral-700/50 px-3 py-2"><input type="radio" name="kf" value={c.id} bind:group={chooser.pick} /> {c.label}</label>
        {/each}
      </div>
      <div class="mt-6 flex justify-end gap-2">
        <button type="button" class="btn btn-ghost" onclick={() => (chooser = null)}>Cancel</button>
        <button type="submit" class="btn btn-primary">Open vault</button>
      </div>
    </form>
  </div>
{/if}

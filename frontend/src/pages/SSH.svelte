<script>
  import { onMount } from 'svelte'
  import { app, Go, run, ask, promptText, refreshStatus, withVaultPIN } from '../lib/store.svelte.js'
  import Icon from '../lib/Icon.svelte'

  let settings = $state({})
  let keys = $state([])
  let label = $state('')
  let showNew = $state(false)
  let busy = $state(false)

  async function load() {
    if (!app.status.connected) { keys = []; return }
    keys = (await Go.ListSSHKeys().catch(() => [])) ?? []
  }
  onMount(async () => { settings = await Go.GetSettings(); load() })
  let deviceState = $derived(app.devices.map((d) => `${d.id}:${d.unlocked}:${d.second}`).join(',') + app.keyMode)
  $effect(() => { app.status.connected; app.status.pin?.verified; app.status.serial; deviceState; load() })

  let locked = $derived(!app.status.pin?.verified)
  let initialized = $derived(app.status.vaultStorageReady && app.status.pin && !app.status.pin.notInitialized)
  let ag = $derived(app.status.agent ?? {})
  let ready = $derived(ag.running && ag.configInstalled)

  async function save() { await run(() => Go.SaveSettings($state.snapshot(settings)), 'Settings saved') }

  async function generate(e) {
    e.preventDefault()
    busy = true
    const k = await withVaultPIN(() => Go.GenerateSSHKey(label), twoKey ? 'SSH key pair created on both ESP32s' : 'SSH key created on the ESP32', 'Unlock your ESP Crypt to create the SSH key.')
    busy = false
    if (k && k !== true) { label = ''; showNew = false; load() }
  }
  async function rename(k) {
    const n = await promptText('Rename SSH key', k.label)
    if (n && n !== k.label) { await run(() => Go.RenameSSHKey(k.deviceId, k.id, n)); load() }
  }
  async function del(k) {
    if (!(await ask({ title: 'Delete SSH key', danger: true, confirmText: 'Delete forever', typeToConfirm: k.label,
      message: `"${k.label}" is erased from ${k.deviceLabel}. The private key exists nowhere else, so servers that trust only this key will no longer let you in.` }))) return
    await run(() => Go.DeleteSSHKey(k.deviceId, k.id), 'SSH key deleted')
    load()
  }
  let twoKey = $derived(app.keyMode === 2)
  let secondDevice = $derived(app.devices.find((d) => d.second))
  let groups = $derived.by(() => {
    const out = []
    for (const k of keys) {
      let g = out.find((x) => x.deviceId === k.deviceId)
      if (!g) out.push((g = { deviceId: k.deviceId, label: k.deviceLabel, second: k.second, keys: [] }))
      g.keys.push(k)
    }
    return out
  })

  async function turnOn() {
    await run(async () => {
      await Go.StartAgent()
      if (!ag.configInstalled) await Go.SetSSHConfig(true)
      settings.agentAutostart = true
      await Go.SaveSettings($state.snapshot(settings))
      await refreshStatus()
    }, 'SSH now uses your ESP Crypt')
  }
  async function turnOff() {
    await run(async () => {
      await Go.StopAgent()
      await Go.SetSSHConfig(false)
      settings.agentAutostart = false
      await Go.SaveSettings($state.snapshot(settings))
      await refreshStatus()
    }, 'SSH agent turned off')
  }
</script>

<div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_380px]">
  <div class="min-w-0 space-y-5">
    <section class="card">
      <div class="flex items-start gap-5">
        <div class="grid h-14 w-14 shrink-0 place-items-center rounded-2xl {ready ? 'bg-violet-500/15 text-violet-300' : 'bg-neutral-700/60 text-neutral-300'}"><Icon name="terminal" class="h-7 w-7" /></div>
        <div class="flex-1">
          <div class="text-lg font-semibold">{ready ? 'SSH uses your ESP Crypt' : ag.running ? 'SSH agent is running, ssh does not use it yet' : 'SSH agent is off'}</div>
          <p class="mt-1 text-sm text-neutral-400">{ready ? 'ssh, scp, git and VS Code Remote sign in with the keys on your active ESP32. The private keys never leave the chip.' : ag.running ? 'Turn it on to point ~/.ssh/config at the agent, or set SSH_AUTH_SOCK yourself (see Technical details).' : 'Turn it on and ssh/git sign in with keys generated on your ESP32. No ssh-add or key files on disk.'}</p>
          <div class="mt-4 flex gap-2">
            {#if ready}
              <button class="btn" onclick={turnOff}><Icon name="stop" /> Turn off</button>
            {:else}
              <button class="btn btn-primary" onclick={turnOn}><Icon name="play" /> Turn on</button>
            {/if}
          </div>
        </div>
      </div>
    </section>

    {#if !app.status.connected}
      <div class="card text-sm text-neutral-400">Connect your custom ESP Crypt to see its SSH keys.</div>
    {:else if !initialized}
      <section class="card border-amber-500/30">
        <div class="card-title text-amber-100"><Icon name="shield" /> Vault storage is not initialized</div>
        <p class="mb-4 text-sm text-neutral-300">Set a Vault passphrase first; SSH keys are stored in the same encrypted container as your keyfiles.</p>
        <button class="btn btn-primary" onclick={() => (app.page = 'security')}>Open passphrase setup</button>
      </section>
    {:else}
      <section class="card">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div class="card-title mb-0"><Icon name="key" /> {twoKey ? 'SSH keys · 2-ESP mode' : `SSH keys on ${app.status.serial}`}</div>
          <button class="btn btn-sm" disabled={locked} onclick={() => (showNew = !showNew)}><Icon name="plus" /> New SSH key</button>
        </div>
        {#if showNew}
          <form class="mb-4 grid gap-3 rounded-xl border border-neutral-700/50 bg-neutral-900/40 p-4 md:grid-cols-[1fr_auto]" onsubmit={generate}>
            <input class="input" bind:value={label} placeholder="Name, e.g. laptop or github" />
            <button class="btn btn-primary" disabled={!label || busy || (twoKey && !secondDevice)}>{busy ? 'Generating…' : twoKey ? 'Generate on both ESP32s' : 'Generate on ESP32'}</button>
            <p class="hint md:col-span-2">An ECDSA P-256 key (ecdsa-sha2-nistp256) is generated by the chip's hardware random generator. It cannot be exported; only signatures leave the device.{twoKey ? ` In 2-ESP mode a key is generated on ${app.status.serial} and on ${secondDevice?.label ?? 'the second chip (connect one first)'}.` : ''}</p>
          </form>
        {/if}
        {#if twoKey}
          <div class="callout callout-info mb-3 text-xs">
            <span class="font-medium">2-ESP mode:</span> the agent signs only while both chips are connected and unlocked. To make a server require both chips, add both public keys to <code>authorized_keys</code> and set <code>AuthenticationMethods publickey,publickey</code> in its <code>sshd_config</code>.
          </div>
        {/if}
        {#each groups as g (g.deviceId)}
          {#if groups.length > 1 || twoKey}<div class="mt-3 text-[11px] font-semibold tracking-wider text-neutral-500 uppercase">{g.label}{g.second ? ' · second chip' : ' · active'}</div>{/if}
          <div class="divide-y divide-neutral-700/50">
            {#each g.keys as k (k.deviceId + ':' + k.id)}
              <div class="flex items-center gap-4 py-3">
                <div class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-neutral-700/60 text-violet-300"><Icon name="key" /></div>
                <div class="min-w-0 flex-1">
                  <div class="text-sm font-medium">{k.label}</div>
                  <div class="truncate font-mono text-xs text-neutral-500">{k.type} · {k.fingerprint}</div>
                </div>
                <button class="btn btn-sm" onclick={() => run(() => Go.CopyText(k.authorized), 'Public key copied')}><Icon name="copy" /> Copy public key</button>
                <button class="btn btn-sm btn-ghost" title="Rename" disabled={k.second ? !secondDevice?.unlocked : locked} onclick={() => rename(k)}><Icon name="edit" /></button>
                <button class="btn btn-sm btn-ghost text-rose-300" title="Delete" disabled={k.second ? !secondDevice?.unlocked : locked} onclick={() => del(k)}><Icon name="trash" /></button>
              </div>
            {/each}
          </div>
        {:else}
          <p class="py-6 text-center text-sm text-neutral-500">{locked ? 'Unlock the ESP Crypt to see and create SSH keys.' : 'No SSH keys yet. Create one, then add its public key to your server or GitHub.'}</p>
        {/each}
        {#if locked && keys.length}<p class="hint mt-3">Locked: ssh can still see these keys; signing asks you to unlock the Vault first.</p>{/if}
      </section>
    {/if}
  </div>

  <aside class="space-y-5">
    <div class="card">
      <div class="card-title"><Icon name="shield" /> Options</div>
      <div class="space-y-3">
        <label class="check"><input type="checkbox" bind:checked={settings.confirmSign} onchange={save} /> Ask before every SSH sign-in</label>
        <label class="check"><input type="checkbox" bind:checked={settings.agentAutostart} onchange={save} /> Start the agent with the app</label>
      </div>
      <p class="hint mt-3">When the Vault is locked and ssh needs a key, the app asks for your passphrase and the connection continues once it is unlocked.</p>
    </div>
    <details class="card text-sm">
      <summary class="cursor-pointer font-medium">Technical details</summary>
      <dl class="kv mt-3 text-xs"><dt>Agent</dt><dd>{ag.socket}</dd><dt>~/.ssh/config</dt><dd class="font-sans">{ag.configInstalled ? 'IdentityAgent set for all hosts' : 'not set'}</dd></dl>
      <div class="mt-3 text-xs text-neutral-400">Use it in a terminal without changing ~/.ssh/config:</div>
      <pre class="selectable mt-1 rounded-lg bg-neutral-900 p-3 text-xs">$env:SSH_AUTH_SOCK = "{ag.socket}"</pre>
      <div class="mt-3 text-xs text-neutral-400">Sign git commits with your ESP32:</div>
      <pre class="selectable mt-1 rounded-lg bg-neutral-900 p-3 text-xs">git config --global gpg.format ssh
git config --global user.signingkey "key::&lt;public key&gt;"
git config --global commit.gpgsign true</pre>
    </details>
  </aside>
</div>

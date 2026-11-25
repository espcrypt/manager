<script>
  let { name, class: cls = 'h-4 w-4' } = $props()
  const paths = {
    dashboard: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z',
    key: 'M15.5 7.5a3.5 3.5 0 1 1-7 0 3.5 3.5 0 0 1 7 0zM12 11v10m0-4h3m-3 2h2',
    file: 'M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9l-6-6zm0 0v6h6',
    terminal: 'M4 17l6-5-6-5m8 12h8',
    lock: 'M6 11V8a6 6 0 1 1 12 0v3M5 11h14v10H5z',
    shield: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6l8-3z',
    archive: 'M3 7h18v4H3zM5 11v9h14v-9M10 15h4',
    chip: 'M7 7h10v10H7zM9 3v4m6-4v4M9 17v4m6-4v4M3 9h4m-4 6h4m10-6h4m-4 6h4',
    disk: 'M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zm4 10h8',
    code: 'M8 8l-4 4 4 4m8-8l4 4-4 4M14 4l-4 16',
    list: 'M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01',
    copy: 'M9 9h10v10H9zM5 15V5h10',
    trash: 'M4 7h16M10 11v6m4-6v6M6 7l1 13h10l1-13M9 7V4h6v3',
    download: 'M12 4v12m0 0l-4-4m4 4l4-4M4 20h16',
    upload: 'M12 20V8m0 0l-4 4m4-4l4 4M4 4h16',
    plus: 'M12 5v14M5 12h14',
    refresh: 'M4 4v6h6M20 20v-6h-6M5 14a8 8 0 0 0 14 3M19 10A8 8 0 0 0 5 7',
    check: 'M5 12l5 5L20 7',
    x: 'M6 6l12 12M18 6L6 18',
    edit: 'M4 20h4L19 9l-4-4L4 16v4z',
    usb: 'M12 2v14m0 0a3 3 0 1 0 0 .01M8 6h8M7 10l5-4 5 4',
    wifi: 'M2 8.5a15 15 0 0 1 20 0M5 12a10 10 0 0 1 14 0M8.5 15.5a5 5 0 0 1 7 0M12 19h.01',
    bluetooth: 'M7 7l10 10-5 4V3l5 4L7 17',
    play: 'M7 5v14l11-7z',
    stop: 'M6 6h12v12H6z',
    unlock: 'M6 11V8a6 6 0 0 1 11.3-2.8M5 11h14v10H5z',
    cert: 'M5 4h14v12H5zM9 20l3-2 3 2v-4H9z',
    bolt: 'M13 2L4 14h7l-1 8 9-12h-7z',
    database: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zm0 0v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
    warning: 'M12 3L2 21h20L12 3zm0 6v5m0 3h.01',
    eye: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zm10 3a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
    folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  }
</script>

<svg class={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  <path d={paths[name] ?? ''} />
</svg>

<div align="center">
  <img src="https://raw.githubusercontent.com/Yaxyobek0877/fastremote/main/docs/images/banner.png" alt="FastRemote Banner" width="800">
  <h1>⚡ FastRemote</h1>
  <p><b>Xavfsiz, tezkor va mustaqil masofaviy boshqaruv agenti (Remote Desktop & Terminal)</b></p>
</div>

---

## 📸 Skrinshotlar

<div align="center">
  <img src="docs/images/settings.png" alt="Settings Page" width="800">
</div>

---

## ✨ Imkoniyatlar

- 🚀 **Standalone ishlaydi**: Alohida signaling server yoki backend talab qilmaydi (barcha narsa, jumladan React frontend ham bitta Go binary fayl ichida ishlaydi).
- 🖥️ **Remote Desktop**: 60 FPS gacha bo'lgan to'liq DPI-moslashuvchan ekran translyatsiyasi.
- ⌨️ **To'liq nazorat**: Mouse (1:1 harakat) va Keyboard (barcha tugmalar, kombinatsiyalar) nazorati.
- 🧑‍💻 **Web Terminal**: Server komandalarini brauzerdan turib masofadan boshqarish.
- 📁 **Fayl Menejeri**: Tezkor fayl qidirish, yuklash va o'chirish.
- 🔒 **Xavfsizlik & Multi-User**: `bcrypt` orqali shifrlangan parollar. Admin va oddiy user rollari.
- ⏱️ **Sessiya jurnali**: Faollik va ishlash vaqtini kuzatish.

---

## 📊 Statistik Taqqoslash: FastRemote vs AnyDesk & RustDesk

FastRemote o'zining mustaqil arxitekturasi va optimallashtirilgan Go engine'i tufayli, bozordagi boshqa gigantlarga qaraganda eng barqaror va past kechikuvchanlik (Low Latency) bilan ishlaydi. Quyidagi infografikada ularning qanday farq qilishi ko'rsatilgan:

<div align="center">
  <img src="docs/images/stats.png" alt="FastRemote Statistics vs AnyDesk vs RustDesk" width="800">
</div>

* **Performance (Tezlik)**: FastRemote — 60 FPS va yuqori bitreyt imkoniyati.
* **Latency (Kechikish)**: P2P va Direct aloqa tufayli eng past (ultra-low) kecikish.
* **Stability (Barqarorlik)**: Go va React integratsiyasi evaziga 99.8% uzluksiz aloqa.

---

## 🛠 O'rnatish

Loyihani klonlash ancha osonlashtirilgan. Barcha qismlar (Frontend va Backend) bitta joyga jamlangan.

### 1-qadam: Repository-ni klonlash

Chet el (yoki qaysidir) serveringizda:

```bash
git clone https://github.com/Yaxyobek0877/fastremote.git
cd fastremote
```

### 2-qadam: Loyihani build qilish (Frontend va Backend)

Dastlab, React qismini build qilib, uni Go build tizimi o'qishi uchun to'g'rilang:

```bash
# Frontend-ni build qilish
cd web
npm install
npm run build

# Frontend fayllarini Backend jildiga o'tkazish
rm -rf ../agent/dist
cp -r dist ../agent/dist

# Go Binary-ni build qilish
cd ../agent
go build -o fastremote-agent .
```

---

## 🚀 Ishga tushirish (Nginx va Systemd yordamida)

**Muhim eslatma:** Bu loyiha Python emas, balki **Golang** (Go) tilida yozilgan. Shu sababli, **Gunicorn o'rnatish shart emas! Go o'zining ichki, juda kuchli serveriga ega.** Gunicorn vazifasini **Systemd** (dasturni fonda ushlab turish uchun) bajaradi.

### 1. Systemd Service (Dasturni fonda doimiy ishlashi uchun)

Fayl yarating: `sudo nano /etc/systemd/system/fastremote.service`

Ichiga quyidagilarni yozing:

```ini
[Unit]
Description=FastRemote Agent Service
After=network.target

[Service]
Type=simple
User=root
# Dastur turgan papkaga yo'lni to'g'rilang
WorkingDirectory=/root/fastremote/agent
# Default port va parolni shu yerdan bering:
Environment="PORT=9090"
Environment="ADMIN_PASSWORD=admin123"
ExecStart=/root/fastremote/agent/fastremote-agent
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Saqlang va xizmatni yoqing:
```bash
sudo systemctl daemon-reload
sudo systemctl enable fastremote
sudo systemctl start fastremote
sudo systemctl status fastremote
```

### 2. Nginx Sozlamalari (Reverse Proxy sifatida)

Dasturning xavfsizlik protokoli va WebSocket'lar to'g'ri ishlashi uchun Nginx'da biroz to'g'rilash kerak bo'ladi. Domenizni `desk.1pro.uz` deb hisobga olamiz.

Konfiguratsiya yarating: `sudo nano /etc/nginx/sites-available/fastremote`

```nginx
server {
    listen 80;
    server_name desk.1pro.uz; # O'zingizning domeningiz

    # SSL konfiguratsiyasini Certbot orqali qo'shishingiz tavsiya etiladi
    
    location / {
        proxy_pass http://localhost:9090; # Dastur ishlayotgan port
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        # Original IP larni uzatish
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Nginx-ga ulab, serverni restart qiling:
```bash
sudo ln -s /etc/nginx/sites-available/fastremote /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl restart nginx
```

### 3. Tayyor!
Endi brauzeringizdan `http://desk.1pro.uz` (yoki SSL o'rnatilgan bo'lsa `https://`) orqali kiring.
Standart loginingiz:
* **Username**: `admin`
* **Password**: `admin123` *(Service faylga yozganingiz)*

Kira solib **Settings** bo'limidan maxfiy parolingizni yangilab oling!

### Persistent terminal deployment (standalone Linux agent)

The terminal workspace uses a dedicated tmux server and authenticated
`/api/terminals` / `/ws/terminal` endpoints. Each account has its own catalogue,
active tab, names and independent shells. Closing a browser, switching pages or
restarting the HTTP agent only detaches clients. Shells, working directories,
running commands and full-screen application state remain in tmux. The active
device controls the shared pane size; other devices can reconnect or type to
fit it to their own viewport.

On this server the source is `/disk1/test_project/FastRemote`. The user services
are `fastremote-terminal.service` (tmux) and `fastremote-web.service` (HTTP on
9091). Templates, `terminal-server.sh` and the isolated tmux configuration are
in `deploy/`; their absolute paths must match the checkout. Keep the wrapper
executable. It supports tmux 3.0a, signals readiness after the socket works and
checks the server every two seconds, recreating it if it has crashed.

For a fresh installation, copy the units to `~/.config/systemd/user/`, run
`systemctl --user daemon-reload`, then `systemctl --user enable --now
fastremote-terminal fastremote-web`. Enable login lingering with
`loginctl enable-linger "$USER"` so the services start at boot without an
interactive login; lingering is already enabled for `face` on this server.

When upgrading an existing installation, copy the updated units and wrapper,
run `systemctl --user daemon-reload`, and enable the units **without `--now`**.
Do not restart `fastremote-terminal.service` or the legacy
`fastremote-agent.service` while their shells are in use. The new terminal
supervisor takes effect on the next host boot; reloading unit definitions does
not interrupt the current tmux server or legacy shell.

Build the frontend with `cd web && npm ci && npm run build`, copy `web/dist/.`
into `agent/dist/`, then build the agent with `go build -o fastremote-web.next .`
from `agent/`. Replace `fastremote-web` by renaming the new binary and restart
**only `fastremote-web.service`**. Do not restart the tmux service during ordinary
web deployments, because that would end running shells. The runtime socket is
`$XDG_RUNTIME_DIR/fastremote/tmux.sock`; `TERMINAL_SOCKET` can override it.

The catalogue and private ANSI checkpoints are under `agent/data/terminals/`
(`TERMINAL_DATA_DIR` overrides this path). Files are written atomically with
owner-only permissions. Up to 24 terminals per account and 10,000 scrollback
lines per live terminal are retained. Recovery archives are bounded to 4 MiB.
The catalogue keeps a last-valid backup; screen, current directory and session
state are checkpointed every five seconds and during graceful web shutdown.
An abrupt power loss can lose changes since the last successful checkpoint,
normally up to five seconds of recent screen output or directory changes.

After a host restart or tmux failure, previously running terminals reopen with
the same IDs, titles, ownership and selected tab, in their last saved directory.
Saved output remains above a fresh shell. If that directory no longer exists,
the shell opens in the home directory and shows a recovery notice. New and
recovered shells append completed commands to private per-session Bash history
at each prompt. Deliberately exited sessions stay stopped until **Qayta ochish**
is selected.

A full power cycle ends processes and their RAM state. Recovery does not replay
commands or automatically restart an interrupted application. The interface
shows when a fresh shell was restored; an application with its own saved session
can then be resumed explicitly. Terminals execute as the agent's OS user, as the
remote desktop does; account-scoped API ownership is not an OS sandbox.

For the initial migration, the old `fastremote-agent.service` remains on 9090
so an existing shell is not terminated. An optional local systemd drop-in sets
`LEGACY_TERMINAL_URL` and `LEGACY_TERMINAL_OWNER` to expose that shell only to
its owner as **Avvalgi terminal**. Its earlier browser-only scrollback cannot
be recovered. On reconnect the browser briefly changes and restores the old
PTY's width to request a fresh application screen without sending keystrokes.
It reports a connected terminal only after shell output arrives. A read-only
recorder now saves subsequent legacy output, directory and boot identity every
five seconds. After the first host reboot, this archive becomes an owned tmux
terminal with a stable new ID; the old tab is not connected to an unrelated
fresh legacy shell. When a saved Codex session can be identified, the terminal
menu offers its `codex resume ...` command for copying, never automatic execution.
New sessions always use the persistent system. Keep the legacy service running
during migration; retire it only after its old shell is no longer needed.
Cloudflare ingress points to the new HTTP port after validation.

Validation: `cd agent && go test -race ./... && go vet ./...`; frontend build and
ESLint on the changed components. `web/scripts/verify-terminals.cjs` is a local
integration check requiring Playwright (`PLAYWRIGHT_MODULE` can locate it),
Chrome, and read access to `agent/.jwt_secret`. It creates isolated test owners,
checks browser disconnect/reconnect, independent sessions, Unicode, history,
full-screen state and mobile layouts, then deletes its own terminals. Setting
`TEST_SERVICE_RESTART=1` also restarts the staging `fastremote-web.service` and
checks that the shell PID is unchanged. Do not use that flag against unrelated
services. The script defaults to `http://127.0.0.1:9091`.
`web/scripts/verify-legacy-terminal.cjs` checks legacy readiness and same-size
reloads using mocked WebSockets, without connecting to an existing user shell.
Recovery backend tests simulate tmux loss using isolated test servers; they do
not reboot the host. `web/scripts/verify-terminal-recovery.cjs` checks recovery
notices and reopening controls in the browser.

### Project terminals

The terminal view has two fixed control rows: session tabs and new terminal,
then connection status and actions. Font size, rename, delete and history export
are under `•••`; `⌨` toggles touch keys over the terminal. Choosing a project in
**Yangi terminal** starts the shell directly in that folder. The chosen folder
is remembered in this browser and persisted with the session. Existing sessions
continue running without changing their current directory.

Copy `deploy/projects.example.json` to `~/.config/fastremote/projects.json` and
edit the names and absolute paths (or `~/...`). `TERMINAL_PROJECTS_FILE` overrides
the configuration path. Only existing directories are shown. Users can also
enter another full directory path; access uses the server OS account's existing
filesystem permissions, just like commands in its shell.

`web/scripts/verify-projects.cjs` verifies preset selection, the real shell working
directory, remembered project selection, invalid paths, menus, and the two-row
layout at 320–1440 px. Set `FASTREMOTE_PROJECT_PATH` to a configured preset path
(default: this repository). It creates and removes only its own test sessions.

### Terminal images and files

Use **📎** to choose images or files from a phone or computer, drag files onto the
terminal, or paste an image from the clipboard. **Joylash** reads clipboard
images when the browser permits it and otherwise supports text paste; the file
picker works when image clipboard access is unavailable. Uploads accept up to
10 files per batch, at most 25 MiB per file and 50 MiB in total.

The browser uploads the files and pastes their quoted absolute server paths
into the connected terminal. It does **not** send Enter: review the input and
submit it yourself. If the connection drops during upload, the saved paths are
shown for pasting after reconnection. Applications receive paths to real files;
handling those files depends on the command or AI tool running in the terminal.

`POST /api/terminals/files?id=<session-id>` accepts multipart `files` fields with
the same Bearer authentication as other terminal APIs. Ownership and a running
session are required. Files are streamed into
`TERMINAL_DATA_DIR/attachments/<session-id>/` (an owner-derived private directory
for the live legacy session), using random filename prefixes, directory mode
0700 and file mode 0600. A failed batch removes its files. The response contains
`files: [{name, path, size, mime}]`; there is no public file-serving endpoint.

`web/scripts/verify-terminal-attachments.cjs` checks upload/paste behavior and
mobile layouts. Backend tests cover ownership, upload limits, binary contents,
private file permissions and cleanup of interrupted or rejected batches.

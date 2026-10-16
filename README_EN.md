# phpo

[中文](./README.md) · [English](./README_EN.md)

> A local containerized development environment manager for PHP developers: multiple versions of PHP / MySQL / PostgreSQL / Redis / Nginx coexisting, all running inside Docker or Podman.
> Desktop app for three platforms. GUI only — no command line.
>
> Downloads: <https://github.com/xiaokentrl/phpo/releases/latest>

---

## What it does in one sentence

Install it once and the machine has several PHP environments that do not disturb each other. Want 8.3 for site A and 7.4 for site B? Switch in the site list dropdown — no second machine, no touching the host PHP.

## Core conventions

First the rules this software runs by, then what happens when they are bent:

- **Versions coexist**: one container per service, named `phpo-{service}-{version}`. Switching a site's PHP points exactly at `php-{version}-fpm:9000`; no fuzzy matching.
- **Config is hand-editable**: vhost bodies and rewrite rules are all exposed for editing. Template-generated config must pass `nginx -t` before it hits disk, and rolls back if it does not. If Nginx is not running right now, the file is still written and validation is completed automatically once Nginx comes up. The copy you edited by hand is never overwritten automatically.
- **73 extensions**: pick extensions while installing PHP; 11 common ones are checked by default. Changing the extension set = recompile + bake an image + rebuild the container. Compilation streams line by line into the log drawer, and a failure names the exact extension.
- **Who owns the state**: the backend snapshot is the sole authority; the UI never "lights it up first and explains later". Every operation ends by flowing back real state — nothing pretends to have succeeded.
- **Passwords are plaintext**: default `123456`, may be empty, stored in plaintext in `config.yaml`. That is a design trade-off (see Security notes below), not a bug.
- **Changing password / port requires a rebuild**: for a database service, those two only reach the container after you click "Rebuild to apply". Data volumes are preserved — rebuilding does not lose data.
- **Backup chain**: logical dump → pause service → filesystem snapshot → archive → restart. A single unreadable file inside the archive is skipped and reported as an aggregated per-directory warning; one bad file does not kill the whole archive.
- **Network use**: only when pulling images / extension packages / checking for updates. No telemetry. An offline cache hit means zero network. Mirror sources can be added on the Settings page and affect only phpo's own `docker pull` — the Docker daemon config is never touched.
- **Directories**: working directory defaults to `~/phpo`, site root to `~/www`. The cache root / backup root / per-version data directory of each database service can be customized; once customized it fully replaces the default, with no second-level fallback.
- **Default port depends on the engine**: with Docker, a new site defaults to 80; with Podman (usually rootless) to 8080. A port you already saved is never rewritten by an engine switch.
- **A container engine must be available**: Docker or Podman. phpo decides by actually dialling the engine — not by whether the CLI exists, not by whether a socket file exists. On Linux with Docker, add your user to the `docker` group and log back in.
- **Read [`AGENTS.md`](./AGENTS.md) before changing code**: it is the project's highest technical charter, a frozen document, and it wins over any other document.

## UI gallery

### ① Overview: how many environments are running, at a glance
![phpo Overview](./docs/screenshots/01-overview.png)

### ② PHP service page: three versions side by side, each managed independently
![PHP Service Page](./docs/screenshots/02-service-php.png)

### ③ Site list: port, PHP version, health status — editable inline
![Site List](./docs/screenshots/03-sites.png)

### ④ vhost body: nginx config is yours to edit; `nginx -t` runs before writing to disk
![vhost Editor](./docs/screenshots/04-vhost-editor.png)

### ⑤ Rewrite rules: presets for the usual frameworks — Laravel / ThinkPHP / Yii2 / WordPress among them
![Rewrite Presets](./docs/screenshots/05-rewrite-presets.png)

### ⑥ Pick extensions while installing PHP: full catalog of 73 items, 11 common ones checked
![Select Extensions When Installing PHP](./docs/screenshots/06-install-extensions.png)

### ⑦ Manage extensions: you always submit the complete target set; compilation streams line by line
![Manage Extensions](./docs/screenshots/07-php-extensions.png)

### ⑧ Log drawer: what each step is doing and where it is stuck, written out line by line
![Task Log Drawer](./docs/screenshots/08-task-drawer.png)

### ⑨ Database service card: port, password, data directory all laid out
![MySQL Service Card](./docs/screenshots/09-service-mysql.png)

### ⑩ Backup: one-click packaging; an unreadable file does not condemn the whole archive
![Backup and Restore](./docs/screenshots/10-backup.png)

### ⑪ Warning instead of blocking: before stopping a PHP, it says clearly who is using it
![Preflight Warning Dialog](./docs/screenshots/11-preflight-warning.png)

### ⑫ Settings: theme, interface language, scaling, tray, autostart, mirror sources, working root
![Settings](./docs/screenshots/12-settings.png)

## Other features

- **8 interface languages**: Simplified Chinese, Traditional Chinese (Taiwan terminology), English, Korean, French, German, Spanish, Turkish. One click on the Settings page, no restart; the preference is stored locally only.
- **Start on login**: one switch on the Settings page. Turning it on writes the system autostart entry (Linux `.desktop` / Windows registry / macOS login item); turning it off removes it.
- **Docker cleanup panel**: a 60-row list at the bottom of the Overview page, 11 groups, covering containers, images, volumes, build cache and host-side directories. A row that cannot be counted shows "—" plus the reason — it never reports 0 to fake a clean machine. Deletion always goes "preview → one-time credential → execute"; the four dangerous rows cannot be taken even if you select all; items that still hold data move to the recycle bin first and stay 7 days.
- **Backup and restore**: the archive carries a logical dump; on restore the UI states plainly "the dump was not replayed automatically" — importing it back is your call.
- **Tray and single instance**: closing the window can send it to the tray. Only one phpo runs per machine; clicking the shortcut again brings the existing window forward instead of opening a second one.
- **Themes and scaling**: 9 themes, adjustable interface scaling.

## System requirements

| Item | Requirement |
|---|---|
| Container engine | Docker or Podman, currently running |
| Operating system | Windows / macOS / Linux desktop (all three are packaged; the Linux side is verified on real machines, Win / mac have not been walked through install → run → uninstall on real hardware) |
| Linux graphics stack | The binary actually links `libgtk-4`, `libwebkitgtk-6.0` and `libsoup-3.0`. The deb/rpm dependency line currently names `libwebkit2gtk-4.1-0` / `webkit2gtk4.1` — a different package, a known packaging gap — so a fresh machine can finish installing and still lack the runtime library (see the troubleshooting table below). AppImage expects all three families installed as well |
| Disk | Each PHP version costs one base image plus one baked-extension image, on top of offline cache, backups and data directories — images and cache can be pointed at another disk from Settings |
| Permissions | On Linux with Docker you need to be in the `docker` group; writing `/etc/hosts` needs system elevation (phpo does not install system packages for you and does not change engine privilege settings) |

## Which package to download

Each release publishes these assets. `checksums.txt` is the SHA256 list, a same-named `.sig` per package is the Ed25519 signature, and `manifest.json` is what in-app upgrade uses to pick a package:

| Platform | Asset | How to use it |
|---|---|---|
| Linux (Debian / Ubuntu) | `phpo_<version>_amd64.deb` | `sudo apt install ./phpo_<version>_amd64.deb`; to upgrade, install the new package over it |
| Linux (Fedora / RHEL / openSUSE) | `phpo-<version>.x86_64.rpm` | `sudo rpm -Uvh phpo-<version>.x86_64.rpm` |
| Linux (no package manager) | `phpo-x86_64.AppImage` | `chmod +x` and run it directly; no root, nothing installed |
| Windows | `phpo-setup-x64.exe` | NSIS installer, double-click |
| macOS | `phpo.dmg` | Drag into Applications; afterwards you must clear quarantine (see below) |

> The version reported inside the installer ≡ the release tag ≡ the version in the filename — all three come from one source. To see the current version, check the bottom of the Settings page or [`docs/CHANGELOG.md`](./docs/CHANGELOG.md).

## Installation

One prerequisite: **Docker or Podman must already be running**. If the engine is not up, phpo tells you which one it cannot reach and how to start it — it never draws a fake "installed" face.

### Linux: Docker route

```bash
# 1. Install Docker (official script)
curl -fsSL https://get.docker.com | sh

# 2. Add your user to the docker group
sudo usermod -aG docker $USER

# 3. Log out and back in (required, otherwise the group does not take effect)
#    Verify: docker ps runs without an error

# 4. Download the matching deb / rpm / AppImage from the Releases page
#    https://github.com/xiaokentrl/phpo/releases/latest
```

Docker installed via `apt install docker.io` creates the `docker` group but **does not** add you to it. In that situation phpo now says "no permission to reach the engine" instead of lying that "Docker is not running".

### Linux: Podman route

```bash
# 1. Install Podman
sudo apt install podman        # Debian / Ubuntu
# or sudo dnf install podman   # Fedora

# 2. Enable the user-level socket (this is what Podman is judged by — not whether a socket file exists)
systemctl --user enable --now podman.socket

# 3. Point phpo at it (can also be set as a desktop session environment variable)
export DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock

# 4. Download a package from Releases and install as above
```

Rootless Podman cannot bind ports below 1024, which is why the default site port is 8080. If you really want 80, open privileged ports for the engine: `sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80` (put it in `/etc/sysctl.d/` and it survives the next boot).

### Windows

1. Install and start Docker Desktop first (or Podman).
2. Download `phpo-setup-x64.exe` and double-click it.
3. Launch from the Start menu after installation.

### macOS

```bash
# 1. Make sure Docker Desktop (or colima / Podman machine) is running

# 2. Download phpo.dmg from Releases and drag it into Applications

# 3. Remove quarantine (required: the build is ad-hoc signed, with no Developer ID and no notarization)
sudo xattr -dr com.apple.quarantine /Applications/phpo.app

# 4. Double-click to launch
```

The current CI builds this dmg on a `macos-14` (Apple Silicon) runner; Intel Macs have not been verified.

### Flatpak (FlatPark channel, maintained by a project friend)

phpo does not build a Flatpak. `io.github.xiaokentrl.phpo` is packaged and distributed on the FlatPark side by a friend of the project; **the commands below were not verified on this machine in this round** — if something breaks, open an issue here and tell us. Use this route if you want it in a sandbox.

Two hard sandbox limits up front: `$XDG_RUNTIME_DIR` is private, so without an explicit grant the app cannot see the host engine socket; and there is no `pkexec` inside the sandbox, so `/etc/hosts` must be edited by hand.

```bash
# remotes
flatpak --user remote-add --if-not-exists flatpark https://dl.flatpark.org/flatpark.flatpakrepo
flatpak --user remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo

# install
flatpak install flatpark io.github.xiaokentrl.phpo

# grant the engine and the data directories
flatpak override --user --filesystem=/run/docker.sock io.github.xiaokentrl.phpo
flatpak override --user --filesystem=~/phpo io.github.xiaokentrl.phpo
flatpak override --user --filesystem=~/www io.github.xiaokentrl.phpo

# run
flatpak run io.github.xiaokentrl.phpo
```

Podman + Flatpak needs two more steps: grant the host socket, and add a wrapper that forwards to the host `podman`.

```bash
# replace the 1000 in /run/user/1000 with your own `id -u`
flatpak override --user --filesystem=/run/user/1000/podman/podman.sock io.github.xiaokentrl.phpo
flatpak override --user --env=DOCKER_HOST=unix:///run/user/1000/podman/podman.sock io.github.xiaokentrl.phpo

mkdir -p ~/.var/app/io.github.xiaokentrl.phpo/data/bin
cat > ~/.var/app/io.github.xiaokentrl.phpo/data/bin/podman << 'EOF'
#!/bin/bash
exec /usr/bin/flatpak-spawn --host podman "$@"
EOF
chmod +x ~/.var/app/io.github.xiaokentrl.phpo/data/bin/podman

flatpak override --user --filesystem=/tmp io.github.xiaokentrl.phpo
flatpak override --user --env="PATH=/var/home/$USER/.var/app/io.github.xiaokentrl.phpo/data/bin:/app/bin:/usr/bin" io.github.xiaokentrl.phpo
```

If you change the working root / cache root / backup root in Settings, grant the new paths to the sandbox as well — otherwise phpo can see the path but cannot write to it.

## First run

1. The wizard settles two roots first: the working directory root (default `~/phpo`) and the site root (default `~/www`). **Nothing is created on disk before you click "Confirm and create"** — the verification stage is read-only.
2. If the engine is not ready, the wizard blocks here. Start Docker / Podman as prompted and come back.
3. Ten-minute path: install PHP 8.3 → install Nginx → create site `demo.test` → open a browser and see the page → if you need 7.4, install it too and switch from the site list dropdown.
4. Database services default to password `123456`, which may be left empty; after changing the password or port, click "Rebuild to apply".

## When something goes wrong, check these first

| Symptom | Real cause and what to do |
|---|---|
| It says the engine is not ready, but I installed it | phpo decides by actually dialling. First confirm `docker ps` / `podman ps` works in your own terminal; for Docker you must log back in for the group to take effect; for Podman you need `systemctl --user enable --now podman.socket`. Sandboxed / unusual setups (colima, lima, podman machine, Windows named pipe, `docker context`) are supported too — export `DOCKER_HOST` to point phpo at it when needed |
| Creating a site or changing a port says the port will not work | The UI names the real cause: if something else holds it, it says which process or service; if the engine cannot bind, it says the engine cannot bind and does not fob you off with "it's taken"; if it is outside 1–65535, it tells you the range. When you change an existing site's port it auto-steps to the first usable port and says clearly which port it moved to and how to switch back |
| On Linux it fails to start with `error while loading shared libraries: libwebkitgtk-6.0.so.4` | The machine is missing the GTK4 / WebKitGTK 6.0 / libsoup3 families — the package is not broken. On deb: `sudo apt install libgtk-4-1 libwebkitgtk-6.0-4 libsoup-3.0-0`; on rpm: install the matching gtk4 / webkitgtk 6.0 / libsoup3 runtime libraries (names differ slightly per distro) |
| Double-clicking the app says the developer cannot be verified | The current macOS build is ad-hoc signed and not notarized. Follow step 3 above to remove quarantine |
| In-app upgrade did nothing (Linux) | In-app upgrade on Linux does not work today — install the new package with your package manager instead. A failed upgrade rolls back to the old version automatically, so you are not left half-broken |
| I reinstalled after uninstalling and the data is still there | Intentional: uninstalling keeps data volumes and `~/phpo` by default. To really wipe it, use the cleanup panel and confirm volume deletion item by item — deleting a volume is a second-confirmation action |

## Security notes

- **`config.yaml` stores passwords in plaintext and its permission is normalized to 0777**: that is the accepted cost of the design — other users on the same machine can read that password. Do not put real passwords on a shared machine, and the databases only listen locally.
- **No telemetry**: apart from pulling packages and checking for updates, nothing is sent out on its own initiative.
- **Signatures are verifiable**: every package ships a `.sig`. The chain is "package → SHA256 → Ed25519 signature over the lowercase hexadecimal digest **string**"; the public key is embedded in the app, so an upgrade that cannot be verified is refused outright rather than installing a package of unknown origin.

## Building from source

Toolchain: Go 1.27, Wails v3.0.0-beta.23, Node 22 + pnpm 12. On Linux you also need the GTK4 family of development headers (the app is CGO + WebKit):

```bash
sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev \
  libglib2.0-dev libx11-dev build-essential pkg-config
```

All tasks live in `Taskfile.yml` and run through `wails3 task`:

```bash
wails3 task dev             # dev mode with hot reload
wails3 task build           # compile
wails3 task check           # five CI gates + windows / darwin cross-compile self-check
wails3 task test            # full unit tests
wails3 task package:linux   # deb / rpm / AppImage (on a Linux host)
```

The five gate scripts (mandatory in CI; red blocks the merge): `scripts/check-i18n-keys.go` (key sets across eight languages / placeholders / duplicate keys / unresolved entry lines / escape shapes), `check-templates.go` (template golden, empty diff), `check-docker-naming.go`, `check-cache-manifest.go`, `check-ext-catalog.go` (extension catalog equals the backend list item by item).

Integration tests require a real engine and are skipped by default; run them only under explicit authorization:

```bash
PHPO_LIVE=1 go test ./test/integration/...
```

The frontend language packs hold 1389 keys on each of the 8 sides, with `zh-CN` as the frozen baseline for key names; `zh-TW` is derived by `scripts/gen-zh-tw.mjs` (OpenCC `s2twp`) — do not hand-edit it. Adding a language means adding one locale file.

## Documentation

- [`AGENTS.md`](./AGENTS.md): the highest technical charter, a frozen document, required reading before touching code.
- [`docs/`](./docs/): 32 design and specification documents — architecture, directory conventions, domain model, API contracts, event stream protocol, UI specs, cross-platform differences, the five policies (password / version / domain / path / port), offline caching, backup sanitization, packaging and release, user manual, and more. **These are Chinese-only.**
- [`docs/CHANGELOG.md`](./docs/CHANGELOG.md): per-round changes and verification records.
- [`README.md`](./README.md) is the Chinese original of this file; apart from it, the documentation is Chinese-only.
- On any conflict, `AGENTS.md` wins.

## Known limitations (honest list)

- The WebKit dependency declared in deb/rpm (`libwebkit2gtk-4.1-0` / `webkit2gtk4.1`) is not the family the binary links (6.0): installation completes, but a fresh machine may still lack the runtime library.
- The Windows / macOS packages have not been walked through install → run → uninstall on real machines; the Linux side is verified on real hardware.
- macOS has ad-hoc signing only — no Developer ID, no notarization — so the first launch requires removing quarantine by hand.
- In-app upgrade does not work on Linux; install the new package manually. Update checking and downloading themselves are fine.
- The Flatpak package is maintained by a project friend on the FlatPark side; the phpo repository does not build it, and its install commands were not verified on this machine in this round.
- Real-host GUI walkthroughs still have outstanding gaps; some UI conclusions rest on build artifacts and gate scripts rather than on click-through verification.

## License

MIT — see [`LICENSE`](./LICENSE) for the full text.

## Feedback

- Bugs and feature requests: [Issues](https://github.com/xiaokentrl/phpo/issues) (issue / PR templates are in the repo).
- When reporting a bug, please include: app version, OS and distribution, container engine (Docker / Podman and its version), and the last few lines of the failing task in the log drawer.

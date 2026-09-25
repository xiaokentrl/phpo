# phpo

> A local Docker development environment manager for PHP developers. Desktop app for three platforms — **GUI only, no command line**.
>
> Latest release: **0.1.42** · Download: https://github.com/xiaokentrl/phpo/releases/latest
> The single authority for this project's rules is [AGENTS.md](./AGENTS.md) (Chinese, frozen document). This file is the entry point for "understand the project and use it".

---

## 1. What this thing is for

You write PHP and have three or five projects in flight at once: one WordPress needs PHP 7.4, one Laravel needs PHP 8.3, an old client site still wants MySQL 5.7 while the new one runs 8.4. The traditional approach is to install one PHP, edit config, install another, edit it back — or hand-write docker-compose files.

What phpo does: **it turns "install an environment" into a few button clicks**.

- PHP, MySQL, PostgreSQL, Redis and Nginx versions are **installed side by side and running side by side**, without fighting each other. Each one is a container (named `phpo-{service}-{version}` throughout).
- To switch a site to another PHP version, pick it in a dropdown — the nginx upstream immediately points at exactly `php-{version}-fpm:9000`.
- Install to try, uninstall when done. Uninstalling keeps the data volume; reinstall and your data is still there.
- Offline, intranet, company blocks Docker Hub — everything you installed is in the local cache and can be reinstalled with zero network.

In one sentence: **it isn't yet another PHP bundle — it's a tool that hides the Docker layer for you, while keeping it liftable** (every step is written line by line in the log drawer; nothing is fudged).

### How it differs from similar products

| Others | phpo |
|--------|------|
| One PHP version, switched back and forth | Multiple versions genuinely coexisting, each with its own config, logs and extensions |
| Downloads images for you, needs network every time | **Checks the local cache first**; if the cache misses it looks inside your local Docker; only then does it hit the network. Once installed, it lands in the cache |
| Caches only images | Also caches the **extension package files themselves** (pecl `.tgz`, Alpine `.apk`) — so compiling extensions on another machine, offline, needs no network either |
| Extensions either aren't there or you get a text box | Each PHP version gets a **full extension catalog (73 entries / 8 groups)**, the 11 common ones pre-checked, the compile streamed line by line, and failures name the exact extension |
| On error it says "failed" | Errors carry the reason and the tail of the container log, and tell you how to get back |
| Various "for your own good" restrictions | **Only 8 hard red lines**; everything else passes or downgrades to a warning. Password may be empty, versions are free-form, site roots may live outside WWW_ROOT, you can uninstall every PHP |
| One unreadable file kills the whole backup | Unreadable entries are skipped with aggregated warnings; databases are additionally covered by logical dumps (`mysqldump` / `pg_dumpall` / redis rdb) |

---

## 2. What you need before installing

- **Docker must be running**: Docker Desktop on Windows/macOS, native Docker Engine on Linux. Without Docker, no service can start (one of the hard red lines).
- **One extra step on Linux**: after `sudo apt install docker.io`, also run `sudo usermod -aG docker $USER` and then **log out and back in** (a new terminal is not enough). Skip it and the socket is on disk and the daemon is running, but phpo can't dial it — and phpo will say "the current user has no permission to access Docker" rather than lie that "Docker isn't running".
- The Docker SDK path only: phpo never shells out to the `docker` command, so "docker works in my terminal" does not equal "phpo works" — the criteria differ.
- Port 80 is best left free (default site port). If it's taken nothing fails — that one site is simply created in a degraded state first.

---

## 3. Download and install

Go to https://github.com/xiaokentrl/phpo/releases/latest and pick the file for your system:

| System | File | Notes |
|--------|------|-------|
| Windows | `phpo-setup-x64.exe` | NSIS installer |
| macOS | `phpo.dmg` | **ad-hoc signed; a proper Developer ID and notarization are not wired up yet** — on first launch allow it under "System Settings → Privacy & Security" |
| Linux (deb) | `phpo_0.1.42_amd64.deb` | Debian / Ubuntu |
| Linux (rpm) | `phpo-0.1.42.x86_64.rpm` | RHEL / Fedora family |

Alongside them: `checksums.txt` (SHA256 per package), four `.sig` files (Ed25519 signatures, always 88 bytes), and `manifest.json` (the release manifest used by in-app upgrade).

How to verify each package's signature yourself: see [docs/打包发布.md](./docs/打包发布.md) (Packaging & Release, Chinese).

**First launch runs the setup wizard**, which asks you to choose two directories (both can be anywhere):

- Working root (PHPO_HOME, default `~/phpo`) — per-service config, logs, data and the offline cache all land here.
- Site root (WWW_ROOT, default `~/www`) — one subdirectory per site.

The "Verify" step in the wizard is a **read-only pre-flight**: it only checks whether the directory exists and whether its parent is writable. It creates nothing. Only "Confirm and create" writes to disk. That is a hard design requirement, not a courtesy.

> Every `./` below refers to that working root, not the repository root. It defaults to `~/phpo`, but once you change it, `~` has nothing to do with it.

---

## 4. Running in ten minutes

1. Open phpo and finish the wizard.
2. "PHP" page → click "Install" → version `8.3` → the 11 common extensions are already ticked in the catalog, glance over them if you like → confirm.
   The log drawer (bottom) streams live: whether the cache was hit, the image pull, container creation, which extensions were compiled, and the new frozen image.
3. "Nginx" page → install one (Nginx is a singleton; only one).
4. "Sites" page → Add site → domain `demo.test`, pick the PHP you just installed, leave the port at the default 80, point the root at your project.
5. Open `http://demo.test` in a browser. If the DNS row says "unresolved", click "Add hosts entry" (it elevates; if you decline, the site is unharmed and you just get one line to copy by hand).
6. Another project needs PHP 7.4? Go back to "PHP", install 7.4, switch the dropdown on the site row — it writes to disk and reloads only after `nginx -t` passes.

For databases, install from the "MySQL / PostgreSQL / Redis" pages. The password field defaults to `123456`, **may be cleared and may be as long as you like**; 👁 reveals it, 📋 copies it. Changing a password or port doesn't take effect immediately — those are fixed at container-create time; click "**Rebuild to apply**" on the card (the data volume is kept, the database is not lost).

---

## 5. What's in the UI

The left sidebar has 10 entries (Sites + five service pages + Overview + Backup + Offline cache + Settings; the five service pages share one base component). Counting the "Cleanup" panel makes 11 screens — it opens as a modal from a button on the Overview page, not as its own route. There are 12 view files in the tree (one is the shared base, one is the body of the cleanup panel).

| Page | What it does |
|------|--------------|
| Overview | The whole picture at a glance + **environment doctor** (15 checks: Docker, ports, directory writability, hosts permission, network, cache integrity, temp-dir residue… anything fixable in one click gets a button; the rest are warnings) |
| Sites | Add site, delete site, change port, switch PHP, rewrite rules (9 presets), edit the vhost body by hand |
| PHP / MySQL / PgSQL / Redis / Nginx | Per-service install, start/stop, rebuild, uninstall, config, password, port, data directory |
| Backup | One-click archive (logical DB dump first → pause → snapshot → pack → auto-restart), download, restore, delete |
| Offline cache | Usage and entries, verify all, cleanup in three modes, **manual import** of a single package file, change the cache root |
| Cleanup | Scan orphan containers/volumes/networks/images, clean in three modes (conservative / standard / aggressive) + trash |
| Settings | Passwords and ports, release sources, audit log, tray preferences, check for updates |

**The bottom log drawer is the core of the project, not an accessory.** It's two panes (default 70% / 30%; the splitter is draggable, clamped between 40–80%, double-click resets):

- Left pane: the current operation's **plain-language name** ("Create backup", "Stop PHP 8.4") + live logs for each step + the failure reason. When no task is running, this is the system event feed (cache hits, state drift, new version found — the most recent 200 lines).
- Right pane: the task queue. The most recently submitted is always at the top; each row is labelled "Waiting / Running / Done" (failures and cancellations are labelled honestly too). Queued items can be withdrawn.

The rule is hard: **the backend is the only authority, and the UI never optimistically pretends success**. So you'll see "the button you clicked greys out first, and lights up again once the state genuinely comes back" rather than a spinner that just declares victory. Other buttons on the same page stay clickable — concurrent writes are queued FIFO, which is a legitimate path, and nothing blocks you.

**No `phpo …` fake command line appears anywhere in the UI**: this product has no CLI, so showing a line that looks runnable would only deceive you.

---

## 6. Where things live

| Content | Location |
|---------|----------|
| `config.yaml` (paths, passwords, ports, data dirs, release sources) | The `phpo/` folder inside your OS app-data directory: Linux `~/.config/phpo/`, macOS `~/Library/Application Support/phpo/`, Windows `%APPDATA%\phpo\` |
| `phpo.db` (runtime state only), audit log, trash (7 days), upgrade workspace | Same as above |
| Per-service config/logs/data, offline cache (default `./offline/`), backup archives (default `./backups/`) | Working root `./` (default `~/phpo`, movable anywhere by the wizard) |
| Site directories | Site root (default `~/www`) |
| Theme, language, UI zoom, drawer ratio | Browser local storage — not in config, not in the database |

Three paths can be **customized individually**: the offline cache root, the backup root, and each database version's data directory. The rule is **mutually exclusive and singular** — once you customize, that default path is entirely out of use; two places never coexist and it never reads one while writing the other; clearing the value falls back to the default. The only validation is path safety (traversal defence): absolute paths aren't required, existence isn't required, character sets aren't limited.

**Permissions are always 0777**: every directory and file phpo itself creates is 0777, normalized by an explicit `chmod` after the write (putting 0777 only in the `MkdirAll` argument gets trimmed by umask — that's fake compliance). Old installs left at 0755 get repaired on the next write.
**No overreach**: files written by processes inside the containers (database data dirs, logs) are left alone — that's a different uid's business.
**The cost, stated plainly**: passwords are stored in plaintext, so `config.yaml` is world-readable. That's the accepted price of "built for programmers, we don't restrict your access for you"; the product introduces no encryption, no keyring, no strength validation. On shared machines, use disk encryption or separate accounts yourself.

---

## 7. Network behaviour

Two kinds of outbound traffic, no third kind:

1. **Fetching things**: images, pecl extension packages, Alpine dependency packages. A local cache hit means zero network; a cache miss but the image already present in your local Docker also means zero network (it exports and rebuilds the cache directly).
2. **Checking for updates**: an anonymous GET of the release manifest.

No telemetry, no analytics beacons, no accounts, no usage data uploaded. The signing public key ships inside the binary (`internal/updater/signing/public.key`); a downloaded package must pass both SHA256 and signature verification — either failure refuses the install.

Release sources can be several: `update_sources` in `config.yaml` is an array, and every check **asks all sources concurrently** (15-second timeout each) and **takes the manifest with the highest version number** (mirrors usually lag, so it's not "whoever answers first wins"). It only errors if all fail, and then names each source with its reason. In mainland China you can add a Gitee mirror yourself. **There is no automatic region detection** — the app has no reliable location signal, and guessing wrong is slower.

---

## 8. Running from source

### Dependencies

Go 1.27+ · Node 22+ · pnpm · Wails CLI v3 (`wails3`, currently pinned to `v3.0.0-beta.23`) · Docker (only needed to run a real environment).

Building the desktop binary on Linux needs the GTK4 stack (Wails v3 beta uses GTK4 / webkitgtk-6.0 on Linux):

```bash
apt install libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev libglib2.0-dev libx11-dev
```

That's why the Linux CI runner must be ubuntu-24.04 (22.04 has no `libwebkitgtk-6.0-dev` in its archives).

### Common commands

There is no standalone `task` binary in this repo; everything goes through `wails3 task`:

```bash
wails3 task dev               # development mode (Wails + Vite hot reload)
wails3 task bindings          # regenerate the frontend TS bindings (frontend/bindings/ is generated — never hand-edit)
wails3 task frontend:install  # install frontend dependencies
wails3 task build             # build the binary for the current platform (runs bindings first)
wails3 task test              # go test ./...
wails3 task vet               # static checks
wails3 task check             # the five gates, see below
wails3 task package:linux     # produce deb + rpm locally
wails3 task release:local     # full local release: bump version → check → test → package
```

CI has four more pipelines under `.github/workflows/`, split like this:

| File | What it runs |
|------|--------------|
| `ci.yml` | Two jobs, both on `ubuntu-24.04`: `Go build & test` (install GTK4/webkitgtk deps → build → vet → unit tests → the five gates) and `Frontend build` (install the wails3 CLI → generate bindings → build the frontend) |
| `lint.yml` | Three jobs: `go-lint`, `frontend-lint`, `workflow-lint` (the last one validates the pipeline files themselves) |
| `release.yml` | Runs on `v*` tags (also manually triggerable; the `guard_only` checkbox runs just the signing-key guard): `verify-guard` → three platforms packaging in parallel → collect signatures/checksums and attach them to the Release. **Three-platform builds live here, not in `ci.yml`** |
| `cleanup-actions.yml` | Periodically removes old artifacts |

### The five gates (`wails3 task check`)

All are `scripts/check-*.go`, shared between CI and local runs:

| Script | What it locks down |
|--------|--------------------|
| `check-i18n-keys.go` | The Chinese and English locale files have **equal key sets** (currently 619 keys each), no duplicates |
| `check-templates.go` | The 7 config templates match the frozen prototype verbatim (the single production deviation is annotated with its reason: pgsql logging moved to stderr) |
| `check-docker-naming.go` | Containers/networks/volumes always carry the `phpo-` prefix; nothing may slip |
| `check-cache-manifest.go` | The JSON field names of `manifest.json` are frozen (including the two slots `image` and `extensions_image`) |
| `check-ext-catalog.go` | The frontend catalog's `tool` classification matches the backend `peclExts` **entry by entry** — get the classification wrong and you're "installing a third-party extension with a built-in command", which must fail to compile |

### Tests

- Unit tests live next to their packages (84 `*_test.go` files, fakes/mocks inline, no separate mocks directory). They must be green without Docker — that's a gate-level requirement.
- Real-environment tests are in `test/integration/` (11 `*_live_test.go` files), guarded by a **double skip**: they run only with `PHPO_LIVE=1` *and* Docker available. How to run:

```bash
PHPO_LIVE=1 go test ./test/integration/ -run TestM3 -v
```

### Two traps, up front

1. `frontend/dist/index.html` is a **placeholder tracked in the repo** (so a fresh clone compiles as-is). Restore it after any frontend build, otherwise you'll commit a build artifact.
2. `frontend/bindings/` is generated and gitignored. After changing backend DTOs or facade methods you **must run `wails3 task bindings` first**, otherwise `vue-tsc` is a false green — it type-checks against the old signature.

### Versioning and release guards

The version number has one source of truth: `productVersion` in `wails.json` (`bash scripts/version.sh` prints it, `wails3 task version:bump` increments it and syncs `build/linux/nfpm.yaml`).

`release.yml` has two guards: ① the tag name must equal `v` + the current version; ② the commit the tag points at must be the commit being built. **So every round you release must bump the version first and then push a new tag**, or the release job is rejected.

The signing private key lives in `~/.local/share/phpo-signing/` (never committed, never leaves the machine); CI holds the same key under a secret of that name. `scripts/verify-signing-guard.sh` checks that the committed public key and the private key pair up.

---

## 9. Where the code lives

```
phpo/
├── main.go / app.go        # GUI entry + the only facade exposed to the frontend (app.go is the single exit)
├── internal/               # strictly one-directional inside: assemble → config → model → store → engine → template → preflight → task → service
│   ├── app/                # assembly and event bus (17 event names)
│   ├── config/             # config.yaml, path derivation, customizable roots, extension metadata, path-safety validation
│   ├── model/              # domain model + snapshot shapes
│   ├── store/              # SQLite (runtime state only, lazily created) + 8 migrations + snapshot/audit/trash
│   ├── engine/             # the Docker SDK layer: containers/images/networks/volumes/exec (frame-header stripping)/host⇄container byte channel/calibration/cleaning
│   ├── cache/              # offline cache core: lookup/manifest/promotion/temp dir/SHA256/stats
│   ├── template/           # go:embed config templates (grouped by service, not by version)
│   ├── vhost/              # vhost generation, upstream rewriting, rewrite presets, nginx -t, hosts elevation on three platforms
│   ├── preflight/          # **the single adjudication layer** (19 actions; UI validation is only instant feedback, the final call is here)
│   ├── task/               # three-phase task engine (serial FIFO + cancel + rollback + ledger)
│   ├── service/            # business services; GetState is the authoritative snapshot exit
│   ├── updater/            # check/download/verify/install/rollback (SHA256 + Ed25519)
│   └── util/fs.go          # the single disk-write helper: 0777 + explicit chmod after write
├── pkg/                    # version / port / archive / disk / dockerutil / errs (27 error codes)
├── frontend/src/           # Vue 3.5 + TS 5 + Vite + Pinia: 12 views / 20 business components / 17 api / 8 stores
├── build/                  # packaging resources for three platforms (NSIS script, nfpm config, icons, desktop entry)
├── scripts/                # the five gates + signing/checksum/version helpers
├── test/integration/       # 11 real-environment live tests
├── docs/                   # 32 documents (Chinese)
├── AGENTS.md               # ★ the master plan, frozen; on conflict it wins over this file
└── 前端唯一界面来源.txt       # single-file HTML prototype = the UI's source of truth (SSOT, never hand-edit)
```

A few trade-offs the design genuinely cares about:

- **Every write goes through three phases**: preflight adjudication → task execution → state change applied. No bypass.
- **Temp directories must be cleared**: the host temp directory for extension compilation, `./{kind}/{version}/ext/`, is cleared on success, failure, cancellation and at startup-scan — never persisted across tasks. The in-container staging directory `/tmp/phpo-ext` must be cleared **before** `docker commit`, or the package files get frozen into the image layer and can never be removed.
- **Offline caching has three iron rules**: always check before installing, a hit means zero network, temp directories must be cleared. Caching and Docker resources are fully decoupled — uninstalling a service doesn't touch the cache, clearing the cache doesn't affect data.
- **External deletions are only named, never rewritten into the database**: whatever you removed via Docker Desktop or `docker rm`, "Sync status" detects it, flags the card in warning colour and spells out in the log which piece is missing — but it will **not** flip that version to "not installed" on its own, and it won't secretly rebuild anything. Because the config, volume and cache are all still there: click "Enable" and it rebuilds from the current config.

---

## 10. FAQ

| Symptom | What's going on |
|---------|-----------------|
| `docker info` works in the terminal but phpo says it can't connect | Read the raw reason in parentheses: `permission denied` → you're not in the `docker` group, and after adding yourself you must **log out and back in**; can't reach the daemon → `systemctl start docker`. Also, when launched from a desktop icon the process does not see the `DOCKER_HOST` you `export`ed in a shell (rootless and Docker Desktop on Linux sockets are already in the auto-detection chain) |
| Install fails (intranet/offline) | Check whether the currently effective cache root holds that version. Holding only a single `image.tar` or extension package? Register it via "Import" on the Offline cache page (**it copies; your original stays put**). No cache and not in local Docker either — an offline machine genuinely can't install it |
| Changing the cache/backup root seems to do nothing | ① Is the "customized" marker on the path bar showing (no marker = still on the default root); ② changing a root while a task runs is refused outright — wait for an empty queue; ③ a data directory needs "Rebuild to apply" before the mount switches |
| "Enable" fails with "expected running=true, actual false" | The container came up but the process exited itself (typically pgsql's old config writing logs into a host bind-mount directory and being denied → crash loop). This case now **self-heals on enable**, with one explanatory log line; config you edited yourself is not touched, only flagged for review. If it still fails, the error carries the exit code and the last 5 lines of container log — act on that |
| Files missing from a backup | No longer fatal: unreadable entries are skipped and the log lists what's missing, aggregated per directory; database contents are covered by the logical dumps under `dump/` |
| Database empty/stale after a restore | Restore **replays the cold copy only; it never auto-replays dumps** (auto-applying SQL would overwrite your existing database — an irreversible risk). For cross-machine recovery where you need the data, import that `.sql` / `.rdb` from the archive by hand |
| Port already in use | New site: warning only, the site is still created in a degraded state, your port is kept as typed — free the port or change it once and it self-heals. Changing an existing site's port: automatically advances to the first free port in 1–65535. Service port: error, change it yourself |
| I clicked "Apply and rebuild" for extensions and the dialog vanished instantly | Expected. That's a background task taking tens of seconds; the dialog closes on submit and progress plus failures live in the log drawer. Next time you open "Manage extensions", the pre-checked set is exactly the last set that **succeeded** |
| I want a clean slate | Aggressive cleanup on the Cleanup page (volumes kept by default; deleting a volume asks twice); deleted site directories go to the trash and stay 7 days |

---

## 11. What isn't done yet (stated honestly)

- **The Windows / macOS packages have only ever been built successfully in CI; they've never been installed on a real machine.** Silent NSIS installation, uninstall registry entries, shortcut placement, and the .dmg's actual behaviour under Gatekeeper are still paper inferences. The Linux deb/rpm have been installed, upgrade-over-installed and rollback-checked on this machine.
- **macOS is ad-hoc signed**; a proper Developer ID and notarization are not wired up, and Windows has no code-signing certificate.
- **In-app auto-upgrade does not work on Linux**: the installer is implemented around AppImage replacement semantics, while releases only ship deb/rpm. This is registered as an unlanded item in AGENTS.md §11. Linux users should re-download from the Releases page for now.
- **A batch of real-host GUI walkthroughs is still owed**: UI behaviour has extensive unit tests and build-artifact evidence, but click-level acceptance inside native windows hasn't been systematically completed.
- **There is no LICENSE file**: the licence terms are not finalised, so until then treat this document as "all rights reserved". Third-party dependency licences are under `third_party/licenses/`.
- No plugin system, no CLI, no password encryption. These three are deliberate choices, not things we ran out of time for.

---

## 12. Document map

Go deeper on any topic in `docs/` (32 documents, all in Chinese). The ones people reach for:

| What you want to know | Read this |
|-----------------------|-----------|
| Product positioning, user personas, hard red lines | [项目概述](./docs/项目概述.md) (Project Overview) |
| The exact behaviour of every screen and dialog | [界面规格](./docs/界面规格.md) (UI Spec) |
| Which backend capabilities are callable, the event protocol | [接口契约](./docs/接口契约.md) (Interface Contract) · [事件流协议](./docs/事件流协议.md) (Event Stream Protocol) |
| Why changing a root must be mutually exclusive and singular | [路径策略](./docs/路径策略.md) (Path Policy) |
| Why a new site's port doesn't advance but a changed one does | [端口策略](./docs/端口策略.md) (Port Policy) |
| What the cache directory looks like, manifest fields | [离线缓存机制](./docs/离线缓存机制.md) · [缓存清单规范](./docs/缓存清单规范.md) · [离线缓存协议](./docs/离线缓存协议.md) |
| Why passwords/versions are stored the way they are | [密码策略](./docs/密码策略.md) · [版本策略](./docs/版本策略.md) · [备份脱敏规范](./docs/备份脱敏规范.md) |
| How containers/volumes avoid leaving dirt behind | [Docker操作规范](./docs/Docker操作规范.md) · [资源清洁机制](./docs/资源清洁机制.md) · [回收站机制](./docs/回收站机制.md) |
| Where the three platforms differ | [跨平台差异](./docs/跨平台差异.md) (Cross-platform Differences) |
| How to package, sign and release | [打包发布](./docs/打包发布.md) (Packaging & Release) |
| What rules code changes must follow | [编码行为准则](./docs/编码行为准则.md) + [AGENTS.md §3.4](./AGENTS.md) |
| What changed | [CHANGELOG](./docs/CHANGELOG.md) |

**Read [AGENTS.md](./AGENTS.md) before changing this project.** It is a frozen document: the tech stack may not drift, numbers may not be edited from memory, and when a clause contradicts the code you stop and report it rather than quietly working around it.
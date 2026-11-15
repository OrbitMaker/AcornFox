<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://acornfox.com/assets/brand/lockup-horizontal-dark.svg">
    <img src="https://acornfox.com/assets/brand/lockup-horizontal.svg" alt="AcornFox" height="48">
  </picture>
</h1>

[中文](README.md)

AcornFox is an open-source deployment platform for your own Linux server. Ask an AI coding tool such as Claude Code or Codex to deploy, and AcornFox builds and runs the app on your server, manages its databases, domains and HTTPS, and reports failures as structured diagnoses the AI can act on.

> Supported platforms in this release:
>
> - **Linux server**: fully supported. The AcornFox service and the command-line tool are both installed on the server.
> - **Mac**: the command-line tool (CLI) only, used as a client that manages a server over SSH.
> - **Windows**: no program in this release. SSH into the server and run `acornfox` there (this is also what AI coding tools do).
> - Running the AcornFox service locally on a Mac or Windows computer is not available in this release.
>
> The user guide (Chinese) is in [docs/user-guide.md](docs/user-guide.md).

[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)](https://go.dev)

## Features

- **Deploy from anywhere**: a local folder, a Git repository or a Docker image
- **Built for AI coding tools**: deploy by asking in plain language
- **Structured diagnoses**: a failed deploy says which step stopped, why and what to change, so the AI can fix it; `acornfox faults CODE` explains any fault code
- **Add-on services**: add PostgreSQL, MySQL or Redis with one command
- **Logs and metrics**: follow logs live and see resource usage
- **Automatic HTTPS**: certificates for your domains (in mainland China the domain needs an ICP filing)
- **App center**: install ready-made apps from a signed catalog; what an app needs (a model API, a mail server and so on) is registered as a "service" and plugged in when it installs
- **Works in mainland China**: registry mirrors, cloud-internal package sources and timeout diagnoses

## Who it is for

- **Individual developers** running their own projects on a cloud server
- **Small teams** spinning up test environments and demos
- **AI users** who want to deploy by talking to an AI assistant
- **Learners** getting hands-on with Docker, containers and deployment

## Quick start

### Recommended: send one message to your AI coding tool

Get a Linux server you can log in to over SSH (see "Server requirements" below), then send this in Claude Code, Codex or a similar tool:

```text
Please install the AcornFox Skill: download https://acornfox.com/skill/SKILL.md and save it as acornfox/SKILL.md in your skills folder (~/.claude/skills for Claude Code, ~/.codex/skills for Codex). Then follow the Skill to deploy this project to my server.
```

The AI installs the Skill first. The Skill then works out separately whether your computer and your server are in mainland China or elsewhere, picks Gitee or GitHub as the download source, installs the CLI and the server, and deploys.

> **Cloud security group**: apps are reached at `http://SERVER_IP:port`, with ports assigned from 18810-18899. Allow inbound TCP 18810-18899 in your cloud provider's security group; binding a domain with HTTPS also needs 80 and 443.

### Server requirements

| Item | Requirement |
| --- | --- |
| System | Ubuntu 22.04 or 24.04, Debian 12 or 13 (x86_64; fresh install, self-check and deployment tested on all four). Ubuntu 24.04 or Debian 13 recommended. arm64 is not tested yet |
| Size | At least 2 CPU cores and 2 GB of memory; root or sudo required |
| Ports | 18800, 18801, 18807 (used by AcornFox itself, local only) and 18810-18899 (app ports) must be free; the installer stops otherwise and this cannot be skipped. 80 and 443 should preferably be free; if BaoTa, 1Panel or Nginx already uses them, see the note below. 18802 is the LAN console; if it is in use, the LAN console is simply not opened |

> **When buying a cloud server in mainland China, choose Ubuntu 24.04 (or Debian 13).** Alibaba Cloud Linux (Alibaba Cloud's default), OpenCloudOS / TencentOS (Tencent Cloud's default), veLinux (Volcano Engine's default), CentOS, Rocky and similar systems are not supported; the installer stops with fault code `INSTALL-OS-01`. Ubuntu or Debian versions outside the list above (for example Ubuntu 20.04 or Debian 11) stop with `INSTALL-OS-02`; switch to a supported version.

> **Ports 80 and 443 already in use (BaoTa, 1Panel, your own Nginx)**: the installer stops (fault code `INSTALL-PORT-02`). To keep your existing sites, add `--allow-busy-80-443` to install alongside them: `curl … | sudo bash -s -- --allow-busy-80-443`. Apps are still reached at `http://SERVER_IP:port`, but you cannot bind domains, enable HTTPS or open the public console in AcornFox; to use a domain, reverse-proxy it to `127.0.0.1:app-port` in your existing panel.

### Manual install

#### 1. Install the server

```bash
# Outside mainland China
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash
# Mainland China (Gitee mirror)
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash
```

When it finishes, the installer shows an admin token (also saved in `/root/.acornfox-token`). Keep it safe.

If the server is on your home or office network (a private address, not a cloud server), the first install opens the LAN console at `http://SERVER_LAN_ADDRESS:18802`; you set its password in the browser the first time you open it. To leave it closed, add `--no-lan-console`: `curl … | sudo bash -s -- --no-lan-console`. It is never opened on cloud servers.

- On Alibaba Cloud, Tencent Cloud and Volcano Engine (any region), the installer uses that provider's internal package sources first (system packages and the Docker installer), so nothing goes over the public internet or counts against traffic, and falls back to the next source if they cannot be reached. Huawei Cloud and other servers in mainland China measure several domestic public mirrors and use the fastest.
- Afterwards, as the installer reminds you, allow inbound TCP 18810-18899 (app ports) in the security group, plus 80 and 443 if you bind a domain.

#### 2. Install the CLI (Linux or Mac computer)

```bash
# Outside mainland China
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
# Mainland China
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
```

There is no command-line program for Windows in this release. Do steps 2-4 over SSH on the server instead (`acornfox` is already installed there); see the [user guide](docs/user-guide.md#windows-电脑经-ssh-在服务器上操作).

#### 3. Connect to the server

```bash
acornfox target add my-server --ssh user@your-server-ip
```

#### 4. Deploy your first app

```bash
cd your-project
acornfox deploy
```

Check the status after deploying:

```bash
acornfox status
```

Once the status is `live`, open the URL it shows.

## App center

The App center installs ready-made apps from a signed catalog. Click Install in the web console's App center, or use the CLI:

```bash
acornfox catalog list                # apps and recipes in the catalog
acornfox install APP --plan          # see the plan first: which services it needs and what is missing (read-only)
acornfox install APP                 # install one app
acornfox install --recipe RECIPE     # install the apps of a recipe, connected
acornfox services list               # the services on this server and which apps use them
acornfox open APP                    # open it from your computer through an SSH tunnel
```

- The catalog has apps and recipes (a recipe installs several apps in order and connects them). What is in it is whatever `acornfox catalog list` and the console's App center show.
- **From v0.6.0 only apps in the new manifest format can be installed.** The catalog has been published in the new format, and servers that are online pick it up by themselves after the upgrade (run `acornfox catalog sync` to see it right away). Apps listed in the old format can be installed again once they are listed in the new format; apps already installed are not affected and keep running. The catalog built into the program has no apps: on a server that has never synced the new catalog (for example one with no internet access), an install says the App center has no app to install yet; once it is online, `acornfox catalog sync` (or the daily sync) fixes that.
- **Services** (from v0.6.0): some apps need something else to do their whole job, such as a model API or a mail server for notifications. Each app says which services it needs (required or optional) and which it provides once installed. Services provided by installed apps are registered by themselves; one you already have (your own mail server, a model API key from a provider) you register with `acornfox services add` (secret items are typed in, not written on the command line; secret install settings can be typed in the same way with `--set KEY=-`). An install plugs in a service of the right type; when a required one is missing it stops and says how to register one. AcornFox never installs another app to provide a service; a recipe is how several apps are installed together.
- Apps installed by v0.5.x used the old manifest format: they keep running and can be started, stopped and deleted, but are not plugged into services (`acornfox status APP` and the console's app details say so); to use the new features, wait until the app is listed again in the new format, then delete it (keeping the data) and install it again from the catalog.
- An install checks memory, disk space and CPU architecture, generates passwords, downloads the image, starts the app, runs the health check and does the first-time setup. If any step fails, everything it created is cleaned up.
- When an app plugs into a service another app provides, the provider often issues it its own credential (for example a token); AcornFox does that by the steps in the app's manifest, keeps it encrypted, shows only its last 4 characters, and takes it back when the consuming app is deleted with its data.
- Large images come through the AcornFox download gateway, with progress shown and resumable downloads; if the gateway fails, the image is pulled from public registries.
- Installed apps listen only on the server itself by default. Open one with `acornfox open APP` on your computer, or, if the server is on your local network, switch it to LAN access in the console (this opens the port on all of the server's network interfaces; the console asks you to confirm).
- The first-login password is masked in the output; see it in the console's app details or with `acornfox install status TASK_ID --show-secrets`.
- `acornfox delete APP` keeps the app's data by default. `acornfox install APP --reuse-data` reinstalls it on the old data with the same accounts and passwords (`acornfox install --name NAME --reuse-data` works too when you do not remember which catalog app it was). Upgrading an installed app to a newer catalog version is done the same way in this release: delete, then reinstall with `--reuse-data`. The reinstall also keeps the settings you had changed, the services it was plugged into and the config files in the data, so you do not have to repeat the earlier options; `acornfox apps vars APP` shows the settings an app was installed with.

## AI coding tool integration

The Skill is the entry point to AcornFox. On first use it installs the CLI and the server; after that it deploys, reads diagnoses and fixes problems, and manages databases and the app lifecycle.

### Installing the Skill

- **No CLI yet**: send the message from "Quick start" to your AI coding tool; it downloads the Skill from `https://acornfox.com/skill/SKILL.md`.
- **CLI already installed**: `acornfox skill install` installs the Skill matching your CLI version.

| Tool | How |
| --- | --- |
| Claude Code | Installed automatically to `~/.claude/skills/acornfox/` |
| Codex | Installed automatically to `~/.codex/skills/acornfox/` |
| Other tools | `acornfox skill install --dir FOLDER`, or print it with `acornfox skill print` |

### Example

```
You: Deploy this project to my server

AI:  Dockerfile generated, deploying.
     [health] The container exited or kept restarting after start
     Log excerpt: Error: DATABASE_URL is not set
AI:  The diagnosis shows a missing database. Adding PostgreSQL and redeploying.
     Live at http://SERVER_IP:18810
```

## Core commands

```bash
# Deploy
acornfox deploy                              # deploy the current folder
acornfox deploy --git https://github.com/... # deploy a Git repository
acornfox deploy --image nginx:latest         # deploy a Docker image

# Status
acornfox status                              # app status
acornfox apps                                # list all apps
acornfox logs --app my-app -f                # follow logs

# Lifecycle
acornfox stop/start/restart --app my-app     # stop or start an app
acornfox redeploy --app my-app               # redeploy
acornfox rollback --to 5 --app my-app        # roll back to version 5 (acornfox versions lists what you can roll back to)
acornfox delete my-app                       # delete an app (data is kept unless you add --volumes)

# Add-on services
acornfox add postgres --app my-app           # add PostgreSQL
acornfox add mysql --app my-app              # add MySQL
acornfox add redis --app my-app              # add Redis

# Domains
acornfox domain add example.com --app my-app # bind a domain (automatic HTTPS)

# App center
acornfox install APP --plan                  # the plan: services it needs and what is missing (read-only)
acornfox install APP                         # install an app from the catalog
acornfox install status TASK_ID --wait       # wait until the install finishes (up to 90 seconds; run again if not done)
acornfox services add smtp --name my-mail --set host=mail.example.com --set password=-   # register a service you have (the password is typed in)
acornfox services list                       # services on this server, their state and who uses them
acornfox apps vars APP                       # the settings an app was installed with
acornfox open APP                            # open a local-only app through an SSH tunnel

# When something goes wrong
acornfox faults CAT-INST-24                  # what a fault code means and what to do (read-only, works offline)
sudo acornfox doctor                         # self-check on the server
```

## Architecture

AcornFox has three simple layers:

- **AcornFox**: state and coordination
- **Docker**: container runtime
- **Caddy**: HTTP routing and HTTPS (upstream Caddy, run inside the entry container `acornfox-edge`)

Design principles:
- **State separated from execution**: the server has no Docker access; the runner accepts only typed requests
- **Reconcile, do not orchestrate**: record what should be, and let the runner converge to it
- **App independence**: apps are managed by Docker, so an AcornFox crash does not affect running apps
- **The diagnosis is the product**: failures come back as structured diagnoses the AI can act on

## Notes for mainland China

### Domains and ICP filing

- **Domains with an ICP filing**: HTTPS certificates are obtained automatically and the app is served on 80/443
- **Domains without a filing**: you get a notice when binding, and the app stays reachable at IP:port
- **IP access**: not affected by filing rules, always available

### Network

The installer reads the cloud provider's metadata to find out which cloud the server is on and whether the region is in mainland China, then configures:
- System packages (APT) and the Docker installer: servers on Alibaba Cloud, Tencent Cloud and Volcano Engine use that provider's internal sources first (in every region, including Hong Kong and Singapore), falling back to the next source if they cannot be reached
- Servers in mainland China (including Huawei Cloud) then measure several domestic mirrors on the spot (Alibaba Cloud, Tsinghua, USTC, Nanjing University, SJTU, Huawei Cloud, Tencent Cloud) and use the fastest one that works; servers elsewhere use Docker's official repository
- Pulling images: sources come in layers (the cloud provider's internal mirror, then public domestic mirrors, then Docker Hub or ghcr.io itself). In mainland China every source is measured before a pull: the ones that refuse or do not answer are skipped, slow ones go last, and a pull that becomes too slow moves to the next source. `acornfox sources --probe` shows how each source does right now
- Hong Kong, Singapore, Tokyo and other regions outside the mainland are treated as overseas: downloads come from GitHub, without domestic mirrors or registry mirrors
- The entry proxy image comes with the release as an entry package, downloaded from a domestic source (Gitee); no image registry is used
- If the cloud cannot be identified, the server's IP location decides whether it is in mainland China

If the guess is wrong, choose explicitly (passing options to the script through the pipe):
```bash
# Force mainland China mirrors
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- --china

# Force international sources
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- --global
```

## Documentation

The documents below are in Chinese.

- [User guide](docs/user-guide.md)
- [FAQ](docs/user-guide.md#常见问题)
- [AI Skill](skills/acornfox/SKILL.md)
- [Capabilities](docs/capabilities.md)
- [Fault codes](docs/faults.md)

What AcornFox connects to and what it sends is described at [acornfox.com/en/privacy.html](https://acornfox.com/en/privacy.html).

## Upgrading

```bash
# Upgrade the server (outside mainland China)
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade
# Upgrade the server (mainland China)
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade

# Upgrade the CLI (Linux or Mac computer; outside mainland China)
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
# Upgrade the CLI (Linux or Mac computer; mainland China)
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
```

Running the script through a pipe (including when an AI coding tool runs it for you) counts as agreeing to the upgrade; the script first lists what it will do. If you are already on the latest version it exits without stopping services or reinstalling, so running it again is harmless. Add `-y` (`| sudo bash -s -- -y`) only to reinstall the same version.

What the upgrade does:
- Downloads the new version and checks its SHA256 first; if the download fails, the existing install is untouched
- Stops the AcornFox service, backs up the program and database (in `/var/lib/acornfox/backups/`, keeping the latest 5), then runs the database migration
- If the migration fails, the new version will not start, or the upgrade is interrupted, everything is restored to how it was before (program and database, as if the upgrade never happened)
- App containers keep running throughout; the CLI and console are briefly unavailable during the upgrade
- When the entry proxy container is replaced (for example on the first upgrade to v0.6.0), new requests are not affected, but open long-lived connections, requests still running after 8 seconds and WebSockets are dropped once; servers still on v0.3.x must upgrade to v0.5.1 first
- Upgrading never opens the LAN console; one that was already open keeps working

AcornFox itself only upgrades and never goes back to an older release: there is no manual rollback, and asking for an older version is refused. Rolling back your own deployed apps with `acornfox rollback` works as before. See the [user guide](docs/user-guide.md#只能升级).

### Uninstalling

Download the uninstall script to the server and run it as root:

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh -o get.sh
sudo bash get.sh uninstall            # keeps /var/lib/acornfox and the data volumes
sudo bash get.sh uninstall --purge    # also deletes all data; cannot be undone
```

In mainland China, download it from `https://gitee.com/VIP13390/AcornFox/raw/main/get.sh`. Docker itself is not removed.

## Contributing

Contributions of code and documentation, and suggestions, are welcome.

1. Fork this repository
2. Create a feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push the branch (`git push origin feature/amazing`)
5. Open a pull request

See the [contributing guide](CONTRIBUTING.md) for details.

## License

AcornFox is licensed under AGPL-3.0; see [LICENSE](LICENSE).

## Acknowledgements

AcornFox builds on:

- [Docker](https://www.docker.com/): container runtime
- [Caddy](https://caddyserver.com/): HTTP server and automatic HTTPS
- [SQLite](https://www.sqlite.org/): embedded database
- All contributors and users

## Contact

- Issues: [GitHub Issues](https://github.com/OrbitMaker/AcornFox/issues)
- Docs: https://github.com/OrbitMaker/AcornFox/tree/main/docs

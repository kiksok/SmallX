# smallX

`smallX` is a lightweight Xboard-compatible backend project with a small control plane and an install flow aimed at simple self-hosting.

Current version: `v0.5.0`

The goal is not to embed a full protocol core like XrayR does. Instead, this project keeps the control layer small and focuses on:

- fetching node config from Xboard
- fetching user lists from Xboard
- reporting traffic, online IPs, and node status back to Xboard
- exposing a tiny runtime adapter interface so an external core can be plugged in later

## Why this shape

After comparing XrayR, Xboard UniProxy, and Soga v1 WebAPI, the real control-plane overlap is small:

- pull node config
- pull users
- read route rules from node config when the panel includes them
- push traffic
- push alive IPs
- push node status

That means we can keep the management binary tiny and move protocol-specific work into adapters.

## Current scope

This first scaffold includes:

- config loader
- provider abstraction
- Xboard provider implementation with ETag support
- alignment with `cedar2025/Xboard` `UniProxy` semantics, where route rules are included in the node config payload
- sync agent loop
- dry-run runtime adapter
- SS prototype translator for Xboard Shadowsocks nodes, including:
  - AEAD ciphers
  - Shadowsocks 2022 user-key derivation
  - `simple-obfs-http` style plugin parsing

This means the control plane already compiles and can talk to Xboard.

## Current Verified Scope

The currently verified runtime path is:

- Linux AMD64
- `ss-native`
- `Shadowsocks aes-256-gcm`
- TCP forwarding
- UDP forwarding
- UDP FullCone-style mapping behavior
- Xboard `UniProxy` control-plane pull/push
- Per-user TCP connection limits
- Single-machine IP/device limit enforcement with hard reject of excess devices
- Active device counting based on admitted device activity
- Local allow/block target rules
- Remote Xboard block route enforcement
- Real `speed_limit` enforcement from Xboard user settings

The following items are still planned rather than fully verified in production:

- `aes-128-gcm`
- `aes-192-gcm`
- `chacha20-ietf-poly1305`
- `simple_obfs_http`
- Shadowsocks 2022

## Next steps

The intended next phase is:

1. add a real runtime adapter with a native SS TCP+UDP data plane
2. translate Xboard UniProxy config into runtime-specific config
3. collect per-user traffic and alive IPs from the runtime
4. support audit rules and optional Soga-style provider compatibility

## Run

```bash
go run ./cmd/smallx -config ./config.example.yaml
```

Print version:

```bash
go run ./cmd/smallx -version
```

## Config

See [config.example.yaml](./config.example.yaml).

## One-Command Install

After this repository is pushed to GitHub, a server can be provisioned with:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/kiksok/SmallX/main/scripts/install.sh) \
  --panel-url https://your-panel.example.com \
  --token your-xboard-server-token \
  --node-id 1 \
  --node-type shadowsocks
```

To install a tagged version:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/kiksok/SmallX/main/scripts/install.sh) \
  --ref v0.2.0 \
  --panel-url https://your-panel.example.com \
  --token your-xboard-server-token \
  --node-id 1 \
  --node-type shadowsocks
```

Useful policy flags:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/kiksok/SmallX/main/scripts/install.sh) \
  --ref v0.3.0 \
  --panel-url https://your-panel.example.com \
  --token your-xboard-server-token \
  --node-id 1 \
  --node-type shadowsocks \
  --default-tcp-conn-limit 8 \
  --enforce-device-limit true \
  --allow-targets "example.com,api.example.com" \
  --block-targets "ads.example.com"
```

The installer will:

- install or upgrade Go if needed
- clone or update `smallX` into `/opt/smallx`
- build `/usr/local/bin/smallx`
- write `/etc/smallx/config.yaml`
- install and start `smallx.service`

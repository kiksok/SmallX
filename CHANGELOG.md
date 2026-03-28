# Changelog

## v0.3.1

- Fix device limit enforcement so excess TCP devices are disconnected immediately
- Fix device limit enforcement so excess UDP devices are dropped immediately
- Avoid reporting over-limit devices as online before admission succeeds
- Add service-level TCP and UDP device limit tests

## v0.3.0

- Add UDP FullCone-style mapping test coverage
- Add per-user TCP connection limit support
- Add single-machine IP/device limit enforcement based on Xboard `device_limit`
- Add local allow/block target rules
- Add remote Xboard block route enforcement
- Add installer flags for connection limit and target allow/block policies
- Verify the updated `ss-native` path on the live Linux AMD64 test server

## v0.2.0

- Verify `smallX` on a real Linux AMD64 test server
- Confirm live `Shadowsocks aes-256-gcm` TCP forwarding through Xboard node `1`
- Confirm live `Shadowsocks aes-256-gcm` UDP forwarding through Xboard node `1`
- Keep `ss-native` as the default runtime adapter
- Confirm systemd deployment and public port listen on the test machine

## v0.1.0

- Rename project branding to `smallX`
- Keep GitHub sync workflow in place
- Add Xboard control-plane integration scaffold
- Add Shadowsocks config translation layer
- Add native Shadowsocks service scaffold for TCP+UDP runtime work
- Add one-command install script for Linux AMD64
- Add versioning files and startup version output

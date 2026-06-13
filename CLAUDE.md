# aruba-aos — Agent Operating Guide

Native OpenTofu/Terraform provider for **ArubaOS-Switch (AOS-S)** via REST API
v8. Sibling of `../openwrt-ubus` (same generic-over-the-API philosophy, same
toolchain). The workspace-root `../CLAUDE.md` applies; this adds specifics.

## What this is / isn't

- **Is:** a provider for AOS-S (ProVision-era 2530/2920/2930F, 16.x firmware),
  driven entirely through the documented `/rest/v8` REST API (cookie auth).
- **Isn't:** an ArubaOS-CX provider. CX has `aruba/terraform-provider-aoscx`.
  Do not pull CX concepts (NETCONF, declarative config replace) in here.

## Design tenets

General Go/provider standards: see `/home/jameson/source/ai-prompts/go.md` (§8).

- **The generic resources here are `arubaos_object` (+ data source)** — they
  address any REST path. Resist adding typed resources until there's a real
  ergonomics need (todo 4.1).
- **The subset plan modifier is `subsetMatches`**; `body` is the keys we manage.
- **PUT is idempotent** on AOS-S; create = POST to `create_path` (collection) or
  PUT to `path` (upsert). Singletons (`system`, `stp`, `dns`, `lldp`) use
  `delete_method = "NONE"`.

## Toolchain

General Go/provider standards: see `/home/jameson/source/ai-prompts/go.md` (§7, §10).

- Go 1.26.4 (`/home/jameson/.local/go`), `terraform-plugin-framework` v1.19.0.
- Provider address: `registry.terraform.io/JamesonRGrieve/aruba-aos`.

## Hard rules

General Go/provider standards: see `/home/jameson/source/ai-prompts/go.md` (§7, §8).

- **No secrets in the repo.** Creds come from the provider config (OpenBao →
  `TF_VAR_*` via Semaphore). The lab switch lives at `192.168.2.210`.
- **The target is a production backbone switch** (OPNsense LAG on Trk3, every
  VLAN). Drive changes via Semaphore.
- Reuse `../openwrt-ubus`'s vetted dependency versions.

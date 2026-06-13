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

- **Generic, not per-feature.** `arubaos_object` (+ data source) address any
  REST path → 100% coverage by construction. Resist adding typed resources
  until there's a real ergonomics need (todo 4.1); never as the *only* way to
  reach a feature.
- **Manage-declared-only.** `body` is the keys we manage; state holds the full
  device object; the subset plan modifier (`subsetMatches`) suppresses the diff
  when declared keys already match. This is what makes imports land at 0-diff
  and stops the provider clobbering unmanaged device fields. Do not "fix" a
  spurious diff by widening what we store — fix the subset logic and test it.
- **PUT is idempotent** on AOS-S; create = POST to `create_path` (collection) or
  PUT to `path` (upsert). Singletons (`system`, `stp`, `dns`, `lldp`) use
  `delete_method = "NONE"`.

## Toolchain

- Go 1.26.4 (`/home/jameson/.local/go`), `terraform-plugin-framework` v1.19.0.
- Provider address: `registry.terraform.io/JamesonRGrieve/aruba-aos`.
- `make check` = tidy + fmt + vet + test + build (pre-commit gate; enable hooks
  with `git config core.hooksPath .githooks`).

## Hard rules

- **No secrets in the repo.** Creds come from the provider config (OpenBao →
  `TF_VAR_*` via Semaphore). The lab switch lives at `192.168.2.210`.
- **The target is a production backbone switch** (OPNsense LAG on Trk3, every
  VLAN). Validate with read-only GETs and `tofu plan`; never apply by hand —
  drive via Semaphore, additive-only, in a change window.
- Reuse `../openwrt-ubus`'s vetted dependency versions; don't gratuitously bump.

<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
# terraform-provider-aruba-aos

A native OpenTofu/Terraform provider for **ArubaOS-Switch (AOS-S)** switches —
the ProVision/ProCurve-lineage 2530 / 2920 / 2930F running 16.x firmware — via
the **REST API v8** (HTTPS, cookie-session auth).

> **Not ArubaOS-CX.** The official `aruba/terraform-provider-aoscx` targets the
> newer AOS-CX line (6300/8320/…) and does **not** manage AOS-S switches. AOS-S
> has no upstream provider; this fills that gap. If you have a CX switch, use
> the official one.

## Why generic

AOS-S exposes a broad, stable REST surface (`/rest/v8/...`): singletons
(`system`, `stp`, `dns`, `lldp`, `snmp-server`, `syslog`) and collections
(`vlans/{vid}`, `vlans-ports/{vid}-{port}`, `ports/{id}`,
`snmp-server/communities/{name}`, `stp/ports/{id}`, …). Rather than hand-code a
resource per feature (and chase firmware additions forever), this provider is
**generic over the API** — one resource and one data source address *any* path.
That is **100% feature coverage** by construction.

## Resources

### `arubaos_object` (resource)

CRUD + `ImportState` for any addressable AOS-S resource.

```hcl
resource "arubaos_object" "vlan_iot" {
  path        = "vlans/40"   # GET/PUT/DELETE target
  create_path = "vlans"      # POST here on create (omit to create via PUT path)
  body        = jsonencode({ vlan_id = 40, name = "IOT" })
}

resource "arubaos_object" "system" {
  path          = "system"
  delete_method = "NONE"     # singleton — cannot be deleted
  body          = jsonencode({ name = "house-aruba-2530" })
}
```

**Manage-declared-only / 0-diff imports.** `body` declares *only* the keys you
manage. State holds the full device object; a plan modifier suppresses the diff
when every declared key already matches the device, so:

- importing an existing resource (`tofu import` / `import {}` block) lands at
  **0-diff** with no apply against the switch, and
- the provider never clobbers device fields you didn't declare.

| Attribute | | Meaning |
|-----------|---|---------|
| `path` | required, ForceNew | addressed path under `/rest/v8` (leading slash optional) |
| `body` | required | JSON object of the keys you manage |
| `create_path` | optional, ForceNew | collection to `POST` to on create; omit → create via idempotent `PUT path` |
| `delete_method` | optional | `DELETE` (default), `PUT` (send `delete_body` — reset a singleton), or `NONE` |
| `delete_body` | optional | reset body for `delete_method = "PUT"` |
| `id` | computed | equals `path` |

### `arubaos_object` (data source)

```hcl
data "arubaos_object" "vlans" { path = "vlans" }   # .response is raw JSON
```

## Provider configuration

```hcl
terraform {
  required_providers {
    arubaos = { source = "registry.terraform.io/jamesonrgrieve/aruba-aos" }
  }
}

provider "arubaos" {
  host     = "192.168.2.210"     # no scheme
  username = var.switch_user
  password = var.switch_password # sensitive
  insecure = true                # AOS-S self-signed cert (default true)
}
```

## Local build / dev install

```sh
make build          # -> terraform-provider-aruba-aos
make install        # installs to $DEV_BIN_DIR for a dev_overrides .tfrc
make check          # tidy + fmt + vet + test + build (pre-commit / CI gate)
```

For runners without registry access, install into a filesystem mirror:
`<plugins>/registry.terraform.io/JamesonRGrieve/aruba-aos/<ver>/<os>_<arch>/terraform-provider-aruba-aos`
and point a `.terraformrc` `provider_installation { filesystem_mirror {...} }` at it.

## License

AGPL-3.0-or-later.

// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/JamesonRGrieve/aruba-aos/internal/aos"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*objectResource)(nil)
	_ resource.ResourceWithConfigure   = (*objectResource)(nil)
	_ resource.ResourceWithImportState = (*objectResource)(nil)
)

// NewObjectResource constructs the generic aruba_aos_object resource.
func NewObjectResource() resource.Resource { return &objectResource{} }

type objectResource struct {
	client *aos.Client
}

// objectModel is the state/plan shape for aruba_aos_object.
type objectModel struct {
	ID           types.String `tfsdk:"id"`
	Path         types.String `tfsdk:"path"`
	CreatePath   types.String `tfsdk:"create_path"`
	DeleteMethod types.String `tfsdk:"delete_method"`
	DeleteBody   types.String `tfsdk:"delete_body"`
	Body         types.String `tfsdk:"body"`
}

func (r *objectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object"
}

func (r *objectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A generic ArubaOS-Switch REST resource addressed by its `/rest/v8` path. " +
			"Covers 100% of the AOS-S API: any singleton (`system`, `stp`, `dns`, `lldp`) or " +
			"collection item (`vlans/40`, `vlans-ports/40-3`, `ports/5`, `snmp-server/communities/public`). " +
			"`body` declares only the keys this resource manages; device-returned keys outside `body` are " +
			"ignored for drift, so a subset declaration imports to 0-diff and never clobbers unmanaged fields.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resource id — equal to `path`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"path": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Addressed resource path under `/rest/v8` (leading slash optional), " +
					"used for GET/PUT/DELETE. E.g. `vlans/40`, `system`, `vlans-ports/40-3`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"create_path": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Collection path to POST to on create (e.g. `vlans` while `path` is `vlans/40`). " +
					"When unset, create is an idempotent PUT to `path`. Carry it in the import id " +
					"(`<path>|<create_path>`) so an imported resource matches config and lands at 0-diff.",
			},
			"delete_method": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "How to destroy: `DELETE` (default), `PUT` (send `delete_body` to `path` — " +
					"reset a singleton to default), or `NONE` (no-op for un-deletable singletons). Carry it in the " +
					"import id (`<path>|<create_path>|<delete_method>`) to match config.",
			},
			"delete_body": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "JSON body PUT to `path` on destroy when `delete_method = \"PUT\"`. Import id field 4.",
			},
			"body": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "JSON object of the declared (managed) attributes. State holds the full " +
					"device object; drift is detected only on these keys.",
				PlanModifiers: []planmodifier.String{subsetSuppress{}},
			},
		},
	}
}

func (r *objectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*aos.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *aos.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

// normPath ensures a leading slash.
func normPath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// parentCollection returns the collection path for an item path by dropping the
// last segment: "/vlans-ports/58-41" -> "/vlans-ports", "/vlans/58" -> "/vlans".
// Returns "" for a top-level singleton (no parent).
func parentCollection(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return ""
	}
	return p[:i]
}

func (r *objectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := []byte(m.Body.ValueString())
	if !json.Valid(body) {
		resp.Diagnostics.AddError("Invalid body", "`body` must be valid JSON")
		return
	}
	var err error
	if !m.CreatePath.IsNull() && m.CreatePath.ValueString() != "" {
		// Explicit collection POST (e.g. /vlans).
		_, err = r.client.Post(normPath(m.CreatePath.ValueString()), body)
	} else {
		// Idempotent PUT to the item path; if the item doesn't exist yet
		// (AOS-S replies 404 to PUT on a not-yet-present collection item, e.g.
		// a new vlans-ports membership), fall back to POSTing the parent
		// collection. This makes the generic resource handle both upsert-PUT
		// singletons and POST-create collections without an explicit create_path.
		p := normPath(m.Path.ValueString())
		_, err = r.client.Put(p, body)
		if err != nil && aos.NotFound(err) {
			if parent := parentCollection(p); parent != "" {
				_, err = r.client.Post(parent, body)
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("AOS-S create failed", err.Error())
		return
	}
	m.ID = m.Path
	// Store the declared body verbatim so the create plan/state are consistent;
	// the next refresh (Read) replaces it with the full device object.
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := r.client.Get(normPath(m.Path.ValueString()))
	if err != nil {
		if aos.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("AOS-S read failed", err.Error())
		return
	}
	// Store the full device object (compacted). The subset plan modifier
	// reconciles it against the declared config body at plan time.
	compact, err := compactJSON(raw)
	if err != nil {
		resp.Diagnostics.AddError("AOS-S read: invalid JSON from device", err.Error())
		return
	}
	m.Body = types.StringValue(compact)
	m.ID = m.Path
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := []byte(m.Body.ValueString())
	if !json.Valid(body) {
		resp.Diagnostics.AddError("Invalid body", "`body` must be valid JSON")
		return
	}
	if _, err := r.client.Put(normPath(m.Path.ValueString()), body); err != nil {
		resp.Diagnostics.AddError("AOS-S update failed", err.Error())
		return
	}
	m.ID = m.Path
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	method := "DELETE"
	if !m.DeleteMethod.IsNull() && m.DeleteMethod.ValueString() != "" {
		method = strings.ToUpper(m.DeleteMethod.ValueString())
	}
	var err error
	switch method {
	case "NONE":
		// Singleton that cannot be deleted (e.g. /system); just drop from state.
	case "PUT":
		if m.DeleteBody.IsNull() {
			resp.Diagnostics.AddError("delete_method=PUT requires delete_body", "no reset body provided")
			return
		}
		_, err = r.client.Put(normPath(m.Path.ValueString()), []byte(m.DeleteBody.ValueString()))
	default: // DELETE
		_, err = r.client.Delete(normPath(m.Path.ValueString()))
		if err != nil && aos.NotFound(err) {
			err = nil // already gone
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("AOS-S delete failed", err.Error())
	}
}

func (r *objectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import id is a pipe-delimited tuple so the imported state matches the
	// config's operational hints exactly (→ 0-diff, no spurious update/replace):
	//   <path>[|<create_path>[|<delete_method>[|<delete_body>]]]
	// Empty fields are treated as null. Body is populated on the following Read.
	parts := strings.Split(req.ID, "|")
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[0])...)
	setOpt := func(p string, i int) {
		if i < len(parts) && parts[i] != "" {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(p), parts[i])...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(p), types.StringNull())...)
		}
	}
	setOpt("create_path", 1)
	setOpt("delete_method", 2)
	setOpt("delete_body", 3)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("body"), "{}")...)
}

// ---------------------------------------------------------------------------
// subset plan modifier — suppress diff when every declared key already matches
// the full device object held in prior state. This is what lets a subset
// `body` import/refresh to 0-diff without clobbering unmanaged device fields.
// ---------------------------------------------------------------------------

type subsetSuppress struct{}

func (subsetSuppress) Description(context.Context) string {
	return "Suppress diff when all declared JSON keys already match the device object in state."
}
func (subsetSuppress) MarkdownDescription(context.Context) string {
	return (subsetSuppress{}).Description(nil)
}

func (subsetSuppress) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return // create — nothing to reconcile against
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	// All declared (config) keys already match the device object in prior state:
	// keep the full prior object and show no diff. Otherwise leave the planned
	// (config) value in place so the drift surfaces as an update.
	if subsetMatches(req.StateValue.ValueString(), req.ConfigValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

// subsetMatches reports whether every top-level key in the config JSON object
// is present in the prior JSON object with a structurally-equal value (config
// is a value-subset of prior). Invalid JSON on either side returns false so the
// caller falls back to a normal diff.
func subsetMatches(prior, cfg string) bool {
	var p, c map[string]json.RawMessage
	if json.Unmarshal([]byte(prior), &p) != nil {
		return false
	}
	if json.Unmarshal([]byte(cfg), &c) != nil {
		return false
	}
	for k, cv := range c {
		pv, ok := p[k]
		if !ok || !jsonEqual(cv, pv) {
			return false
		}
	}
	return true
}

// jsonEqual compares two raw JSON values structurally (order-insensitive).
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// compactJSON re-serializes raw JSON in compact, key-sorted-by-encoder form.
func compactJSON(raw []byte) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

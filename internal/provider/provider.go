// SPDX-License-Identifier: AGPL-3.0-or-later

// Package provider implements the aruba-aos OpenTofu/Terraform provider — a
// native client for the ArubaOS-Switch (AOS-S) REST API v8. It is generic over
// the API surface (the aruba_aos_object resource/data source address any
// /rest/v8 path), giving 100% feature coverage without per-feature code.
package provider

import (
	"context"

	"github.com/JamesonRGrieve/aruba-aos/internal/aos"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*aosProvider)(nil)

// New returns the provider factory for a given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &aosProvider{version: version} }
}

type aosProvider struct {
	version string
}

type providerModel struct {
	Host     types.String `tfsdk:"host"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Insecure types.Bool   `tfsdk:"insecure"`
}

func (p *aosProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	// Single-token type name -> resources are `arubaos_object`, so Terraform's
	// prefix-before-first-underscore inference resolves the local name cleanly
	// (the source address is still jamesonrgrieve/aruba-aos).
	resp.TypeName = "arubaos"
	resp.Version = p.version
}

func (p *aosProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Native provider for ArubaOS-Switch (AOS-S) switches (2530/2920/2930F, 16.x) " +
			"via the REST API v8. Not for ArubaOS-CX — use aruba/aoscx for those.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Switch address (host or host:port), no scheme.",
			},
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "AOS-S operator/manager username.",
			},
			"password": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "AOS-S password.",
			},
			"insecure": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Skip TLS verification (default true — AOS-S ships a self-signed cert). " +
					"Set false only with a trusted cert installed.",
			},
		},
	}
}

func (p *aosProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	insecure := true
	if !cfg.Insecure.IsNull() && !cfg.Insecure.IsUnknown() {
		insecure = cfg.Insecure.ValueBool()
	}
	client := aos.NewClient(aos.Config{
		Host:     cfg.Host.ValueString(),
		Username: cfg.Username.ValueString(),
		Password: cfg.Password.ValueString(),
		Insecure: insecure,
	})
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *aosProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewObjectResource}
}

func (p *aosProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewObjectDataSource}
}

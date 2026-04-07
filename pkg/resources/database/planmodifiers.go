package database

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time assertion that setRequiresReplaceModifier satisfies the
// planmodifier.Set interface.
var _ planmodifier.Set = setRequiresReplaceModifier{}

// setRequiresReplaceModifier is a plan modifier that triggers resource
// replacement whenever a set attribute changes.
//
// The terraform-plugin-framework ships a `setplanmodifier.RequiresReplace`
// helper upstream, but it is not vendored in this repository, so we provide a
// minimal equivalent here.
type setRequiresReplaceModifier struct{}

// setRequiresReplace returns a plan modifier that will force resource
// replacement on any change to a set attribute.
func setRequiresReplace() planmodifier.Set {
	return setRequiresReplaceModifier{}
}

func (m setRequiresReplaceModifier) Description(_ context.Context) string {
	return "If the value of this attribute changes, Terraform will destroy and recreate the database service (including all data on it)."
}

func (m setRequiresReplaceModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m setRequiresReplaceModifier) PlanModifySet(_ context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	// Do not replace on resource creation.
	if req.State.Raw.IsNull() {
		return
	}

	// Do not replace on resource destroy.
	if req.Plan.Raw.IsNull() {
		return
	}

	// Do not replace when the planned value is unknown (e.g. it references
	// a computed attribute of another resource that has not been applied
	// yet). The framework will call us again once the value is resolved.
	if req.PlanValue.IsUnknown() {
		return
	}

	// Do not replace if the plan and state values are equal.
	if req.PlanValue.Equal(req.StateValue) {
		return
	}

	resp.RequiresReplace = true
}

// Compile-time assertion that integrationsSelfSourceValidator satisfies
// the validator.Set interface.
var _ validator.Set = integrationsSelfSourceValidator{}

// integrationsSelfSourceValidator rejects an `integrations` set where any
// element points at the resource declaring it — e.g. a `read_replica`
// integration whose `source_service` equals the resource's own `name`.
// Such a configuration can never succeed server-side and only surfaces as
// a late, expensive apply-time failure without this validator.
type integrationsSelfSourceValidator struct{}

// integrationsSelfSource returns a validator that rejects integrations
// referencing the declaring resource as their source.
func integrationsSelfSource() validator.Set {
	return integrationsSelfSourceValidator{}
}

func (v integrationsSelfSourceValidator) Description(_ context.Context) string {
	return "Ensures no element of the `integrations` set points at the resource it is declared on (source_service != self.name)."
}

func (v integrationsSelfSourceValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v integrationsSelfSourceValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var selfName types.String
	if diags := req.Config.GetAttribute(ctx, path.Root("name"), &selfName); diags.HasError() {
		// If we can't read the resource's name (e.g. it's unknown or the
		// attribute path is different in a future schema), skip — the
		// framework's own validators will catch a missing name.
		return
	}
	if selfName.IsNull() || selfName.IsUnknown() {
		return
	}

	var models []ResourceDbaasIntegrationModel
	if diags := req.ConfigValue.ElementsAs(ctx, &models, false); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	for _, m := range models {
		if m.SourceService.IsNull() || m.SourceService.IsUnknown() {
			continue
		}
		if m.SourceService.ValueString() == selfName.ValueString() {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid integration source",
				fmt.Sprintf(
					"An integration references %q as source_service, which is the same service this resource declares. Integrations must be declared on the destination side, with source_service pointing at a different service.",
					m.SourceService.ValueString(),
				),
			)
			return
		}
	}
}

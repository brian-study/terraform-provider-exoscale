package database

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

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
	return "If the value of this attribute changes, Terraform will destroy and recreate the resource."
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

	// Do not replace if the plan and state values are equal.
	if req.PlanValue.Equal(req.StateValue) {
		return
	}

	resp.RequiresReplace = true
}

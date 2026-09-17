package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// serviceNameNote explains the one thing users get wrong about service names.
//
// They usually look like domains, which invites the assumption that a DNS zone
// name will do. It often will, because a hosting service is frequently named
// after a domain, but the two are different namespaces and nothing guarantees
// they match.
const serviceNameNote = "The webhosting service this belongs to. This is a virtual server name, " +
	"which often looks like a domain but is a different namespace from a DNS zone — use the " +
	"`zone_vservers` data source to list the valid values. Changing it moves the resource to " +
	"another service, which means replacing it."

// serviceNameAttribute is the service_name argument every /vserver resource
// takes. It deliberately has no FQDN validation: a service name is not a
// hostname and need not resolve.
func serviceNameAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		Required:            true,
		MarkdownDescription: serviceNameNote,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		Validators:          []validator.String{noPathSeparator()},
	}
}

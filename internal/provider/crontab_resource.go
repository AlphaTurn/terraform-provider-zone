package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlphaTurn/terraform-provider-zone/internal/zoneapi"
)

// NewCrontabResource returns the scheduled job resource.
func NewCrontabResource() resource.Resource { return &crontabResource{} }

type crontabResource struct {
	resourceClient
}

var (
	_ resource.Resource                   = (*crontabResource)(nil)
	_ resource.ResourceWithConfigure      = (*crontabResource)(nil)
	_ resource.ResourceWithImportState    = (*crontabResource)(nil)
	_ resource.ResourceWithValidateConfig = (*crontabResource)(nil)
)

type crontabResourceModel struct {
	ServiceName  types.String `tfsdk:"service_name"`
	ID           types.Int64  `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Command      types.String `tfsdk:"command"`
	Schedule     types.String `tfsdk:"schedule"`
	ExecType     types.String `tfsdk:"exec_type"`
	ScheduleType types.String `tfsdk:"schedule_type"`
	Report       types.String `tfsdk:"report"`
	ReportEmail  types.String `tfsdk:"report_email"`
	Nice         types.String `tfsdk:"nice"`
	Timezone     types.String `tfsdk:"timezone"`
	RuntimeLimit types.Int64  `tfsdk:"runtime_limit"`
	Active       types.Bool   `tfsdk:"active"`
	ResourceURL  types.String `tfsdk:"resource_url"`
}

func (r *crontabResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_crontab"
}

func (r *crontabResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A scheduled job on a webhosting service.\n\n" +
			"~> **The argument names here come from the live API, not from zone.eu's published " +
			"description, which disagrees with it.** `OPTIONS` on the endpoint reports `exec_type`, " +
			"`nice` and `schedule_type`; the document calls the first two `type` and `priority` and " +
			"does not mention the third. No account available for testing had a crontab, so no read " +
			"could settle it and settling it by writing one would have created a job on a " +
			"production service. Reads accept either spelling; writes use these. Please report what " +
			"happens.",
		Attributes: map[string]schema.Attribute{
			"service_name": serviceNameAttribute(),
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric identifier assigned by zone.eu.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name for the job, shown in the ZoneID panel.",
			},
			"command": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "What to run: an absolute path for a `system` job, or a URL to " +
					"fetch for an `http` one.\n\n" +
					"zone.eu substitutes a few variables in a system command, which is how to " +
					"invoke the right interpreter without hardcoding a path: `[[$PHP]]` for the " +
					"current PHP, `[[$PHP84]]` and friends for a specific one, and " +
					"`[[$ACC_HOMEDIR_A]]` for the service's home directory.",
			},
			"schedule": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "When to run it, as a cron expression such as `15 3 * * *`.\n\n" +
					"This is not validated locally: zone.eu accepts forms a home-grown parser " +
					"would reject, so an unaccepted schedule is reported against this argument by " +
					"the API rather than guessed at here.",
			},
			"exec_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "How to run the job: `system` to execute a command on the " +
					"server, or `http` to fetch a URL.",
				Validators: []validator.String{
					stringvalidator.OneOf(zoneapi.CrontabExecTypes...),
				},
			},
			"schedule_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("basic"),
				MarkdownDescription: "How `schedule` is interpreted: `basic` or `raw`. Absent from " +
					"zone.eu's published description entirely; reported by the live API.",
				Validators: []validator.String{
					stringvalidator.OneOf(zoneapi.CrontabScheduleTypes...),
				},
			},
			"report": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("onerror"),
				MarkdownDescription: "When to email about a run: `never`, `onerror`, `onoutput`, " +
					"`onoutputorerror` or `always`.",
				Validators: []validator.String{
					stringvalidator.OneOf(zoneapi.CrontabReports...),
				},
			},
			"report_email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Where to send those reports.",
				Validators:          []validator.String{isEmailAddress()},
			},
			"nice": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("normal"),
				MarkdownDescription: "Scheduling priority: `low`, `normal` or `high`. zone.eu's " +
					"published description calls this `priority` and omits `high`.",
				Validators: []validator.String{
					stringvalidator.OneOf(zoneapi.CrontabNiceLevels...),
				},
			},
			"timezone": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Which timezone `schedule` is read in: `UTC`, `Europe/Tallinn`, " +
					"`Europe/Amsterdam` or `Europe/Helsinki`. Only valid when `exec_type` is " +
					"`system`; zone.eu defaults to `" + zoneapi.CrontabDefaultTimezone + "`.",
				Validators: []validator.String{
					stringvalidator.OneOf(zoneapi.CrontabTimezones...),
				},
			},
			"runtime_limit": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "How long a run may take before it is killed, in seconds: " +
					"`900`, `1800`, `3600`, `10800` or `86340`. Only valid when `exec_type` is " +
					"`system`.",
				Validators: []validator.Int64{
					int64validator.OneOf(zoneapi.CrontabRuntimeLimits...),
				},
			},
			"active": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the job runs. Set it to `false` to keep it without running it.",
			},
			"resource_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "API URL of this job.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateConfig enforces the two arguments that only apply to a system job.
//
// The API rejects them on an HTTP job, and catching it here names the fix
// instead of producing a 422 after an apply has started.
func (r *crontabResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config crontabResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !known(config.ExecType) || config.ExecType.ValueString() == "system" {
		return
	}

	for _, restricted := range []struct {
		attribute string
		set       bool
	}{
		{"timezone", known(config.Timezone)},
		{"runtime_limit", known(config.RuntimeLimit)},
	} {
		if !restricted.set {
			continue
		}
		resp.Diagnostics.AddAttributeError(
			path.Root(restricted.attribute),
			fmt.Sprintf("%s only applies to a system job", restricted.attribute),
			fmt.Sprintf(
				"zone.eu accepts %s only when exec_type is \"system\", and this job is %q.\n\n"+
					"Remove %s, or change exec_type to \"system\".",
				restricted.attribute, config.ExecType.ValueString(), restricted.attribute,
			),
		)
	}
}

func (r *crontabResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan crontabResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	created, err := r.client.CreateCrontab(ctx, service, crontabInput(plan))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not create the scheduled job", err, crontabAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromCrontab(service, created, plan))...)
}

func (r *crontabResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state crontabResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := state.ServiceName.ValueString()
	job, err := r.client.GetCrontab(ctx, service, state.ID.ValueInt64())
	if err != nil {
		if zoneapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIError(&resp.Diagnostics, "Could not read the scheduled job", err, crontabAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromCrontab(service, job, state))...)
}

func (r *crontabResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state crontabResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	service := plan.ServiceName.ValueString()
	updated, err := r.client.UpdateCrontab(ctx, service, state.ID.ValueInt64(), crontabInput(plan))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not update the scheduled job", err, crontabAttributes)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, modelFromCrontab(service, updated, plan))...)
}

func (r *crontabResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state crontabResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCrontab(ctx, state.ServiceName.ValueString(), state.ID.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Could not delete the scheduled job", err, crontabAttributes)
	}
}

// ImportState accepts "<service name>/<job id>".
func (r *crontabResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := importParts(&resp.Diagnostics, req.ID, 2, "<service name>/<job id>", "virt1.example.com/31")
	if !ok {
		return
	}

	id, err := parseImportInt64(&resp.Diagnostics, parts[1])
	if !err {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// crontabAttributes maps the API's field names, in both spellings, onto this
// schema's arguments.
var crontabAttributes = map[string]string{
	"name":          "name",
	"command":       "command",
	"schedule":      "schedule",
	"exec_type":     "exec_type",
	"type":          "exec_type",
	"schedule_type": "schedule_type",
	"report":        "report",
	"report_email":  "report_email",
	"nice":          "nice",
	"priority":      "nice",
	"timezone":      "timezone",
	"runtime_limit": "runtime_limit",
	"active":        "active",
}

func crontabInput(plan crontabResourceModel) zoneapi.CrontabInput {
	input := zoneapi.CrontabInput{
		Name:         plan.Name.ValueString(),
		Command:      plan.Command.ValueString(),
		Schedule:     plan.Schedule.ValueString(),
		ExecType:     plan.ExecType.ValueString(),
		ScheduleType: plan.ScheduleType.ValueString(),
		Report:       plan.Report.ValueString(),
		ReportEmail:  plan.ReportEmail.ValueString(),
		Nice:         plan.Nice.ValueString(),
		Active:       plan.Active.ValueBool(),
	}

	// Both are only accepted on a system job, and ValidateConfig has already
	// refused them elsewhere.
	if known(plan.Timezone) {
		input.Timezone = plan.Timezone.ValueString()
	}
	if known(plan.RuntimeLimit) {
		limit := plan.RuntimeLimit.ValueInt64()
		input.RuntimeLimit = &limit
	}
	return input
}

func modelFromCrontab(service string, job *zoneapi.Crontab, desired crontabResourceModel) crontabResourceModel {
	// Each of these falls back to the configured value, because the field
	// names the API echoes are not certain — see the resource description.
	text := func(returned string, wanted types.String) types.String {
		if returned == "" && known(wanted) {
			return wanted
		}
		return optionalString(returned)
	}

	runtimeLimit := int64PointerValue(job.RuntimeLimit)
	if job.RuntimeLimit == nil && known(desired.RuntimeLimit) {
		runtimeLimit = desired.RuntimeLimit
	}

	return crontabResourceModel{
		ServiceName:  types.StringValue(service),
		ID:           types.Int64Value(job.ID),
		Name:         text(job.Name, desired.Name),
		Command:      text(job.Command, desired.Command),
		Schedule:     text(job.Schedule, desired.Schedule),
		ExecType:     text(job.ExecType, desired.ExecType),
		ScheduleType: text(job.ScheduleType, desired.ScheduleType),
		Report:       text(job.Report, desired.Report),
		ReportEmail:  text(job.ReportEmail, desired.ReportEmail),
		Nice:         text(job.Nice, desired.Nice),
		Timezone:     text(job.Timezone, desired.Timezone),
		RuntimeLimit: runtimeLimit,
		Active:       types.BoolValue(job.Active),
		ResourceURL:  optionalString(job.ResourceURL),
	}
}

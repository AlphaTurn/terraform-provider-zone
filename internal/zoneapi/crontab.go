package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// Crontab is a scheduled job on a webhosting service.
//
// The field names here are the ones the live API uses, which are not the ones
// its published description uses. OPTIONS /vserver/{service}/crontab reports
// exec_type, nice and schedule_type; the document calls the first two type and
// priority and does not mention the third at all. No account on hand has a
// crontab, so no GET could settle it, and settling it by writing one would mean
// creating a job on a production service.
//
// So: writes use the OPTIONS names, because those came from the live API, and
// reads accept either spelling. If the tolerance turns out to be unnecessary it
// costs a few lines; if the names are wrong it is the difference between a
// working resource and a 422 on every apply.
type Crontab struct {
	ID           int64
	Name         string
	Command      string
	Schedule     string
	ExecType     string
	ScheduleType string
	Report       string
	ReportEmail  string
	Nice         string
	Timezone     string
	RuntimeLimit *int64
	Active       bool
	ResourceURL  string
}

// Enumerated values, from OPTIONS on the live API rather than from the
// published description, which disagrees with it.
var (
	// CrontabExecTypes is how a job is run.
	CrontabExecTypes = []string{"http", "system"}
	// CrontabScheduleTypes is how its schedule is expressed.
	CrontabScheduleTypes = []string{"basic", "raw"}
	// CrontabReports is when zone.eu emails about a run.
	CrontabReports = []string{"never", "onerror", "onoutputorerror", "onoutput", "always"}
	// CrontabNiceLevels is the scheduling priority. The document omits "high".
	CrontabNiceLevels = []string{"low", "normal", "high"}
	// CrontabTimezones is the set of timezones accepted for a system job.
	CrontabTimezones = []string{"UTC", "Europe/Tallinn", "Europe/Amsterdam", "Europe/Helsinki"}
	// CrontabRuntimeLimits is the set of accepted runtime caps, in seconds.
	CrontabRuntimeLimits = []int64{900, 1800, 3600, 10800, 86340}
)

// CrontabDefaultTimezone is what zone.eu applies when a system job names none.
const CrontabDefaultTimezone = "Europe/Tallinn"

func (c *Crontab) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := decodeFields(data, &fields); err != nil {
		return err
	}

	c.ID, _ = coerceInt64(fields["identificator"])
	for target, field := range map[*string]string{
		&c.Name:        "name",
		&c.Command:     "command",
		&c.Schedule:    "schedule",
		&c.Report:      "report",
		&c.ReportEmail: "report_email",
		&c.Timezone:    "timezone",
		&c.ResourceURL: "resource_url",
	} {
		*target, _ = coerceString(fields[field])
	}

	// Either spelling, live API first.
	c.ExecType = firstField(fields, "exec_type", "type")
	c.Nice = firstField(fields, "nice", "priority")
	c.ScheduleType = firstField(fields, "schedule_type")

	c.Active, _ = coerceBool(fields["active"])
	c.RuntimeLimit = coerceOptionalInt64(fields["runtime_limit"])
	return nil
}

// firstField returns the first of several field names that carries a value.
func firstField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		if value, ok := coerceString(fields[name]); ok && value != "" {
			return value
		}
	}
	return ""
}

// CrontabInput is a job as submitted.
//
// runtime_limit and timezone are pointers so they can be left out entirely: the
// API only accepts them on a system job, and sending either on an HTTP job is a
// validation failure rather than a harmless no-op.
type CrontabInput struct {
	Name         string `json:"name"`
	Command      string `json:"command"`
	Schedule     string `json:"schedule"`
	ExecType     string `json:"exec_type"`
	ScheduleType string `json:"schedule_type,omitempty"`
	Report       string `json:"report,omitempty"`
	ReportEmail  string `json:"report_email,omitempty"`
	Nice         string `json:"nice,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	RuntimeLimit *int64 `json:"runtime_limit,omitempty"`
	Active       bool   `json:"active"`
}

func crontabCollectionPath(service string) string { return vserverPath(service) + "/crontab" }

func crontabItemPath(service string, id int64) string {
	return crontabCollectionPath(service) + "/" + strconv.FormatInt(id, 10)
}

// ListCrontabs returns a service's scheduled jobs.
func (c *Client) ListCrontabs(ctx context.Context, service string) ([]Crontab, error) {
	raws, err := c.list(ctx, crontabCollectionPath(service))
	if err != nil {
		return nil, err
	}
	return decodeList(raws, func(job *Crontab) string { return strconv.FormatInt(job.ID, 10) })
}

// GetCrontab reads one job from the service's cached listing.
func (c *Client) GetCrontab(ctx context.Context, service string, id int64) (*Crontab, error) {
	jobs, err := c.ListCrontabs(ctx, service)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		if jobs[i].ID == id {
			return &jobs[i], nil
		}
	}
	return nil, fmt.Errorf("zone.eu: crontab %d on service %q: %w", id, service, ErrNotFound)
}

// CreateCrontab schedules a job.
func (c *Client) CreateCrontab(ctx context.Context, service string, input CrontabInput) (*Crontab, error) {
	defer c.invalidateList(crontabCollectionPath(service))

	var created Crontab
	if err := c.doOne(ctx, http.MethodPost, crontabCollectionPath(service), input, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateCrontab replaces a job.
func (c *Client) UpdateCrontab(ctx context.Context, service string, id int64, input CrontabInput) (*Crontab, error) {
	defer c.invalidateList(crontabCollectionPath(service))

	var updated Crontab
	if err := c.doOne(ctx, http.MethodPut, crontabItemPath(service, id), input, &updated); err != nil {
		return nil, err
	}
	if updated.ID == 0 {
		updated.ID = id
	}
	return &updated, nil
}

// DeleteCrontab unschedules a job.
func (c *Client) DeleteCrontab(ctx context.Context, service string, id int64) error {
	defer c.invalidateList(crontabCollectionPath(service))

	if _, err := c.do(ctx, http.MethodDelete, crontabItemPath(service, id), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

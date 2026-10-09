package graphhelper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/models/odataerrors"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

const (
	syncCategory = "CalSync"
	syncPropID   = "String {6f1d1c0e-8a42-4d0a-9f55-2c7b1e3a9d10} Name CalSyncKey"
)

type Config struct {
	TenantID string
	ClientID string

	// Name of the persistent token cache. Must be identical for Login and New.
	CacheName string // default "graphhelper"
	// Absolute path (the service's working dir is not what you expect).
	AuthRecordPath string

	CalendarID string // optional; empty = your default calendar
	TimeZone   string // IANA name, default "Europe/Brussels"
}

func (c Config) validateAuth() error {
	if c.TenantID == "" || c.ClientID == "" || c.AuthRecordPath == "" {
		return errors.New("graphhelper: tenant id, client id and auth record path are required")
	}
	return nil
}

type Event struct {
	Subject  string
	Body     string
	Location string
	Start    time.Time
	End      time.Time
}

type Calendar struct {
	ID   string
	Name string
}

type Helper struct {
	client     *msgraphsdk.GraphServiceClient
	calendarID string
	loc        *time.Location
}

func New(ctx context.Context, cfg Config) (*Helper, error) {
	if cfg.CacheName == "" {
		cfg.CacheName = "graphhelper"
	}
	if cfg.TimeZone == "" {
		cfg.TimeZone = "Europe/Brussels"
	}
	if err := cfg.validateAuth(); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("graphhelper: load time zone: %w", err)
	}

	record, err := loadRecord(cfg.AuthRecordPath)
	if err != nil {
		return nil, err
	}
	cred, err := newCredential(cfg, record, true)
	if err != nil {
		return nil, err
	}

	if _, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: tokenScopes}); err != nil {
		return nil, mapAuthError(err)
	}

	client, err := msgraphsdk.NewGraphServiceClientWithCredentials(
		cred, []string{"Calendars.ReadWrite"})
	if err != nil {
		return nil, fmt.Errorf("graphhelper: client: %w", err)
	}

	return &Helper{client: client, calendarID: cfg.CalendarID, loc: loc}, nil
}

func (h *Helper) CreateEvent(ctx context.Context, ev Event) (string, error) {
	if ev.Subject == "" {
		return "", errors.New("graphhelper: subject is required")
	}
	if !ev.End.After(ev.Start) {
		return "", errors.New("graphhelper: end must be after start")
	}

	const layout = "2006-01-02T15:04:05"
	tz := h.loc.String()

	e := models.NewEvent()
	e.SetSubject(&ev.Subject)

	if ev.Body != "" {
		bodyType := models.TEXT_BODYTYPE
		body := models.NewItemBody()
		body.SetContentType(&bodyType)
		body.SetContent(&ev.Body)
		e.SetBody(body)
	}

	startStr := ev.Start.In(h.loc).Format(layout)
	start := models.NewDateTimeTimeZone()
	start.SetDateTime(&startStr)
	start.SetTimeZone(&tz)
	e.SetStart(start)

	endStr := ev.End.In(h.loc).Format(layout)
	end := models.NewDateTimeTimeZone()
	end.SetDateTime(&endStr)
	end.SetTimeZone(&tz)
	e.SetEnd(end)

	if ev.Location != "" {
		loc := models.NewLocation()
		loc.SetDisplayName(&ev.Location)
		e.SetLocation(loc)
	}

	var (
		created models.Eventable
		err     error
	)
	if h.calendarID == "" {
		created, err = h.client.Me().Events().Post(ctx, e, nil)
	} else {
		created, err = h.client.Me().Calendars().ByCalendarId(h.calendarID).Events().Post(ctx, e, nil)
	}
	if err != nil {
		return "", wrapGraphError("create event", err)
	}
	return *created.GetId(), nil
}

func (h *Helper) ListCalendars(ctx context.Context) ([]Calendar, error) {
	res, err := h.client.Me().Calendars().Get(ctx, nil)
	if err != nil {
		return nil, wrapGraphError("list calendars", err)
	}
	var out []Calendar
	for _, c := range res.GetValue() {
		if c.GetId() == nil || c.GetName() == nil {
			continue
		}
		out = append(out, Calendar{ID: *c.GetId(), Name: *c.GetName()})
	}
	return out, nil
}

func mapAuthError(err error) error {
	var are *azidentity.AuthenticationRequiredError
	if errors.As(err, &are) {
		return fmt.Errorf("%w: %v", ErrLoginRequired, err)
	}
	return fmt.Errorf("graphhelper: token: %w", err)
}

func wrapGraphError(op string, err error) error {
	var oe *odataerrors.ODataError
	if errors.As(err, &oe) && oe.GetErrorEscaped() != nil {
		me := oe.GetErrorEscaped()
		code, msg := "", ""
		if me.GetCode() != nil {
			code = *me.GetCode()
		}
		if me.GetMessage() != nil {
			msg = *me.GetMessage()
		}
		return fmt.Errorf("graphhelper: %s: %s: %s", op, code, msg)
	}
	if _, ok := errors.AsType[*azidentity.AuthenticationRequiredError](err); ok {
		return fmt.Errorf("%w: %v", ErrLoginRequired, err)
	}
	return fmt.Errorf("graphhelper: %s: %w", op, err)
}

func (h *Helper) CreateBusyBlock(ctx context.Context, key string, start, end time.Time) error {
	const layout = "2006-01-02T15:04:05"
	tz := h.loc.String()
	subject := "Busy"
	showAs := models.BUSY_FREEBUSYSTATUS
	sens := models.PRIVATE_SENSITIVITY
	noReminder := false

	e := models.NewEvent()
	e.SetSubject(&subject)
	e.SetShowAs(&showAs)
	e.SetSensitivity(&sens)
	e.SetIsReminderOn(&noReminder)
	e.SetCategories([]string{syncCategory})

	s, en := start.In(h.loc).Format(layout), end.In(h.loc).Format(layout)
	ds := models.NewDateTimeTimeZone()
	ds.SetDateTime(&s)
	ds.SetTimeZone(&tz)
	e.SetStart(ds)
	de := models.NewDateTimeTimeZone()
	de.SetDateTime(&en)
	de.SetTimeZone(&tz)
	e.SetEnd(de)

	id, k := syncPropID, key
	p := models.NewSingleValueLegacyExtendedProperty()
	p.SetId(&id)
	p.SetValue(&k)
	e.SetSingleValueExtendedProperties([]models.SingleValueLegacyExtendedPropertyable{p})

	_, err := h.client.Me().Events().Post(ctx, e, nil)
	if err != nil {
		return wrapGraphError("create busy block", err)
	}
	return nil
}

func (h *Helper) ListBusyBlocks(ctx context.Context, from, to time.Time) (map[string]string, error) {
	s := from.UTC().Format(time.RFC3339)
	e := to.UTC().Format(time.RFC3339)
	top := int32(500)

	res, err := h.client.Me().CalendarView().Get(ctx,
		&users.ItemCalendarViewRequestBuilderGetRequestConfiguration{
			QueryParameters: &users.ItemCalendarViewRequestBuilderGetQueryParameters{
				StartDateTime: &s,
				EndDateTime:   &e,
				Top:           &top,
				Select:        []string{"id", "categories"},
				Expand:        []string{"singleValueExtendedProperties($filter=id eq '" + syncPropID + "')"},
			},
		})
	if err != nil {
		return nil, wrapGraphError("list busy blocks", err)
	}

	out := map[string]string{}
	for _, ev := range res.GetValue() {
		if ev.GetId() == nil || !hasCategory(ev.GetCategories(), syncCategory) {
			continue
		}
		for _, p := range ev.GetSingleValueExtendedProperties() {
			if p.GetValue() != nil && strings.EqualFold(*p.GetId(), syncPropID) {
				out[*p.GetValue()] = *ev.GetId()
			}
		}
	}
	return out, nil
}

func (h *Helper) DeleteEvent(ctx context.Context, id string) error {
	if err := h.client.Me().Events().ByEventId(id).Delete(ctx, nil); err != nil {
		return wrapGraphError("delete event", err)
	}
	return nil
}

func hasCategory(cats []string, want string) bool {
	for _, c := range cats {
		if c == want {
			return true
		}
	}
	return false
}

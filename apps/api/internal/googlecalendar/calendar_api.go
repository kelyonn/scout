package googlecalendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const calendarAPIBase = "https://www.googleapis.com/calendar/v3"

// eventDate is the all-day-event date shape Calendar v3 wants — {"date":
// "2026-08-25"}, not {"dateTime": ...}. A deadline is a day, not a
// specific minute, and all-day events sidestep timezone math entirely
// (Google renders them on the right calendar day regardless of the
// viewer's timezone, which a dateTime event doesn't guarantee without
// also sending a timeZone field).
type eventDate struct {
	Date string `json:"date"`
}

type calendarEvent struct {
	ID          string    `json:"id,omitempty"`
	Summary     string    `json:"summary"`
	Description string    `json:"description,omitempty"`
	Start       eventDate `json:"start"`
	End         eventDate `json:"end"`
}

type eventsListResponse struct {
	Items []calendarEvent `json:"items"`
}

func doCalendarRequest(ctx context.Context, client *http.Client, method, url string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calendar api request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("calendar api %s %s: %d: %s", method, url, res.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// insertEvent creates a new all-day event and returns its Google-assigned ID.
func insertEvent(ctx context.Context, client *http.Client, calendarID string, ev calendarEvent) (string, error) {
	var created calendarEvent
	url := fmt.Sprintf("%s/calendars/%s/events", calendarAPIBase, calendarID)
	if err := doCalendarRequest(ctx, client, http.MethodPost, url, ev, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// updateEvent overwrites an existing event by ID.
func updateEvent(ctx context.Context, client *http.Client, calendarID, eventID string, ev calendarEvent) error {
	url := fmt.Sprintf("%s/calendars/%s/events/%s", calendarAPIBase, calendarID, eventID)
	return doCalendarRequest(ctx, client, http.MethodPut, url, ev, nil)
}

// listEvents returns events in [from, to), ordered by start time.
func listEvents(ctx context.Context, client *http.Client, calendarID string, from, to time.Time) ([]calendarEvent, error) {
	url := fmt.Sprintf(
		"%s/calendars/%s/events?timeMin=%s&timeMax=%s&singleEvents=true&orderBy=startTime",
		calendarAPIBase, calendarID,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339),
	)
	var resp eventsListResponse
	if err := doCalendarRequest(ctx, client, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

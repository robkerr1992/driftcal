package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/nylas"
)

// NormalizeEvent converts a Nylas event into sqlc UpsertEventParams.
// It handles timespan, date, and datespan event types.
func NormalizeEvent(calendarID int64, ev *nylas.Event) (sqlcdb.UpsertEventParams, error) {
	startTime, endTime, allDay, err := normalizeWhen(ev.When)
	if err != nil {
		return sqlcdb.UpsertEventParams{}, fmt.Errorf("normalizing event %s timing: %w", ev.ID, err)
	}

	rawData, err := json.Marshal(ev)
	if err != nil {
		return sqlcdb.UpsertEventParams{}, fmt.Errorf("marshaling raw data for event %s: %w", ev.ID, err)
	}

	busy := "free"
	if ev.Busy {
		if ev.Status == "tentative" {
			busy = "tentative"
		} else {
			busy = "busy"
		}
	}

	status := normalizeStatus(ev.Status)

	var recurrence sql.NullString
	if len(ev.RecurrenceRule) > 0 {
		recurrence = sql.NullString{String: strings.Join(ev.RecurrenceRule, "\n"), Valid: true}
	}

	return sqlcdb.UpsertEventParams{
		CalendarID:   calendarID,
		NylasEventID: ev.ID,
		Title:        nullString(ev.Title),
		Description:  nullString(ev.Description),
		Location:     nullString(ev.Location),
		StartTime:    startTime,
		EndTime:      endTime,
		OriginalTz:   nullString(ev.When.StartTimezone),
		AllDay:       allDay,
		Status:       status,
		Busy:         busy,
		// TODO(categorize): derive category from event attributes; hardcoded until AI categorization milestone
		Category:     sql.NullString{String: "other", Valid: true},
		RecurrenceRule: recurrence,
		RawData:      sql.NullString{String: string(rawData), Valid: true},
	}, nil
}

func normalizeWhen(w nylas.EventWhen) (start, end time.Time, allDay bool, err error) {
	switch w.Object {
	case "timespan":
		return time.Unix(w.StartTime, 0).UTC(), time.Unix(w.EndTime, 0).UTC(), false, nil

	case "date":
		d, err := time.Parse("2006-01-02", w.Date)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("parsing date %q: %w", w.Date, err)
		}
		return d.UTC(), d.Add(24*time.Hour - time.Second).UTC(), true, nil

	case "datespan":
		sd, err := time.Parse("2006-01-02", w.StartDate)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("parsing start_date %q: %w", w.StartDate, err)
		}
		ed, err := time.Parse("2006-01-02", w.EndDate)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("parsing end_date %q: %w", w.EndDate, err)
		}
		return sd.UTC(), ed.Add(24*time.Hour - time.Second).UTC(), true, nil

	default:
		return time.Time{}, time.Time{}, false, fmt.Errorf("unknown when.object type %q", w.Object)
	}
}

func normalizeStatus(s string) string {
	switch s {
	case "confirmed", "tentative", "cancelled":
		return s
	default:
		return "confirmed"
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

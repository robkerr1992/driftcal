package preferences

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

// Package preferences provides a typed wrapper around the user_preferences
// table. Currently assumes a single user — all preferences are global.
// Multi-user support will require adding a user_id column.

// Store provides typed access to the user_preferences key-value table.
type Store struct {
	db      *sql.DB
	queries *sqlcdb.Queries
}

// New creates a preferences Store backed by the given sqlc queries and DB connection.
func New(q *sqlcdb.Queries, db *sql.DB) *Store {
	return &Store{queries: q, db: db}
}

// Timezone returns the user's configured timezone, defaulting to UTC.
func (s *Store) Timezone(ctx context.Context) (*time.Location, error) {
	val, err := s.getOrDefault(ctx, "timezone", "UTC")
	if err != nil {
		return nil, fmt.Errorf("loading timezone preference: %w", err)
	}
	loc, err := time.LoadLocation(val)
	if err != nil {
		return nil, fmt.Errorf("parsing timezone %q: %w", val, err)
	}
	return loc, nil
}

// ActiveHoursStart returns the active hours start time (HH:MM), defaulting to "07:00".
func (s *Store) ActiveHoursStart(ctx context.Context) (string, error) {
	return s.getOrDefault(ctx, "active_hours_start", "07:00")
}

// ActiveHoursEnd returns the active hours end time (HH:MM), defaulting to "22:00".
func (s *Store) ActiveHoursEnd(ctx context.Context) (string, error) {
	return s.getOrDefault(ctx, "active_hours_end", "22:00")
}

// MinGapMinutes returns the minimum gap duration in minutes, defaulting to 45.
func (s *Store) MinGapMinutes(ctx context.Context) (int, error) {
	val, err := s.getOrDefault(ctx, "min_gap_minutes", "45")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 45, nil // fall back to default on corrupt data
	}
	return n, nil
}

// Get returns the raw value for a preference key.
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	pref, err := s.queries.GetPreference(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("getting preference %q: %w", key, err)
	}
	return pref.Value, nil
}

// Set stores a preference value.
func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.queries.UpsertPreference(ctx, sqlcdb.UpsertPreferenceParams{
		Key:   key,
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("setting preference %q: %w", key, err)
	}
	return nil
}

// SetMany atomically stores multiple preferences in a single transaction.
// If any upsert fails, the entire batch is rolled back.
func (s *Store) SetMany(ctx context.Context, updates map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := s.queries.WithTx(tx)
	for key, val := range updates {
		if _, err := qtx.UpsertPreference(ctx, sqlcdb.UpsertPreferenceParams{
			Key:   key,
			Value: val,
		}); err != nil {
			return fmt.Errorf("setting preference %q: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing preferences: %w", err)
	}
	return nil
}

// All returns all preferences as a flat key-value map.
func (s *Store) All(ctx context.Context) (map[string]string, error) {
	prefs, err := s.queries.ListPreferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing preferences: %w", err)
	}
	m := make(map[string]string, len(prefs))
	for _, p := range prefs {
		m[p.Key] = p.Value
	}
	return m, nil
}

// FormatForAPI transforms the flat key-value pairs into the nested API response
// structure specified in api-design.md.
func (s *Store) FormatForAPI(ctx context.Context) (map[string]any, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}

	getStr := func(key, def string) string {
		if v, ok := all[key]; ok {
			return v
		}
		return def
	}

	getInt := func(key string, def int) int {
		if v, ok := all[key]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	}

	getJSON := func(key string, def any) any {
		if v, ok := all[key]; ok {
			var parsed any
			if err := json.Unmarshal([]byte(v), &parsed); err == nil {
				return parsed
			}
		}
		return def
	}

	return map[string]any{
		"active_hours": map[string]string{
			"start": getStr("active_hours_start", "07:00"),
			"end":   getStr("active_hours_end", "22:00"),
		},
		"timezone":        getStr("timezone", "UTC"),
		"min_gap_minutes": getInt("min_gap_minutes", 45),
		"interests":       getJSON("interests", []any{}),
		"energy_profile": getJSON("energy_profile", map[string]any{
			"morning":   "high",
			"afternoon": "medium",
			"evening":   "low",
		}),
		"digest_time": getStr("digest_time", "07:30"),
	}, nil
}

func (s *Store) getOrDefault(ctx context.Context, key, def string) (string, error) {
	pref, err := s.queries.GetPreference(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return def, nil
		}
		return "", fmt.Errorf("getting preference %q: %w", key, err)
	}
	return pref.Value, nil
}

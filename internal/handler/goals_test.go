package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/goal"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

func createTestGoal(t *testing.T, q *sqlcdb.Queries, label string) goalResponse {
	t.Helper()
	log := zerolog.Nop()
	h := CreateGoal(q, log)

	body := `{"label":"` + label + `","category":"study","duration_minutes":60,"times_per_week":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("createTestGoal: status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp goalResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("createTestGoal: decoding: %v", err)
	}
	return resp
}

func TestCreateGoal_HappyPath(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := CreateGoal(q, log)

	body := `{
		"label": "Study Go",
		"category": "study",
		"duration_minutes": 60,
		"times_per_week": 2,
		"priority": 4,
		"energy_level": "high",
		"preferred_time_of_day": "morning",
		"earliest_hour": "08:00",
		"latest_hour": "12:00",
		"allowed_days": ["mon", "wed", "fri"],
		"min_gap_between_hours": 48
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp goalResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Label != "Study Go" {
		t.Errorf("Label = %q, want %q", resp.Label, "Study Go")
	}
	if resp.Priority != 4 {
		t.Errorf("Priority = %d, want 4", resp.Priority)
	}
	if resp.EnergyLevel != "high" {
		t.Errorf("EnergyLevel = %q, want high", resp.EnergyLevel)
	}
	if len(resp.AllowedDays) != 3 {
		t.Errorf("AllowedDays = %v, want [mon wed fri]", resp.AllowedDays)
	}
	if resp.ID == 0 {
		t.Error("ID should be non-zero")
	}
}

func TestCreateGoal_InvalidCategory(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	body := `{"label":"Test","category":"invalid","duration_minutes":60,"times_per_week":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateGoal_DurationTooShort(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	body := `{"label":"Test","category":"study","duration_minutes":10,"times_per_week":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateGoal_InvalidAllowedDays(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	body := `{"label":"Test","category":"study","duration_minutes":30,"times_per_week":1,"allowed_days":["mon","xyz"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateGoal_InvalidHours(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	// latest_hour <= earliest_hour
	body := `{"label":"Test","category":"study","duration_minutes":30,"times_per_week":1,"earliest_hour":"14:00","latest_hour":"10:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListGoals_Empty(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := ListGoals(q, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/goals", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Goals []goalResponse `json:"goals"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(resp.Goals) != 0 {
		t.Errorf("got %d goals, want 0", len(resp.Goals))
	}
}

func TestListGoals_WithThisWeek(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "Study Go")

	// Create instances for this week.
	weekStart := goal.MondayOf(time.Now())
	_, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	// Complete one.
	completed, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(33 * time.Hour),
		ScheduledEnd:   weekStart.Add(34 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}
	_, err = q.UpdateGoalInstanceStatus(t.Context(), sqlcdb.UpdateGoalInstanceStatusParams{
		Status: "completed",
		ID:     completed.ID,
	})
	if err != nil {
		t.Fatalf("completing instance: %v", err)
	}

	h := ListGoals(q, log)
	req := httptest.NewRequest(http.MethodGet, "/api/goals", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Goals []goalResponse `json:"goals"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(resp.Goals) != 1 {
		t.Fatalf("got %d goals, want 1", len(resp.Goals))
	}

	tw := resp.Goals[0].ThisWeek
	if tw.Scheduled != 1 {
		t.Errorf("Scheduled = %d, want 1", tw.Scheduled)
	}
	if tw.Completed != 1 {
		t.Errorf("Completed = %d, want 1", tw.Completed)
	}
	if tw.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0", tw.Remaining)
	}
}

func TestUpdateGoal_PartialUpdate(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "Original")

	updateH := UpdateGoal(q, log)
	r := chi.NewRouter()
	r.Patch("/api/goals/{id}", updateH)

	body := `{"label":"Updated"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/goals/"+strconv.FormatInt(created.ID, 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp goalResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Label != "Updated" {
		t.Errorf("Label = %q, want Updated", resp.Label)
	}
	// Category should be unchanged.
	if resp.Category != "study" {
		t.Errorf("Category = %q, want study (unchanged)", resp.Category)
	}
}

func TestDeleteGoal_SoftDelete(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "To Delete")

	deleteH := DeleteGoal(q, log)
	r := chi.NewRouter()
	r.Delete("/api/goals/{id}", deleteH)

	req := httptest.NewRequest(http.MethodDelete, "/api/goals/"+strconv.FormatInt(created.ID, 10), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	// Verify still in DB but inactive.
	g, err := q.GetGoal(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if g.IsActive {
		t.Error("goal should be inactive after soft delete")
	}
}

func TestDeleteGoal_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	deleteH := DeleteGoal(q, zerolog.Nop())
	r := chi.NewRouter()
	r.Delete("/api/goals/{id}", deleteH)

	req := httptest.NewRequest(http.MethodDelete, "/api/goals/99999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListGoalInstances_DefaultWeek(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "Test Instances")

	weekStart := goal.MondayOf(time.Now())
	_, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	listH := ListGoalInstances(q, log)
	r := chi.NewRouter()
	r.Get("/api/goals/{id}/instances", listH)

	req := httptest.NewRequest(http.MethodGet, "/api/goals/"+strconv.FormatInt(created.ID, 10)+"/instances", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Instances []sqlcdb.GoalInstance `json:"instances"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(resp.Instances) != 1 {
		t.Errorf("got %d instances, want 1", len(resp.Instances))
	}
}

func TestSkipGoalInstance_StatusTransition(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()
	prefs := preferences.New(q, db)

	created := createTestGoal(t, q, "Skip Test")

	weekStart := goal.MondayOf(time.Now())
	inst, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	skipH := SkipGoalInstance(q, prefs, nil, log)
	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/skip", skipH)

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/skip"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Instance    sqlcdb.GoalInstance `json:"instance"`
		Rescheduled any                `json:"rescheduled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Instance.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", resp.Instance.Status)
	}
	// With the reschedule logic active, a skip with ample gaps will reschedule.
	// Just verify the response is well-formed (rescheduled may or may not be nil).
}

func TestCompleteGoalInstance_StatusTransition(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "Complete Test")

	weekStart := goal.MondayOf(time.Now())
	inst, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	completeH := CompleteGoalInstance(q, log)
	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/complete", completeH)

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/complete"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Instance sqlcdb.GoalInstance `json:"instance"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Instance.Status != "completed" {
		t.Errorf("Status = %q, want completed", resp.Instance.Status)
	}
}

func TestSkipGoalInstance_WrongGoal(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()
	prefs := preferences.New(q, db)

	goal1 := createTestGoal(t, q, "Goal 1")
	goal2 := createTestGoal(t, q, "Goal 2")

	weekStart := goal.MondayOf(time.Now())
	inst, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         goal2.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	skipH := SkipGoalInstance(q, prefs, nil, log)
	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/skip", skipH)

	// Try to skip goal2's instance via goal1's URL.
	url := "/api/goals/" + strconv.FormatInt(goal1.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/skip"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- Test Group 3: UpdateGoal validation ---

func TestUpdateGoal_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	r := chi.NewRouter()
	r.Patch("/api/goals/{id}", UpdateGoal(q, zerolog.Nop()))

	req := httptest.NewRequest(http.MethodPatch, "/api/goals/99999", strings.NewReader(`{"label":"X"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateGoal_InvalidCategory(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	created := createTestGoal(t, q, "Test")
	r := chi.NewRouter()
	r.Patch("/api/goals/{id}", UpdateGoal(q, zerolog.Nop()))

	body := `{"category":"invalid"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/goals/"+strconv.FormatInt(created.ID, 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateGoal_DurationTooShort(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	created := createTestGoal(t, q, "Test")
	r := chi.NewRouter()
	r.Patch("/api/goals/{id}", UpdateGoal(q, zerolog.Nop()))

	body := `{"duration_minutes":5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/goals/"+strconv.FormatInt(created.ID, 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateGoal_PriorityOutOfRange(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	created := createTestGoal(t, q, "Test")
	r := chi.NewRouter()
	r.Patch("/api/goals/{id}", UpdateGoal(q, zerolog.Nop()))

	body := `{"priority":10}`
	req := httptest.NewRequest(http.MethodPatch, "/api/goals/"+strconv.FormatInt(created.ID, 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateGoal_HoursInvertedAfterMerge(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	created := createTestGoal(t, q, "Test")
	r := chi.NewRouter()
	r.Patch("/api/goals/{id}", UpdateGoal(q, zerolog.Nop()))

	// Set earliest > existing latest (22:00).
	body := `{"earliest_hour":"23:00"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/goals/"+strconv.FormatInt(created.ID, 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// --- Test Group 4: Handler edge cases ---

func TestSkipGoalInstance_AlreadySkipped(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()
	prefs := preferences.New(q, db)

	created := createTestGoal(t, q, "Skip Twice")
	weekStart := goal.MondayOf(time.Now())
	inst, _ := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID: created.ID, WeekStart: weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour), ScheduledEnd: weekStart.Add(10 * time.Hour),
	})

	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/skip", SkipGoalInstance(q, prefs, nil, log))

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/skip"

	// First skip succeeds.
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first skip: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Second skip fails.
	req = httptest.NewRequest(http.MethodPost, url, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second skip: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCompleteGoalInstance_AlreadyCompleted(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "Complete Twice")
	weekStart := goal.MondayOf(time.Now())
	inst, _ := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID: created.ID, WeekStart: weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour), ScheduledEnd: weekStart.Add(10 * time.Hour),
	})

	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/complete", CompleteGoalInstance(q, log))

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/complete"

	// First complete succeeds.
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first complete: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Second complete fails.
	req = httptest.NewRequest(http.MethodPost, url, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second complete: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSkipGoalInstance_NonExistent(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)

	created := createTestGoal(t, q, "NonExist")
	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/skip", SkipGoalInstance(q, prefs, nil, zerolog.Nop()))

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/99999/skip"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCompleteGoalInstance_WrongGoal(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	goalA := createTestGoal(t, q, "Goal A")
	goalB := createTestGoal(t, q, "Goal B")

	weekStart := goal.MondayOf(time.Now())
	inst, _ := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID: goalB.ID, WeekStart: weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour), ScheduledEnd: weekStart.Add(10 * time.Hour),
	})

	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/complete", CompleteGoalInstance(q, log))

	// Try to complete goalB's instance via goalA's URL.
	url := "/api/goals/" + strconv.FormatInt(goalA.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/complete"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateGoal_MissingLabel(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	body := `{"category":"study","duration_minutes":60,"times_per_week":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateGoal_DuplicateAllowedDays(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	body := `{"label":"Test","category":"study","duration_minutes":30,"times_per_week":1,"allowed_days":["mon","mon"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListGoalInstances_ExplicitWeek(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	created := createTestGoal(t, q, "Explicit Week")

	weekStart := time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC)
	_, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID: created.ID, WeekStart: weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour), ScheduledEnd: weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/api/goals/{id}/instances", ListGoalInstances(q, log))

	req := httptest.NewRequest(http.MethodGet, "/api/goals/"+strconv.FormatInt(created.ID, 10)+"/instances?week_start=2026-02-23", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Instances []sqlcdb.GoalInstance `json:"instances"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(resp.Instances) != 1 {
		t.Errorf("got %d instances, want 1", len(resp.Instances))
	}
}

// --- Bug 1 regression: verify PreferredTimeOfDay serializes as a plain string ---

func TestCreateGoal_PreferredTimeOfDayString(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := CreateGoal(q, zerolog.Nop())
	body := `{"label":"TOD Test","category":"study","duration_minutes":60,"times_per_week":1,"preferred_time_of_day":"morning"}`
	req := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp goalResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.PreferredTimeOfDay != "morning" {
		t.Errorf("PreferredTimeOfDay = %q, want %q", resp.PreferredTimeOfDay, "morning")
	}
}

// --- Reschedule tests ---

func TestSkipGoalInstance_Reschedules(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()
	prefs := preferences.New(q, db)

	// Set timezone to UTC so active hours are straightforward.
	prefs.Set(t.Context(), "timezone", "UTC")
	prefs.Set(t.Context(), "active_hours_start", "07:00")
	prefs.Set(t.Context(), "active_hours_end", "22:00")

	created := createTestGoal(t, q, "Reschedule Test")

	// Schedule instance early in the week — there should be plenty of room to reschedule.
	weekStart := goal.MondayOf(time.Now())
	inst, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	skipH := SkipGoalInstance(q, prefs, nil, log)
	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/skip", skipH)

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/skip"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Instance    sqlcdb.GoalInstance  `json:"instance"`
		Rescheduled *sqlcdb.GoalInstance `json:"rescheduled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Instance.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", resp.Instance.Status)
	}
	if resp.Rescheduled == nil {
		t.Fatal("expected rescheduled instance, got nil")
	}
	if resp.Rescheduled.GoalID != created.ID {
		t.Errorf("Rescheduled.GoalID = %d, want %d", resp.Rescheduled.GoalID, created.ID)
	}
	if !resp.Rescheduled.ScheduledStart.After(inst.ScheduledEnd) {
		t.Errorf("Rescheduled start %v should be after original end %v",
			resp.Rescheduled.ScheduledStart, inst.ScheduledEnd)
	}
}

func TestSkipGoalInstance_NoSlotAvailable(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()
	prefs := preferences.New(q, db)

	prefs.Set(t.Context(), "timezone", "UTC")
	prefs.Set(t.Context(), "active_hours_start", "09:00")
	prefs.Set(t.Context(), "active_hours_end", "10:00")

	// Create goal restricted to Monday only — after skipping the only Monday
	// slot, there's nowhere else to reschedule within the week.
	h := CreateGoal(q, zerolog.Nop())
	body := `{"label":"No Slot","category":"study","duration_minutes":60,"times_per_week":1,"allowed_days":["mon"]}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/goals", strings.NewReader(body))
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("creating goal: status = %d, body: %s", createRec.Code, createRec.Body.String())
	}
	var created goalResponse
	json.NewDecoder(createRec.Body).Decode(&created)

	weekStart := goal.MondayOf(time.Now())
	inst, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         created.ID,
		WeekStart:      weekStart,
		ScheduledStart: weekStart.Add(9 * time.Hour),
		ScheduledEnd:   weekStart.Add(10 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating instance: %v", err)
	}

	skipH := SkipGoalInstance(q, prefs, nil, log)
	r := chi.NewRouter()
	r.Post("/api/goals/{id}/instances/{instance_id}/skip", skipH)

	url := "/api/goals/" + strconv.FormatInt(created.ID, 10) + "/instances/" + strconv.FormatInt(inst.ID, 10) + "/skip"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Instance    sqlcdb.GoalInstance `json:"instance"`
		Rescheduled any                `json:"rescheduled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Instance.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", resp.Instance.Status)
	}
	if resp.Rescheduled != nil {
		t.Errorf("Rescheduled = %v, want nil (no slot available)", resp.Rescheduled)
	}
}

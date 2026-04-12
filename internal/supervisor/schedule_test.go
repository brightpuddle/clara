package supervisor

import (
	"testing"
	"time"
)

func TestNextCronTime(t *testing.T) {
	start := time.Date(2026, 3, 15, 6, 45, 0, 0, time.UTC)
	next, err := nextCronTime("0 7 * * *", start)
	if err != nil {
		t.Fatalf("nextCronTime failed: %v", err)
	}
	want := time.Date(2026, 3, 15, 7, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("unexpected next time: got %s want %s", next, want)
	}
}

func TestNextCronTime_StepField(t *testing.T) {
	start := time.Date(2026, 3, 15, 6, 2, 0, 0, time.UTC)
	next, err := nextCronTime("*/5 * * * *", start)
	if err != nil {
		t.Fatalf("nextCronTime failed: %v", err)
	}
	want := time.Date(2026, 3, 15, 6, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("unexpected next time: got %s want %s", next, want)
	}
}

func TestNextCronTime_InvalidExpression(t *testing.T) {
	if _, err := nextCronTime("daily", time.Now()); err == nil {
		t.Fatal("expected invalid cron expression to fail")
	}
}

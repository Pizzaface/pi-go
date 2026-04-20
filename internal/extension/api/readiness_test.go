package api

import (
	"context"
	"testing"
	"time"
)

func TestReadiness_ExplicitReady(t *testing.T) {
	r := NewReadiness()
	r.Track("ext-a")
	r.MarkReady("ext-a")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx, 500*time.Millisecond); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.State("ext-a") != ReadinessReady {
		t.Fatal("expected Ready")
	}
}

func TestReadiness_Quiescence(t *testing.T) {
	r := NewReadiness()
	r.QuiescenceWindow = 50 * time.Millisecond
	r.Track("ext-a")
	r.Kick("ext-a") // register fires
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx, 500*time.Millisecond); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Fatalf("returned too fast: %v", elapsed)
	}
	if r.State("ext-a") != ReadinessReady {
		t.Fatal("expected Ready via quiescence")
	}
}

func TestReadiness_Timeout(t *testing.T) {
	r := NewReadiness()
	r.Track("ext-a")
	ctx := context.Background()
	if err := r.Wait(ctx, 50*time.Millisecond); err == nil {
		t.Fatal("expected timeout")
	}
	if r.State("ext-a") != ReadinessTimedOut {
		t.Fatalf("state = %v", r.State("ext-a"))
	}
}

func TestReadiness_Errored(t *testing.T) {
	r := NewReadiness()
	r.Track("ext-a")
	r.MarkErrored("ext-a", context.Canceled)
	ctx := context.Background()
	if err := r.Wait(ctx, time.Second); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.State("ext-a") != ReadinessErrored {
		t.Fatal("expected Errored")
	}
}

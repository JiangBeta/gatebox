package reconcile

import (
	"testing"
	"time"

	"github.com/JiangBeta/gatebox/internal/repository"
)

func TestBoltRunStoreRoundTrip(t *testing.T) {
	repo, err := repository.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewBoltRunStore(repo)

	run := Run{
		ID:        NewRunID(time.Now()),
		Trigger:   "manual",
		State:     "success",
		StartedAt: time.Now().Unix(),
		Steps:     []Step{{Component: "caddy", Action: "reconcile", State: "success"}},
	}
	if err := s.Save(run); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "success" || len(got.Steps) != 1 || got.Steps[0].Component != "caddy" {
		t.Fatalf("bad roundtrip: %+v", got)
	}
	list, err := s.List(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
}

func TestBoltRunStoreTrimsEvents(t *testing.T) {
	repo, _ := repository.Open(t.TempDir())
	s := NewBoltRunStore(repo)
	events := make([]string, 30)
	for i := range events {
		events[i] = "e"
	}
	run := Run{ID: NewRunID(time.Now()), State: "success", Steps: []Step{{Component: "c", Events: events}}}
	if err := s.Save(run); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(run.ID)
	if len(got.Steps[0].Events) != 20 {
		t.Fatalf("events not trimmed: %d", len(got.Steps[0].Events))
	}
}

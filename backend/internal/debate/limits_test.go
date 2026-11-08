package debate

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInputBoundaries(t *testing.T) {
	for _, s := range []string{"0", "01", "+1", "18446744073709551616", "-1", ""} {
		if _, err := ParseID(s); err == nil {
			t.Errorf("accepted invalid id")
		}
	}
	if _, err := ParseID("18446744073709551615"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 2000, 2001} {
		_, err := ValidateSubmit(SubmitInput{RoundNo: 1, Content: strings.Repeat("𠮷", n), ClientRequestID: "550E8400-E29B-41D4-A716-446655440000"})
		if (err == nil) != (n <= 2000) {
			t.Fatalf("boundary %d", n)
		}
	}
	if _, err := ValidateSubmit(SubmitInput{RoundNo: 1, Content: "　\n\t", ClientRequestID: "550e8400-e29b-41d4-a716-446655440000"}); err == nil {
		t.Fatal("accepted whitespace")
	}
	if _, err := ValidateSubmit(SubmitInput{RoundNo: 1, Content: " \ufeff ", ClientRequestID: "550e8400-e29b-41d4-a716-446655440000"}); err == nil {
		t.Fatal("accepted mixed BOM whitespace")
	}
}

func TestAdmissionSlotsQuotaAndRollback(t *testing.T) {
	now := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	gate := NewGate(func() time.Time { return now })
	p, err := gate.Acquire(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gate.Acquire(1); !errors.Is(err, ErrGenerationBusy) {
		t.Fatal("same user admitted twice")
	}
	q, err := gate.Acquire(2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gate.Acquire(3); !errors.Is(err, ErrGenerationBusy) {
		t.Fatal("third global admitted")
	}
	p.Finish(false)
	q.Finish(false)
	for i := 0; i < 20; i++ {
		p, err = gate.Acquire(1)
		if err != nil {
			t.Fatal(err)
		}
		p.Finish(true)
		p.Finish(true)
	}
	if _, err = gate.Acquire(1); !errors.Is(err, ErrRateLimited) {
		t.Fatal("daily quota not enforced")
	}
	now = now.Add(24 * time.Hour)
	p, err = gate.Acquire(1)
	if err != nil {
		t.Fatal(err)
	}
	p.Finish(false)
	for i := 0; i < 6; i++ {
		if err = gate.Request(1); err != nil {
			t.Fatal(err)
		}
	}
	if err = gate.Request(1); !errors.Is(err, ErrRateLimited) {
		t.Fatal("minute quota not enforced")
	}
	now = now.Add(time.Minute)
	if err = gate.Request(1); err != nil {
		t.Fatal(err)
	}
}

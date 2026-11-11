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
	for _, bad := range []uint64{0, ^uint64(0)} {
		if _, err := ValidateRetry(RetryInput{ExpectedAttemptVersion: bad}); err == nil {
			t.Fatalf("accepted invalid expected_attempt_version %d", bad)
		}
	}
	if _, err := ValidateRetry(RetryInput{ExpectedAttemptVersion: 1}); err != nil {
		t.Fatal(err)
	}
	page, size, err := ParsePagination(nil, nil)
	if err != nil || page != 1 || size != 20 {
		t.Fatalf("default pagination got page=%d size=%d err=%v", page, size, err)
	}
	page, size, err = ParsePagination([]string{"2"}, []string{"50"})
	if err != nil || page != 2 || size != 50 {
		t.Fatalf("custom pagination got page=%d size=%d err=%v", page, size, err)
	}
	for _, tc := range []struct {
		page, size []string
	}{
		{[]string{"0"}, nil},
		{[]string{"-1"}, nil},
		{[]string{"01"}, nil},
		{[]string{"1", "2"}, nil},
		{nil, []string{"0"}},
		{nil, []string{"51"}},
		{nil, []string{"abc"}},
		{nil, []string{"20", "30"}},
	} {
		if _, _, err := ParsePagination(tc.page, tc.size); err == nil {
			t.Fatalf("accepted invalid pagination page=%v size=%v", tc.page, tc.size)
		}
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

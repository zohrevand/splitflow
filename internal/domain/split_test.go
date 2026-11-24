package domain_test

import (
	"slices"
	"testing"

	"github.com/zohrevand/splitflow/internal/domain"
)

func TestSplitEvenly(t *testing.T) {
	tests := []struct {
		name        string
		totalAmount int64
		n           int
		want        []int64
		wantErr     bool
	}{
		{
			name:        "exact even split",
			totalAmount: 1000,
			n:           2,
			want:        []int64{500, 500},
		},
		{
			name:        "remainder of 1 cent goes to first participant",
			totalAmount: 1000,
			n:           3,
			want:        []int64{334, 333, 333},
		},
		{
			name:        "remainder of 2 cents goes to first two participants",
			totalAmount: 1001,
			n:           3,
			want:        []int64{334, 334, 333},
		},
		{
			name:        "single participant",
			totalAmount: 2550,
			n:           1,
			want:        []int64{2550},
		},
		{
			name:        "zero amount returns error",
			totalAmount: 0,
			n:           2,
			wantErr:     true,
		},
		{
			name:        "negative amount returns error",
			totalAmount: -500,
			n:           2,
			wantErr:     true,
		},
		{
			name:        "zero participants returns error",
			totalAmount: 1000,
			n:           0,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.SplitEvenly(tc.totalAmount, tc.n)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("SplitEvenly(%d, %d) = %v, want %v", tc.totalAmount, tc.n, got, tc.want)
			}

			// Invariant check: sum of shares must ALWAYS equal totalAmount
			var sum int64
			for _, share := range got {
				sum += share
			}
			if sum != tc.totalAmount {
				t.Errorf("sum of shares = %d, want %d", sum, tc.totalAmount)
			}
		})
	}
}

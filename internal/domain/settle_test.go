package domain_test

import (
	"slices"
	"testing"

	"github.com/zohrevand/splitflow/internal/domain"
)

func TestSimplifyDebts(t *testing.T) {
	tests := []struct {
		name     string
		balances []domain.Balance
		want     []domain.Transfer
	}{
		{
			name: "already settled",
			balances: []domain.Balance{
				{UserID: "u1", Name: "Hasan", NetBalanceCents: 0},
				{UserID: "u2", Name: "Ali", NetBalanceCents: 0},
			},
			want: []domain.Transfer{},
		},
		{
			name: "two users simple transfer",
			balances: []domain.Balance{
				{UserID: "u1", Name: "Hasan", NetBalanceCents: 2500},
				{UserID: "u2", Name: "Ali", NetBalanceCents: -2500},
			},
			want: []domain.Transfer{
				{FromUserID: "u2", FromName: "Ali", ToUserID: "u1", ToName: "Hasan", Amount: 2500},
			},
		},
		{
			name: "three users multi-way split",
			balances: []domain.Balance{
				{UserID: "u1", Name: "Hasan", NetBalanceCents: 3000},
				{UserID: "u2", Name: "Ali", NetBalanceCents: -1000},
				{UserID: "u3", Name: "Sara", NetBalanceCents: -2000},
			},
			want: []domain.Transfer{
				{FromUserID: "u3", FromName: "Sara", ToUserID: "u1", ToName: "Hasan", Amount: 2000},
				{FromUserID: "u2", FromName: "Ali", ToUserID: "u1", ToName: "Hasan", Amount: 1000},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.SimplifyDebts(tc.balances)
			if !slices.Equal(got, tc.want) {
				t.Errorf("SimplifyDebts() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

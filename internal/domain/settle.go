package domain

import (
	"cmp"
	"slices"
)

type Balance struct {
	UserID          string
	Name            string
	NetBalanceCents int64
}

type Transfer struct {
	FromUserID string `json:"from_user_id"`
	FromName   string `json:"from_name"`
	ToUserID   string `json:"to_user_id"`
	ToName     string `json:"to_name"`
	Amount     int64  `json:"amount"`
}

// SimplifyDebts computes a deterministic, minimal set of transfers
// to settle all non-zero net balances in a group.
func SimplifyDebts(balances []Balance) []Transfer {
	type entry struct {
		userID string
		name   string
		amount int64
	}

	var creditors, debtors []entry
	for _, b := range balances {
		switch {
		case b.NetBalanceCents > 0:
			creditors = append(creditors, entry{userID: b.UserID, name: b.Name, amount: b.NetBalanceCents})
		case b.NetBalanceCents < 0:
			debtors = append(debtors, entry{userID: b.UserID, name: b.Name, amount: -b.NetBalanceCents})
		}
	}

	// Sort descending by amount, then ascending by userID for deterministic results
	sortDesc := func(a, b entry) int {
		if c := cmp.Compare(b.amount, a.amount); c != 0 {
			return c
		}
		return cmp.Compare(a.userID, b.userID)
	}
	slices.SortFunc(creditors, sortDesc)
	slices.SortFunc(debtors, sortDesc)

	transfers := make([]Transfer, 0)
	i, j := 0, 0
	for i < len(debtors) && j < len(creditors) {
		pay := min(debtors[i].amount, creditors[j].amount)
		transfers = append(transfers, Transfer{
			FromUserID: debtors[i].userID,
			FromName:   debtors[i].name,
			ToUserID:   creditors[j].userID,
			ToName:     creditors[j].name,
			Amount:     pay,
		})

		debtors[i].amount -= pay
		creditors[j].amount -= pay

		if debtors[i].amount == 0 {
			i++
		}
		if creditors[j].amount == 0 {
			j++
		}
	}

	return transfers
}

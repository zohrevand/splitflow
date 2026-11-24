package domain

import "errors"

// SplitEvenly divides totalAmount among n participants without losing any pennies.
// Extra cents from the remainder are distributed +1 to the first `rem` participants.
func SplitEvenly(totalAmount int64, n int) ([]int64, error) {
	if totalAmount <= 0 {
		return nil, errors.New("totalAmount must be positive")
	}
	if n <= 0 {
		return nil, errors.New("must have at least one participant")
	}

	base := totalAmount / int64(n)
	rem := totalAmount % int64(n)

	shares := make([]int64, n)
	for i := range n {
		shares[i] = base
		if int64(i) < rem {
			shares[i]++
		}
	}
	return shares, nil
}

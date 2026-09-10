package workflow

import "fmt"

type BatchError struct {
	Failed int
	Total  int
}

func (e BatchError) Error() string {
	return fmt.Sprintf("%d of %d connector operations failed", e.Failed, e.Total)
}

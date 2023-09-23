package main

import (
	"errors"
	"fmt"
)

// Merge strategy lists from two parties into a common one,
// following the precedency set by the first party.
func strategyConsensus(pa, pb []string) (c []string) {
	pbSet := make(map[string]struct{}, len(pb))
	for _, x := range pb {
		pbSet[x] = struct{}{}
	}

	for _, x := range pa {
		if _, ok := pbSet[x]; ok {
			c = append(c, x)
		}
	}
	logger.Debugf("strategy: a=%v, b=%v, consensus=%v", pa, pb, c)
	return
}

// Map a func onto a slice, for each element returning a result or an error
func tryEach[U, V any](a []U, fn func(U) (V, error)) (r []V, errs []error) {
	for _, x := range a {
		y, err := fn(x)
		if err != nil {
			logger.Debugf("attempt failed: %v", err)
			errs = append(errs, err)
			continue
		}
		r = append(r, y)
	}
	return
}

// Map a func onto a slice, until returning the first successful result
func tryUntil[U, V any](a []U, fn func(U) (V, error)) (r V, err error) {
	var errs []error
	for _, x := range a {
		r, err = fn(x)
		if err != nil {
			logger.Debugf("attempt failed: %v", err)
			errs = append(errs, err)
			continue
		}
		return
	}
	err = fmt.Errorf("all attempts failed: %w", errors.Join(errs...))
	return
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

// SPDX-License-Identifier: MIT
package bad

import "errors"

type errsPkg struct {
	ErrNotImplementedPlan8  error
	ErrNotImplementedPlan99 error
}

var errs = errsPkg{
	ErrNotImplementedPlan8:  errors.New("plan 8 stub"),
	ErrNotImplementedPlan99: errors.New("plan 99 stub"),
}

func DoIt() error {
	return errs.ErrNotImplementedPlan8
}

func DoFuturePlan() error {
	return errs.ErrNotImplementedPlan99
}

func DoMulti() (string, error) {
	return "", errs.ErrNotImplementedPlan8
}

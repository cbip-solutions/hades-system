// go:build cgo
//go:build cgo
// +build cgo

package caronte

import (
	"context"
	"errors"
	"testing"
)

func TestGetContract_ReturnsFederationUnavailableUntilPhaseH(t *testing.T) {
	t.Run("nil_federation_db", func(t *testing.T) {
		e := &Engine{deps: Deps{}}
		got, err := e.GetContract(context.Background(), "ep-1", "proj-1")
		if !errors.Is(err, ErrFederationUnavailable) {
			t.Errorf("err = %v; want ErrFederationUnavailable", err)
		}
		if got != (ContractPayload{}) {
			t.Errorf("payload = %#v; want zero ContractPayload", got)
		}
	})
	t.Run("wired_federation_db_still_unavailable_in_phase_i", func(t *testing.T) {
		e := &Engine{deps: Deps{FederationDB: &fakeFederationStore{}}}
		got, err := e.GetContract(context.Background(), "ep-2", "proj-2")
		if !errors.Is(err, ErrFederationUnavailable) {
			t.Errorf("err = %v; want ErrFederationUnavailable", err)
		}
		if got != (ContractPayload{}) {
			t.Errorf("payload = %#v; want zero ContractPayload", got)
		}
	})
}

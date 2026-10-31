package web

import "net/http"

type sweepOut struct {
	Checked        int  `json:"checked"`
	Dormant        int  `json:"dormant"`
	Deleted        int  `json:"deleted"`
	Closed         int  `json:"closed"`
	Promoted       int  `json:"promoted"`
	Abandoned      int  `json:"abandoned"`
	BaseUnresolved bool `json:"base_unresolved"`
}

// sweep runs one sweep now, which is how the browser promotes staged entries without waiting for the timer.
func (a *api) sweep(r *http.Request) (any, error) {
	rep, err := a.reg.Sweep(r.Context(), a.proj)
	if err != nil {
		return nil, err
	}
	return sweepOut{rep.Checked, rep.Dormant, rep.Deleted, rep.Closed, rep.Promoted, rep.Abandoned, rep.BaseUnresolved}, nil
}

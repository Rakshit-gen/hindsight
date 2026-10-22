package ope

import (
	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/policy"
)

// Row is one estimator's estimate of the target, and of the target minus
// the baseline.
type Row struct {
	Name string `json:"name"`
	Estimate
	Diff Estimate `json:"diff"`
}

type Report struct {
	Payments int `json:"payments"`
	// What the logged payments actually did.
	Logged Estimate    `json:"logged"`
	Rows   []Row       `json:"rows"`
	Diag   Diagnostics `json:"diagnostics"`
	// The baseline policy's diagnostics, when there is one.
	BaselineDiag *Diagnostics `json:"baseline_diagnostics,omitempty"`
}

// Evaluate scores target on the payments and compares it, estimator by
// estimator, with baseline, or with what happened if baseline is nil.
func Evaluate(ps []logs.Payment, target, baseline policy.Policy, opt Options) (*Report, error) {
	t, err := Score(ps, target, opt)
	if err != nil {
		return nil, err
	}
	r := &Report{Payments: len(ps), Logged: t.Of("logged"), Diag: t.Diag}
	var b *Scores
	if baseline != nil {
		if b, err = Score(ps, baseline, opt); err != nil {
			return nil, err
		}
		r.BaselineDiag = &b.Diag
	}
	for _, name := range Names {
		against := t.Cols["logged"]
		if b != nil {
			against = b.Cols[name]
		}
		r.Rows = append(r.Rows, Row{Name: name, Estimate: t.Of(name), Diff: Diff(t.Cols[name], against)})
	}
	return r, nil
}

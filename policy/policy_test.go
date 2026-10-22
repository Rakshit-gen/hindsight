package policy

import (
	"math"
	"testing"
)

func TestTable(t *testing.T) {
	tb, err := Parse([]byte(`{
		"epsilon": 0.1,
		"rules": [
			{"when": {"method": "card", "issuer": "sbin|hdfc"}, "order": ["cedar", "birch"]},
			{"when": {"attempt": "2+"}, "split": {"alder": 1, "birch": 3}, "epsilon": 0},
			{"order": ["birch"]}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	all := []string{"alder", "birch", "cedar"}
	card := map[string]string{"method": "card", "issuer": "hdfc"}
	cases := []struct {
		s    State
		want map[string]float64
	}{
		{State{Context: card, Attempt: 1, Eligible: all}, map[string]float64{"cedar": 0.9 + 0.1/3, "alder": 0.1 / 3, "birch": 0.1 / 3}},
		// cedar was tried, so the card rule falls to birch.
		{State{Context: card, Attempt: 2, Tried: []string{"cedar"}, Eligible: all}, map[string]float64{"birch": 0.9 + 0.1/3, "alder": 0.1 / 3, "cedar": 0.1 / 3}},
		{State{Context: map[string]string{"method": "upi"}, Attempt: 2, Tried: []string{"birch"}, Eligible: all}, map[string]float64{"alder": 1}},
		// Nothing left untried, so everything is a candidate again.
		{State{Context: map[string]string{"method": "upi"}, Attempt: 3, Tried: []string{"birch", "alder"}, Eligible: []string{"alder", "birch"}}, map[string]float64{"alder": 0.25, "birch": 0.75}},
		// No rule can place it: uniform.
		{State{Context: map[string]string{"method": "upi"}, Attempt: 1, Eligible: []string{"alder", "cedar"}}, map[string]float64{"alder": 0.5, "cedar": 0.5}},
	}
	for i, c := range cases {
		got := tb.Probs(c.s)
		sum := 0.0
		for g, p := range got {
			sum += p
			if math.Abs(p-c.want[g]) > 1e-12 {
				t.Errorf("case %d: %s got %v, want %v", i, g, p, c.want[g])
			}
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Errorf("case %d: sums to %v", i, sum)
		}
	}
}

func TestRetrySame(t *testing.T) {
	tb, _ := Parse([]byte(`{"retry": "same", "rules": [{"order": ["birch", "alder"]}]}`))
	got := tb.Probs(State{Attempt: 2, Tried: []string{"birch"}, Eligible: []string{"alder", "birch"}})
	if got["birch"] != 1 {
		t.Fatalf("retry same should stay on birch, got %v", got)
	}
}

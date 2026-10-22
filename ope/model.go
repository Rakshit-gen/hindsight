package ope

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/policy"
)

// The outcome model behind the direct and doubly robust estimates: the
// chance an attempt pays, by gateway, first try or retry, and context, with
// each finer level shrunk toward the one above it. It knows nothing about
// time, incidents or why an attempt failed, which is how a model fitted to
// real logs turns out too. The doubly robust estimate corrects what it gets
// wrong, the direct one doesn't.
type model struct {
	keys []string
	succ map[string]*[2]float64 // paid, attempts
	cont map[string]*[2]float64 // tried again, failures
	last int                    // most attempts any payment had
}

// How many attempts a finer level needs before it counts as much as the
// level above it.
const strength = 20

func fit(ps []logs.Payment, keys []string) *model {
	m := &model{keys: keys, succ: map[string]*[2]float64{}, cont: map[string]*[2]float64{}}
	for _, p := range ps {
		m.last = max(m.last, len(p.Attempts))
		for t, a := range p.Attempts {
			for _, l := range levels(a.Context, keys) {
				add(m.succ, a.Gateway+"|"+bucket(t+1)+"|"+l, a.Paid())
				if !a.Paid() {
					add(m.cont, strconv.Itoa(t+1)+"|"+l, t+1 < len(p.Attempts))
				}
			}
		}
	}
	return m
}

func add(tab map[string]*[2]float64, key string, yes bool) {
	c := tab[key]
	if c == nil {
		c = new([2]float64)
		tab[key] = c
	}
	if yes {
		c[0]++
	}
	c[1]++
}

func bucket(attempt int) string {
	if attempt == 1 {
		return "1"
	}
	return "2+"
}

// levels gives "", "k1=v1", "k1=v1|k2=v2" and so on, coarse to fine.
func levels(ctx map[string]string, keys []string) []string {
	out := []string{""}
	var b strings.Builder
	for _, k := range keys {
		if b.Len() > 0 {
			b.WriteByte('|')
		}
		b.WriteString(k + "=" + ctx[k])
		out = append(out, b.String())
	}
	return out
}

func (m *model) est(tab map[string]*[2]float64, prefix string, ctx map[string]string) float64 {
	p := 0.5
	for _, l := range levels(ctx, m.keys) {
		c := tab[prefix+l]
		if c == nil {
			break
		}
		p = (c[0] + strength*p) / (c[1] + strength)
	}
	return p
}

// pickKeys orders context keys from fewest values to most, leaving out
// ones with so many values they're probably ids.
func pickKeys(ps []logs.Payment) []string {
	seen := map[string]map[string]bool{}
	for _, p := range ps {
		for k, v := range p.Attempts[0].Context {
			if seen[k] == nil {
				seen[k] = map[string]bool{}
			}
			if len(seen[k]) <= 200 {
				seen[k][v] = true
			}
		}
	}
	var keys []string
	for k, vs := range seen {
		if len(vs) <= 200 {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(seen[a]), len(seen[b])), cmp.Compare(a, b))
	})
	return keys
}

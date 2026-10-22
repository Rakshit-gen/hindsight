package logs

import (
	"strings"
	"testing"
)

func TestReadCSV(t *testing.T) {
	in := "\xef\xbb\xbfpayment,attempt,at,method,eligible,gateway,propensity,probs,outcome\n" +
		"p1,2,2026-01-05T00:02:00Z,card,alder|birch,alder,0.1,alder:0.1|birch:0.9,success\n" +
		"p1,1,1767571200000,card,alder|birch,birch,0.9,,declined\n"
	as, err := Read(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 2 || as[0].Context["method"] != "card" || as[0].Probs["birch"] != 0.9 || as[0].At != 1767571320000 {
		t.Fatalf("got %+v", as)
	}
	ps, err := Group(as)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Attempts[0].Gateway != "birch" || !ps[0].Paid() {
		t.Fatalf("got %+v", ps)
	}
}

func TestReadJSONL(t *testing.T) {
	in := `
{"payment":"a","attempt":1,"at":5,"eligible":["x","y"],"gateway":"y","propensity":0.5,"outcome":"pending"}
{"payment":"b","attempt":1,"at":1,"eligible":["x"],"gateway":"x","propensity":1,"outcome":"success"}
`
	as, err := Read(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := Group(as)
	if ps[0].ID != "b" || !as[0].Pending() {
		t.Fatalf("payments should be oldest first: %+v", ps)
	}
}

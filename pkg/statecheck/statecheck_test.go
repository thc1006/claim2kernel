package statecheck

import (
	"fmt"
	"math/rand"
	"testing"
)

func baseEvents() []Event {
	return []Event{{Type: "publish", Profile: "p", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32", NumericalUpperBound: .01}, {Type: "admit", Profile: "p", Request: "r", NumericalBudget: .02}, {Type: "dispatch", Request: "r", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32", NumericalUpperBound: .01}, {Type: "complete", Request: "r"}}
}
func TestHappy(t *testing.T) {
	r := Check(&Trace{Events: baseEvents()})
	if !r.Valid {
		t.Fatal(FormatViolations(r.Violations))
	}
}
func TestInvariants(t *testing.T) {
	cases := map[string][]Event{
		"NoUncertifiedDispatch":         {{Type: "dispatch", Request: "r"}},
		"NoStaleProfileReuse":           {{Type: "publish", Profile: "p", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32", NumericalUpperBound: .01}, {Type: "admit", Profile: "p", Request: "r", NumericalBudget: .02}, {Type: "revoke", Profile: "p"}, {Type: "dispatch", Request: "r", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32", NumericalUpperBound: .01}},
		"AllocationNonReinterpretation": {{Type: "publish", Profile: "p", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32", NumericalUpperBound: .01}, {Type: "admit", Profile: "p", Request: "r", NumericalBudget: .02}, {Type: "device-update", Profile: "p", DeviceFingerprint: "d2"}, {Type: "dispatch", Request: "r", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d2", Precision: "fp32", NumericalUpperBound: .01}},
		"NoNumericalDowngrade":          {{Type: "publish", Profile: "p", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32", NumericalUpperBound: .01}, {Type: "admit", Profile: "p", Request: "r", NumericalBudget: .02}, {Type: "dispatch", Request: "r", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp16", NumericalUpperBound: .03}},
		"NoPerformanceResurrection":     {{Type: "publish", Profile: "p", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32"}, {Type: "revoke", Profile: "p"}, {Type: "publish", Profile: "p", ContractDigest: "c1", ArtifactDigest: "a1", DeviceFingerprint: "d1", Precision: "fp32"}}}
	for inv, events := range cases {
		r := Check(&Trace{Events: events})
		if r.Valid {
			t.Fatalf("%s was not detected", inv)
		}
		found := false
		for _, v := range r.Violations {
			if v.Invariant == inv {
				found = true
			}
		}
		if !found {
			t.Fatalf("wanted %s, got %+v", inv, r.Violations)
		}
	}
}
func TestRandomValidTraces(t *testing.T) {
	for seed := int64(0); seed < 1000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		events := []Event{}
		for i := 0; i < 1+rng.Intn(10); i++ {
			p := fmt.Sprintf("p-%d", i)
			r := fmt.Sprintf("r-%d", i)
			c := fmt.Sprintf("c-%d", i)
			a := fmt.Sprintf("a-%d", i)
			d := fmt.Sprintf("d-%d", i)
			events = append(events, Event{Type: "publish", Profile: p, ContractDigest: c, ArtifactDigest: a, DeviceFingerprint: d, Precision: "fp32", NumericalUpperBound: .01}, Event{Type: "admit", Profile: p, Request: r, NumericalBudget: .02}, Event{Type: "dispatch", Request: r, ContractDigest: c, ArtifactDigest: a, DeviceFingerprint: d, Precision: "fp32", NumericalUpperBound: .01}, Event{Type: "complete", Request: r})
		}
		res := Check(&Trace{Events: events})
		if !res.Valid {
			t.Fatalf("seed %d: %s", seed, FormatViolations(res.Violations))
		}
	}
}

// Package planner evaluates Claim2Kernel profiles against workload requests.
// It is deterministic and fail-closed; it never treats an unknown field,
// missing observation, or failed OOD check as admissible.
package planner

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/dra"
)

type Phase string

const (
	PlanPhase    Phase = "plan"
	RuntimePhase Phase = "runtime"
)

type RuntimeEvidence struct {
	Now            time.Time
	Interference   map[string]float64
	Versions       map[string]string
	Metadata       *dra.DeviceMetadata
	DRARequestName string
}

type Reason struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type Decision struct {
	Admissible         bool     `json:"admissible"`
	Phase              Phase    `json:"phase"`
	Profile            string   `json:"profile"`
	ContractDigest     string   `json:"contractDigest,omitempty"`
	PredictedComputeUS float64  `json:"predictedComputeUS"`
	LatencyUpperUS     float64  `json:"latencyUpperUS"`
	AvailableUS        float64  `json:"availableUS"`
	LatencyMarginUS    float64  `json:"latencyMarginUS"`
	OODScore           float64  `json:"oodScore,omitempty"`
	OODThreshold       float64  `json:"oodThreshold,omitempty"`
	Reasons            []Reason `json:"reasons,omitempty"`
}

func (d *Decision) reject(code, field, message string) {
	d.Reasons = append(d.Reasons, Reason{Code: code, Field: field, Message: message})
	d.Admissible = false
}

func Evaluate(p *contract.KernelProfile, r *contract.KernelRequest, phase Phase, ev RuntimeEvidence) Decision {
	d := Decision{Admissible: true, Phase: phase}
	if p != nil {
		d.Profile = p.Metadata.Name
		if p.Seal != nil {
			d.ContractDigest = p.Seal.ContractDigest
		}
	}
	if phase != PlanPhase && phase != RuntimePhase {
		d.reject("INVALID_PHASE", "phase", "phase must be plan or runtime")
		return d
	}
	if err := contract.ValidateProfile(p, true); err != nil {
		d.reject("INVALID_PROFILE", "profile", err.Error())
		return d
	}
	if err := contract.ValidateRequest(r); err != nil {
		d.reject("INVALID_REQUEST", "request", err.Error())
		return d
	}
	if r.Spec.RequiredProfile != "" && r.Spec.RequiredProfile != p.Metadata.Name {
		d.reject("PROFILE_MISMATCH", "spec.requiredProfile", fmt.Sprintf("request requires %q", r.Spec.RequiredProfile))
	}
	now := ev.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := contract.CertificateFresh(p, now); err != nil {
		d.reject("STALE_CERTIFICATE", "profile", err.Error())
	}

	validateInputs(&d, p, r)
	interference := r.Spec.Interference
	versions := r.Spec.Versions
	if phase == RuntimePhase {
		if ev.Interference != nil {
			interference = ev.Interference
		}
		if ev.Versions != nil {
			versions = ev.Versions
		}
	}
	validateInterference(&d, p, interference)
	validateVersions(&d, p, versions)
	validateNumerical(&d, p, r)

	features, featureOK := modelFeatures(&d, p, r, interference)
	if featureOK {
		d.PredictedComputeUS = p.Spec.Latency.Model.InterceptUS
		for _, name := range p.Spec.Latency.Model.FeatureOrder {
			d.PredictedComputeUS += p.Spec.Latency.Model.Coefficients[name] * features[name]
		}
		if !isFinite(d.PredictedComputeUS) || d.PredictedComputeUS < 0 {
			d.reject("INVALID_PREDICTION", "spec.latency.model", "latency model produced a negative or non-finite prediction")
		}
		d.LatencyUpperUS = d.PredictedComputeUS + p.Spec.Latency.ResidualUpperUS + p.Spec.Latency.IOBudgetUS + p.Spec.Latency.RuntimeJitterUS
		d.AvailableUS = r.Spec.DeadlineUS - r.Spec.SafetyMarginUS
		d.LatencyMarginUS = d.AvailableUS - d.LatencyUpperUS
		if !isFinite(d.LatencyUpperUS) || d.LatencyUpperUS < 0 {
			d.reject("INVALID_LATENCY_BOUND", "profile.spec.latency", "composed latency upper bound is negative or non-finite")
		} else if d.LatencyUpperUS > d.AvailableUS {
			d.reject("LATENCY_SLO_UNSATISFIED", "spec.deadlineUS", fmt.Sprintf("certified upper latency %.3fus exceeds available %.3fus", d.LatencyUpperUS, d.AvailableUS))
		}
		validateOOD(&d, p, features)
	}

	if phase == RuntimePhase {
		if ev.Metadata == nil {
			d.reject("DRA_METADATA_REQUIRED", "runtime.metadata", "runtime phase requires DRA metadata")
		} else {
			requestName := ev.DRARequestName
			if requestName == "" {
				requestName = "gpu"
			}
			validateDRA(&d, p, ev.Metadata, requestName)
		}
	}
	sort.SliceStable(d.Reasons, func(i, j int) bool {
		if d.Reasons[i].Code == d.Reasons[j].Code {
			return d.Reasons[i].Field < d.Reasons[j].Field
		}
		return d.Reasons[i].Code < d.Reasons[j].Code
	})
	return d
}

func validateInputs(d *Decision, p *contract.KernelProfile, r *contract.KernelRequest) {
	for name := range r.Spec.Inputs {
		if _, ok := p.Spec.InputDomain.Features[name]; !ok {
			d.reject("UNKNOWN_INPUT", "spec.inputs."+name, "input was not present during contract calibration")
		}
	}
	for name, spec := range p.Spec.InputDomain.Features {
		v, ok := r.Spec.Inputs[name]
		if !ok {
			if spec.Required {
				d.reject("MISSING_INPUT", "spec.inputs."+name, "required input is missing")
			}
			continue
		}
		switch spec.Kind {
		case "number", "integer":
			n, ok := v.Number()
			if !ok {
				d.reject("INPUT_TYPE_MISMATCH", "spec.inputs."+name, "expected numeric input")
				continue
			}
			if spec.Kind == "integer" && math.Trunc(n) != n {
				d.reject("INPUT_TYPE_MISMATCH", "spec.inputs."+name, "expected an integer-valued number")
			}
			if spec.Minimum == nil || spec.Maximum == nil || n < *spec.Minimum || n > *spec.Maximum {
				d.reject("INPUT_OUT_OF_RANGE", "spec.inputs."+name, fmt.Sprintf("%.6g outside calibrated range [%.6g, %.6g]", n, deref(spec.Minimum), deref(spec.Maximum)))
			}
		case "category":
			s, ok := v.String()
			if !ok {
				d.reject("INPUT_TYPE_MISMATCH", "spec.inputs."+name, "expected string category")
				continue
			}
			if !contains(spec.Categories, s) {
				d.reject("UNSEEN_CATEGORY", "spec.inputs."+name, fmt.Sprintf("category %q was not calibrated", s))
			}
		}
	}
	for _, rel := range p.Spec.InputDomain.Relations {
		lv, lok := r.Spec.Inputs[rel.Left].Number()
		rv, rok := r.Spec.Inputs[rel.Right].Number()
		if !lok || !rok {
			continue
		}
		ok := map[string]bool{"<": lv < rv, "<=": lv <= rv, "=": lv == rv, ">=": lv >= rv, ">": lv > rv}[rel.Op]
		if !ok {
			d.reject("RELATION_VIOLATION", "spec.inputs", fmt.Sprintf("relation %s %s %s is false", rel.Left, rel.Op, rel.Right))
		}
	}
}

func validateInterference(d *Decision, p *contract.KernelProfile, obs map[string]float64) {
	for name := range obs {
		if _, ok := p.Spec.Interference.Metrics[name]; !ok {
			d.reject("UNKNOWN_INTERFERENCE", "spec.interference."+name, "metric was not part of calibration")
		}
	}
	for name, rng := range p.Spec.Interference.Metrics {
		v, ok := obs[name]
		if !ok {
			d.reject("MISSING_INTERFERENCE", "spec.interference."+name, "required interference observation is missing")
			continue
		}
		if !isFinite(v) || v < rng.Minimum || v > rng.Maximum {
			d.reject("INTERFERENCE_OUT_OF_ENVELOPE", "spec.interference."+name, fmt.Sprintf("%.6g outside [%.6g, %.6g]", v, rng.Minimum, rng.Maximum))
		}
	}
}
func validateVersions(d *Decision, p *contract.KernelProfile, versions map[string]string) {
	for component, expr := range p.Spec.Versions {
		v, ok := versions[component]
		if !ok {
			d.reject("MISSING_VERSION", "spec.versions."+component, "runtime/compiler version is missing")
			continue
		}
		match, err := contract.MatchVersionRange(v, expr)
		if err != nil {
			d.reject("INVALID_VERSION", "spec.versions."+component, err.Error())
			continue
		}
		if !match {
			d.reject("UNSUPPORTED_VERSION", "spec.versions."+component, fmt.Sprintf("version %s does not satisfy %s", v, expr))
		}
	}
}
func validateNumerical(d *Decision, p *contract.KernelProfile, r *contract.KernelRequest) {
	if p.Spec.Numerical.UpperBound > r.Spec.MaxNumericalError {
		d.reject("NUMERICAL_BUDGET_UNSATISFIED", "spec.maxNumericalError", fmt.Sprintf("certificate upper bound %.6g exceeds request budget %.6g", p.Spec.Numerical.UpperBound, r.Spec.MaxNumericalError))
	}
	if p.Spec.Numerical.ObservedMax > p.Spec.Numerical.UpperBound {
		d.reject("INVALID_NUMERICAL_CERTIFICATE", "profile.spec.numerical", "observed test error exceeds certificate")
	}
}
func modelFeatures(d *Decision, p *contract.KernelProfile, r *contract.KernelRequest, interference map[string]float64) (map[string]float64, bool) {
	out := map[string]float64{}
	ok := true
	// The latency model and OOD detector may intentionally use different
	// feature subsets. Resolve their union once, without requiring an OOD-only
	// feature to have a meaningless latency coefficient.
	names := append([]string(nil), p.Spec.Latency.Model.FeatureOrder...)
	seen := make(map[string]struct{}, len(names)+len(p.Spec.OOD.Features))
	for _, name := range names {
		seen[name] = struct{}{}
	}
	for _, name := range p.Spec.OOD.Features {
		if _, exists := seen[name]; !exists {
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}
	for _, name := range names {
		switch {
		case strings.HasPrefix(name, "input."):
			key := strings.TrimPrefix(name, "input.")
			v, exists := r.Spec.Inputs[key]
			n, nok := v.Number()
			if !exists || !nok {
				d.reject("MODEL_FEATURE_MISSING", "spec.inputs."+key, "numeric latency-model feature missing")
				ok = false
			} else {
				out[name] = n
			}
		case strings.HasPrefix(name, "interference."):
			key := strings.TrimPrefix(name, "interference.")
			v, exists := interference[key]
			if !exists || !isFinite(v) {
				d.reject("MODEL_FEATURE_MISSING", "spec.interference."+key, "latency-model feature missing")
				ok = false
			} else {
				out[name] = v
			}
		default:
			d.reject("INVALID_MODEL_FEATURE", "profile.spec.latency.model", fmt.Sprintf("unsupported feature namespace %q", name))
			ok = false
		}
	}
	return out, ok
}
func validateOOD(d *Decision, p *contract.KernelProfile, features map[string]float64) {
	o := p.Spec.OOD
	if !o.Required {
		return
	}
	n := len(o.Features)
	delta := make([]float64, n)
	for i, name := range o.Features {
		v, ok := features[name]
		if !ok {
			d.reject("OOD_FEATURE_MISSING", name, "OOD feature is unavailable")
			return
		}
		delta[i] = v - o.Mean[i]
	}
	score := 0.0
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			score += delta[i] * o.InverseCovariance[i][j] * delta[j]
		}
	}
	d.OODScore = score
	d.OODThreshold = o.Threshold
	if !isFinite(score) || score < 0 {
		d.reject("INVALID_OOD_SCORE", "profile.spec.ood", "Mahalanobis score is negative or non-finite")
		return
	}
	if score > o.Threshold {
		d.reject("OOD_REJECTED", "spec.inputs", fmt.Sprintf("OOD score %.6g exceeds calibrated threshold %.6g", score, o.Threshold))
	}
}
func validateDRA(d *Decision, p *contract.KernelProfile, m *dra.DeviceMetadata, requestName string) {
	devices, err := dra.DevicesForRequest(m, requestName)
	if err != nil {
		d.reject("DRA_REQUEST_MISSING", "runtime.metadata", err.Error())
		return
	}
	if int64(len(devices)) != p.Spec.Resources.DeviceCount {
		d.reject("DRA_DEVICE_COUNT_MISMATCH", "runtime.metadata", fmt.Sprintf("got %d devices, contract requires %d", len(devices), p.Spec.Resources.DeviceCount))
	}
	for _, dev := range devices {
		for _, a := range p.Spec.DeviceAssertions {
			actual, ok := dev.Attributes[a.Attribute]
			if !ok {
				d.reject("DRA_ATTRIBUTE_MISSING", "runtime.metadata."+a.Attribute, fmt.Sprintf("device %s/%s/%s lacks required attribute", dev.Driver, dev.Pool, dev.Name))
				continue
			}
			if !assertAttribute(actual, a) {
				d.reject("DRA_ATTRIBUTE_MISMATCH", "runtime.metadata."+a.Attribute, fmt.Sprintf("device %s/%s/%s does not satisfy %s", dev.Driver, dev.Pool, dev.Name, a.Op))
			}
		}
	}
}
func assertAttribute(actual dra.Attribute, a contract.DeviceAssertion) bool {
	if a.Op == "contains" {
		return actual.Contains(a.Value)
	}
	v, ok := actual.Scalar()
	if !ok {
		return false
	}
	if a.Op == "eq" {
		return equal(v, a.Value)
	}
	if a.Op == "ne" {
		return !equal(v, a.Value)
	}
	an, aok := v.Number()
	bn, bok := a.Value.Number()
	if !aok || !bok {
		return false
	}
	if a.Op == "gte" {
		return an >= bn
	}
	if a.Op == "lte" {
		return an <= bn
	}
	return false
}
func equal(a, b contract.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case "number":
		x, _ := a.Number()
		y, _ := b.Number()
		return x == y
	case "string":
		x, _ := a.String()
		y, _ := b.String()
		return x == y
	case "bool":
		x, _ := a.Bool()
		y, _ := b.Bool()
		return x == y
	}
	return false
}
func deref(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}
func contains[T comparable](xs []T, x T) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func Select(c *contract.KernelCatalog, r *contract.KernelRequest, phase Phase, ev RuntimeEvidence) (*contract.KernelProfile, Decision) {
	var best *contract.KernelProfile
	var bestDecision Decision
	first := true
	for i := range c.Profiles {
		p := &c.Profiles[i]
		dec := Evaluate(p, r, phase, ev)
		if !dec.Admissible {
			if first {
				bestDecision = dec
				first = false
			}
			continue
		}
		if best == nil || dec.LatencyUpperUS < bestDecision.LatencyUpperUS || (dec.LatencyUpperUS == bestDecision.LatencyUpperUS && p.Metadata.Name < best.Metadata.Name) {
			best = p
			bestDecision = dec
			first = false
		}
	}
	if best == nil {
		if first {
			bestDecision = Decision{Admissible: false, Phase: phase, Reasons: []Reason{{Code: "EMPTY_CATALOG", Message: "catalog has no profiles"}}}
		}
		return nil, bestDecision
	}
	return best, bestDecision
}

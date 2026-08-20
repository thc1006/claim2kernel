package contract

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/thc1006/claim2kernel/pkg/jsonsafe"
)

const (
	MaxContractBytes             = 4 << 20
	MaxCatalogProfiles           = 128
	MaxInputFeatures             = 64
	MaxInputRelations            = 128
	MaxCategoriesPerFeature      = 256
	MaxRequestInputs             = 128
	MaxInterferenceMetrics       = 64
	MaxVersionConstraints        = 64
	MaxDeviceAssertions          = 64
	MaxFallbackProfiles          = 32
	MaxDefaultArguments          = 64
	MaxModelFeatures             = 64
	MaxOODFeatures               = 64
	MaxCertificateAgeSeconds     = 5 * 366 * 24 * 60 * 60
	MaxArgumentBytes             = 4 << 10
	MaxTotalDefaultArgumentBytes = 64 << 10
)

var (
	dnsLabelRE     = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	dnsSubdomainRE = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$`)
	digestRE       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	ociDigestRE    = regexp.MustCompile(`^[a-z0-9./_-]+@sha256:[0-9a-f]{64}$`)
)

type ValidationError struct{ Issues []string }

func (e *ValidationError) Error() string {
	return "contract validation failed: " + strings.Join(e.Issues, "; ")
}

func LoadProfile(data []byte, requireSeal bool) (*KernelProfile, error) {
	var p KernelProfile
	if err := jsonsafe.DecodeStrict(data, &p, MaxContractBytes); err != nil {
		return nil, err
	}
	if err := ValidateProfile(&p, requireSeal); err != nil {
		return nil, err
	}
	return &p, nil
}
func LoadRequest(data []byte) (*KernelRequest, error) {
	var r KernelRequest
	if err := jsonsafe.DecodeStrict(data, &r, MaxContractBytes); err != nil {
		return nil, err
	}
	if err := ValidateRequest(&r); err != nil {
		return nil, err
	}
	return &r, nil
}
func LoadCatalog(data []byte, requireSeal bool) (*KernelCatalog, error) {
	var c KernelCatalog
	if err := jsonsafe.DecodeStrict(data, &c, MaxContractBytes); err != nil {
		return nil, err
	}
	issues := make([]string, 0)
	if c.APIVersion != APIVersion {
		issues = append(issues, "apiVersion must be "+APIVersion)
	}
	if c.Kind != CatalogKind {
		issues = append(issues, "kind must be "+CatalogKind)
	}
	if err := validateName(c.Metadata.Name); err != nil {
		issues = append(issues, "metadata.name: "+err.Error())
	}
	if len(c.Profiles) == 0 {
		issues = append(issues, "profiles must not be empty")
	}
	if len(c.Profiles) > MaxCatalogProfiles {
		issues = append(issues, fmt.Sprintf("profiles exceeds safety limit %d", MaxCatalogProfiles))
	}
	seen := map[string]struct{}{}
	for i := range c.Profiles {
		p := &c.Profiles[i]
		if _, ok := seen[p.Metadata.Name]; ok {
			issues = append(issues, fmt.Sprintf("profiles[%d]: duplicate name %q", i, p.Metadata.Name))
		}
		seen[p.Metadata.Name] = struct{}{}
		if err := ValidateProfile(p, requireSeal); err != nil {
			issues = append(issues, fmt.Sprintf("profiles[%d]: %v", i, err))
		}
	}
	if len(issues) > 0 {
		return nil, &ValidationError{Issues: issues}
	}
	return &c, nil
}
func LoadSignature(data []byte) (*SignatureEnvelope, error) {
	var s SignatureEnvelope
	if err := jsonsafe.DecodeStrict(data, &s, 1<<20); err != nil {
		return nil, err
	}
	if err := ValidateSignatureEnvelope(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func ValidateRequest(r *KernelRequest) error {
	issues := make([]string, 0)
	if r == nil {
		return &ValidationError{Issues: []string{"request is nil"}}
	}
	if r.APIVersion != APIVersion {
		issues = append(issues, "apiVersion must be "+APIVersion)
	}
	if r.Kind != RequestKind {
		issues = append(issues, "kind must be "+RequestKind)
	}
	if err := validateName(r.Metadata.Name); err != nil {
		issues = append(issues, "metadata.name: "+err.Error())
	}
	if !finitePositive(r.Spec.DeadlineUS) {
		issues = append(issues, "spec.deadlineUS must be finite and > 0")
	}
	if !finiteNonNegative(r.Spec.SafetyMarginUS) || r.Spec.SafetyMarginUS >= r.Spec.DeadlineUS {
		issues = append(issues, "spec.safetyMarginUS must be finite, >= 0, and below deadlineUS")
	}
	if !finiteNonNegative(r.Spec.MaxNumericalError) {
		issues = append(issues, "spec.maxNumericalError must be finite and >= 0")
	}
	if len(r.Spec.Inputs) == 0 {
		issues = append(issues, "spec.inputs must not be empty")
	}
	if len(r.Spec.Inputs) > MaxRequestInputs {
		issues = append(issues, fmt.Sprintf("spec.inputs exceeds safety limit %d", MaxRequestInputs))
	}
	for k, v := range r.Spec.Inputs {
		if strings.TrimSpace(k) == "" {
			issues = append(issues, "spec.inputs contains an empty key")
		}
		if v.Kind() == "" {
			issues = append(issues, "spec.inputs."+k+" is unset")
		}
	}
	for k, v := range r.Spec.Interference {
		if strings.TrimSpace(k) == "" || !finite(v) {
			issues = append(issues, "spec.interference contains invalid key or non-finite value")
		}
	}
	if len(r.Spec.Interference) > MaxInterferenceMetrics {
		issues = append(issues, fmt.Sprintf("spec.interference exceeds safety limit %d", MaxInterferenceMetrics))
	}
	for k, v := range r.Spec.Versions {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			issues = append(issues, "spec.versions contains an empty key or value")
		}
	}
	if len(r.Spec.Versions) > MaxVersionConstraints {
		issues = append(issues, fmt.Sprintf("spec.versions exceeds safety limit %d", MaxVersionConstraints))
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func ValidateProfile(p *KernelProfile, requireSeal bool) error {
	issues := validateProfileBase(p)
	if requireSeal {
		if p == nil || p.Seal == nil {
			issues = append(issues, "seal is required")
		} else if err := VerifySeal(p); err != nil {
			issues = append(issues, err.Error())
		}
	} else if p != nil && p.Seal != nil {
		if err := VerifySeal(p); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func validateProfileBase(p *KernelProfile) []string {
	issues := make([]string, 0)
	if p == nil {
		return []string{"profile is nil"}
	}
	if p.APIVersion != APIVersion {
		issues = append(issues, "apiVersion must be "+APIVersion)
	}
	if p.Kind != ProfileKind {
		issues = append(issues, "kind must be "+ProfileKind)
	}
	if err := validateName(p.Metadata.Name); err != nil {
		issues = append(issues, "metadata.name: "+err.Error())
	}
	if p.Metadata.Version == "" {
		issues = append(issues, "metadata.version is required")
	}
	var createdAt, expiresAt time.Time
	var createdOK, expiresOK bool
	if p.Metadata.CreatedAt == "" {
		issues = append(issues, "metadata.createdAt is required")
	} else if parsed, err := ParseTime(p.Metadata.CreatedAt); err != nil {
		issues = append(issues, "metadata.createdAt must be RFC3339")
	} else {
		createdAt, createdOK = parsed, true
	}
	if p.Metadata.ExpiresAt == "" {
		issues = append(issues, "metadata.expiresAt is required")
	} else if parsed, err := ParseTime(p.Metadata.ExpiresAt); err != nil {
		issues = append(issues, "metadata.expiresAt must be RFC3339")
	} else {
		expiresAt, expiresOK = parsed, true
	}
	if createdOK && expiresOK && !createdAt.Before(expiresAt) {
		issues = append(issues, "metadata.createdAt must be before metadata.expiresAt")
	}

	a := p.Spec.Artifact
	cleanArtifactPath := path.Clean(a.Path)
	if a.Path == "" || strings.ContainsRune(a.Path, '\x00') || strings.Contains(a.Path, `\`) || path.IsAbs(a.Path) || cleanArtifactPath == "." || cleanArtifactPath == ".." || strings.HasPrefix(cleanArtifactPath, "../") {
		issues = append(issues, "spec.artifact.path must be a non-empty POSIX relative path contained beneath the artifact root")
	}
	if !digestRE.MatchString(a.Digest) {
		issues = append(issues, "spec.artifact.digest must be sha256:<64 lowercase hex>")
	}
	if a.SizeBytes <= 0 {
		issues = append(issues, "spec.artifact.sizeBytes must be > 0")
	}
	if a.MaxBytes <= 0 || a.MaxBytes < a.SizeBytes {
		issues = append(issues, "spec.artifact.maxBytes must be >= sizeBytes and > 0")
	}
	if a.MaxBytes > 2<<30 {
		issues = append(issues, "spec.artifact.maxBytes exceeds 2 GiB safety ceiling")
	}
	if a.Protocol != "stdin-json-v1" {
		issues = append(issues, "spec.artifact.protocol must be stdin-json-v1")
	}
	if a.ContainerDigest != "" && !ociDigestRE.MatchString(a.ContainerDigest) {
		issues = append(issues, "spec.artifact.containerDigest must be name@sha256:<digest>")
	}
	if len(a.DefaultArgs) > MaxDefaultArguments {
		issues = append(issues, fmt.Sprintf("spec.artifact.defaultArgs exceeds safety limit %d", MaxDefaultArguments))
	}
	totalArgBytes := 0
	for _, arg := range a.DefaultArgs {
		totalArgBytes += len(arg)
		if strings.ContainsRune(arg, '\x00') {
			issues = append(issues, "spec.artifact.defaultArgs must not contain NUL")
		}
		if len(arg) > MaxArgumentBytes {
			issues = append(issues, fmt.Sprintf("spec.artifact.defaultArgs entry exceeds %d bytes", MaxArgumentBytes))
		}
	}
	if totalArgBytes > MaxTotalDefaultArgumentBytes {
		issues = append(issues, fmt.Sprintf("spec.artifact.defaultArgs exceeds total safety limit %d bytes", MaxTotalDefaultArgumentBytes))
	}

	t := p.Spec.Target
	if t.Backend == "" || t.Vendor == "" || t.Architecture == "" {
		issues = append(issues, "spec.target backend, vendor, and architecture are required")
	}
	if err := validateSubdomain(t.DeviceClass); err != nil {
		issues = append(issues, "spec.target.deviceClass: "+err.Error())
	}

	if len(p.Spec.InputDomain.Features) == 0 {
		issues = append(issues, "spec.inputDomain.features must not be empty")
	}
	if len(p.Spec.InputDomain.Features) > MaxInputFeatures {
		issues = append(issues, fmt.Sprintf("spec.inputDomain.features exceeds safety limit %d", MaxInputFeatures))
	}
	featureNames := make([]string, 0, len(p.Spec.InputDomain.Features))
	for name, f := range p.Spec.InputDomain.Features {
		featureNames = append(featureNames, name)
		if strings.TrimSpace(name) == "" {
			issues = append(issues, "feature name must not be empty")
		}
		if len(name) > 128 || strings.ContainsRune(name, '\x00') {
			issues = append(issues, "feature name must be at most 128 bytes and must not contain NUL")
		}
		switch f.Kind {
		case "number", "integer":
			if f.Minimum == nil || f.Maximum == nil || !finite(*f.Minimum) || !finite(*f.Maximum) || *f.Minimum > *f.Maximum {
				issues = append(issues, "feature "+name+" must have a finite minimum <= maximum")
			}
			if len(f.Categories) != 0 {
				issues = append(issues, "numeric feature "+name+" must not have categories")
			}
		case "category":
			if len(f.Categories) == 0 {
				issues = append(issues, "categorical feature "+name+" must list categories")
			}
			if len(f.Categories) > MaxCategoriesPerFeature {
				issues = append(issues, fmt.Sprintf("categorical feature %s exceeds category safety limit %d", name, MaxCategoriesPerFeature))
			}
			seen := map[string]struct{}{}
			for _, c := range f.Categories {
				if c == "" {
					issues = append(issues, "categorical feature "+name+" contains empty category")
				}
				if _, ok := seen[c]; ok {
					issues = append(issues, "categorical feature "+name+" contains duplicate category "+c)
				}
				seen[c] = struct{}{}
			}
		default:
			issues = append(issues, "feature "+name+" has unsupported kind "+f.Kind)
		}
	}
	if len(p.Spec.InputDomain.Relations) > MaxInputRelations {
		issues = append(issues, fmt.Sprintf("spec.inputDomain.relations exceeds safety limit %d", MaxInputRelations))
	}
	for i, rel := range p.Spec.InputDomain.Relations {
		left, leftOK := p.Spec.InputDomain.Features[rel.Left]
		right, rightOK := p.Spec.InputDomain.Features[rel.Right]
		if !leftOK {
			issues = append(issues, fmt.Sprintf("relation %d left feature %q is unknown", i, rel.Left))
		}
		if !rightOK {
			issues = append(issues, fmt.Sprintf("relation %d right feature %q is unknown", i, rel.Right))
		}
		if leftOK && left.Kind != "number" && left.Kind != "integer" {
			issues = append(issues, fmt.Sprintf("relation %d left feature %q must be numeric", i, rel.Left))
		}
		if rightOK && right.Kind != "number" && right.Kind != "integer" {
			issues = append(issues, fmt.Sprintf("relation %d right feature %q must be numeric", i, rel.Right))
		}
		if !contains([]string{"<", "<=", "=", ">=", ">"}, rel.Op) {
			issues = append(issues, fmt.Sprintf("relation %d has unsupported op %q", i, rel.Op))
		}
	}
	if p.Spec.Precision.Storage == "" || p.Spec.Precision.Accumulation == "" {
		issues = append(issues, "spec.precision storage and accumulation are required")
	}
	if p.Spec.Resources.DeviceCount <= 0 {
		issues = append(issues, "spec.resources.deviceCount must be > 0")
	}
	if p.Spec.Resources.MinMemoryBytes < 0 {
		issues = append(issues, "spec.resources.minMemoryBytes must be >= 0")
	}
	if len(p.Spec.Resources.Counters) > 64 {
		issues = append(issues, "spec.resources.counters exceeds safety limit 64")
	}
	for name, quantity := range p.Spec.Resources.Counters {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(quantity) == "" || len(name) > 253 || len(quantity) > 128 {
			issues = append(issues, "spec.resources.counters contains an invalid name or quantity")
		}
	}

	n := p.Spec.Numerical
	if n.Metric == "" || !finiteNonNegative(n.UpperBound) || !finiteNonNegative(n.ObservedMax) || n.ObservedMax > n.UpperBound || n.TestSampleCount <= 0 {
		issues = append(issues, "spec.numerical requires metric, non-negative observedMax <= upperBound, and testSampleCount > 0")
	}
	l := p.Spec.Latency
	if !contains([]string{"split-conformal", "one-sided-tolerance"}, l.Method) {
		issues = append(issues, "spec.latency.method must be split-conformal or one-sided-tolerance")
	}
	if !prob(l.Quantile) || !prob(l.Confidence) || !finiteNonNegative(l.ResidualUpperUS) || !finiteNonNegative(l.IOBudgetUS) || !finiteNonNegative(l.RuntimeJitterUS) {
		issues = append(issues, "spec.latency probabilities and non-negative budgets are invalid")
	}
	if l.CalibrationSampleCount <= 0 || l.TestSampleCount <= 0 {
		issues = append(issues, "spec.latency sample counts must be > 0")
	}
	if !probClosed(l.ObservedCoverage) || l.ObservedCoverage+1e-12 < l.Quantile {
		issues = append(issues, "spec.latency.observedCoverage must be >= quantile on the independent test split")
	}
	if l.MaxAgeSeconds <= 0 || l.MaxAgeSeconds > MaxCertificateAgeSeconds {
		issues = append(issues, fmt.Sprintf("spec.latency.maxAgeSeconds must be in [1,%d]", MaxCertificateAgeSeconds))
	}
	calibratedAt, calibrationErr := ParseTime(l.CalibratedAt)
	if calibrationErr != nil {
		issues = append(issues, "spec.latency.calibratedAt must be RFC3339")
	} else {
		if createdOK && calibratedAt.After(createdAt.Add(5*time.Minute)) {
			issues = append(issues, "spec.latency.calibratedAt must not postdate profile creation by more than five minutes")
		}
		if expiresOK && !calibratedAt.Before(expiresAt) {
			issues = append(issues, "spec.latency.calibratedAt must be before profile expiration")
		}
	}
	if !finite(l.Model.InterceptUS) || !finiteNonNegative(l.Model.RidgeLambda) {
		issues = append(issues, "spec.latency.model intercept/ridge are invalid")
	}
	seenOrder := map[string]struct{}{}
	if len(l.Model.FeatureOrder) > MaxModelFeatures || len(l.Model.Coefficients) > MaxModelFeatures {
		issues = append(issues, fmt.Sprintf("spec.latency.model exceeds feature safety limit %d", MaxModelFeatures))
	}
	for _, name := range l.Model.FeatureOrder {
		if _, ok := seenOrder[name]; ok {
			issues = append(issues, "spec.latency.model.featureOrder contains duplicate "+name)
		}
		seenOrder[name] = struct{}{}
		if _, ok := l.Model.Coefficients[name]; !ok {
			issues = append(issues, "spec.latency.model.coefficients missing "+name)
		}
		if err := validateNumericFeatureReference(name, p); err != nil {
			issues = append(issues, "invalid latency model feature "+name+": "+err.Error())
		}
	}
	if len(l.Model.FeatureOrder) != len(l.Model.Coefficients) {
		issues = append(issues, "spec.latency.model featureOrder and coefficients must have identical keys")
	}
	for name, coeff := range l.Model.Coefficients {
		if !finite(coeff) {
			issues = append(issues, "non-finite latency coefficient "+name)
		}
	}

	for name, r := range p.Spec.Interference.Metrics {
		if name == "" || !finite(r.Minimum) || !finite(r.Maximum) || r.Minimum > r.Maximum {
			issues = append(issues, "invalid interference range "+name)
		}
	}
	if len(p.Spec.Interference.Metrics) > MaxInterferenceMetrics {
		issues = append(issues, fmt.Sprintf("spec.interference.metrics exceeds safety limit %d", MaxInterferenceMetrics))
	}
	for component, expr := range p.Spec.Versions {
		if component == "" || expr == "" {
			issues = append(issues, "spec.versions contains empty key/value")
			continue
		}
		if _, err := MatchVersionRange("1.0.0", expr); err != nil {
			issues = append(issues, "invalid version range for "+component+": "+err.Error())
		}
	}
	if len(p.Spec.Versions) > MaxVersionConstraints {
		issues = append(issues, fmt.Sprintf("spec.versions exceeds safety limit %d", MaxVersionConstraints))
	}
	if len(p.Spec.DeviceAssertions) > MaxDeviceAssertions {
		issues = append(issues, fmt.Sprintf("spec.deviceAssertions exceeds safety limit %d", MaxDeviceAssertions))
	}
	for i, a := range p.Spec.DeviceAssertions {
		if a.Attribute == "" || !contains([]string{"eq", "ne", "gte", "lte", "contains"}, a.Op) || a.Value.Kind() == "" {
			issues = append(issues, fmt.Sprintf("invalid device assertion %d", i))
		}
	}
	o := p.Spec.OOD
	if o.Required {
		if o.Method != "mahalanobis-conformal" {
			issues = append(issues, "required OOD method must be mahalanobis-conformal")
		}
		if len(o.Features) == 0 || len(o.Mean) != len(o.Features) || len(o.InverseCovariance) != len(o.Features) {
			issues = append(issues, "OOD feature, mean, and matrix dimensions do not match")
		}
		if len(o.Features) > MaxOODFeatures {
			issues = append(issues, fmt.Sprintf("OOD features exceeds safety limit %d", MaxOODFeatures))
		}
		seen := map[string]struct{}{}
		for i, name := range o.Features {
			if _, ok := seen[name]; ok {
				issues = append(issues, "OOD features contains duplicate "+name)
			}
			seen[name] = struct{}{}
			if err := validateNumericFeatureReference(name, p); err != nil {
				issues = append(issues, "invalid OOD feature "+name+": "+err.Error())
			}
			if i < len(o.Mean) && !finite(o.Mean[i]) {
				issues = append(issues, "OOD mean contains non-finite value")
			}
		}
		for _, row := range o.InverseCovariance {
			if len(row) != len(o.Features) {
				issues = append(issues, "OOD inverse covariance matrix is not square")
				break
			}
			for _, x := range row {
				if !finite(x) {
					issues = append(issues, "OOD inverse covariance contains non-finite value")
				}
			}
		}
		if len(o.Features) <= MaxOODFeatures && len(o.InverseCovariance) == len(o.Features) && !positiveDefiniteSymmetric(o.InverseCovariance, 1e-10) {
			issues = append(issues, "OOD inverse covariance must be finite, symmetric, and positive definite")
		}
		if !finitePositive(o.Threshold) || !prob(o.Coverage) || o.CalibrationSampleCount <= 0 || !probClosed(o.ObservedTestInlierRate) || o.ObservedTestInlierRate+1e-12 < o.Coverage || !finitePositive(o.Regularization) {
			issues = append(issues, "OOD threshold, coverage, counts, rate, or regularization are invalid")
		}
	}
	if !p.Spec.Policy.FailClosed {
		issues = append(issues, "spec.policy.failClosed must be true in v1alpha1")
	}
	// Automatic fallback traversal is intentionally not part of v1alpha1.
	// Accepting names here without executing the documented policy would create
	// a dangerous false assurance that an alternate certified profile is used.
	if len(p.Spec.Policy.FallbackProfiles) != 0 {
		issues = append(issues, "spec.policy.fallbackProfiles is reserved in v1alpha1; automatic fallback is not implemented")
	}
	pr := p.Spec.Provenance
	if pr.Compiler == "" || !digestRE.MatchString(pr.SourceDigest) || !digestRE.MatchString(pr.DatasetDigest) {
		issues = append(issues, "spec.provenance requires compiler and source/dataset sha256 digests")
	}
	if pr.CompilerDigest != "" && !digestRE.MatchString(pr.CompilerDigest) {
		issues = append(issues, "spec.provenance.compilerDigest must be sha256")
	}
	sort.Strings(featureNames)
	return issues
}

func validateNumericFeatureReference(name string, p *KernelProfile) error {
	switch {
	case strings.HasPrefix(name, "input."):
		key := strings.TrimPrefix(name, "input.")
		f, ok := p.Spec.InputDomain.Features[key]
		if !ok {
			return fmt.Errorf("input %q is not declared", key)
		}
		if f.Kind != "number" && f.Kind != "integer" {
			return fmt.Errorf("input %q is not numeric", key)
		}
		return nil
	case strings.HasPrefix(name, "interference."):
		key := strings.TrimPrefix(name, "interference.")
		if _, ok := p.Spec.Interference.Metrics[key]; !ok {
			return fmt.Errorf("interference metric %q is not declared", key)
		}
		return nil
	default:
		return fmt.Errorf("must use input. or interference. namespace")
	}
}

// positiveDefiniteSymmetric performs a tolerance-aware Cholesky check. A
// regularized inverse covariance is expected to be positive definite; merely
// checking that it is square would allow an attacker or corrupted profile to
// create negative/indefinite OOD scores and silently widen admission.
func positiveDefiniteSymmetric(a [][]float64, tolerance float64) bool {
	n := len(a)
	if n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if len(a[i]) != n {
			return false
		}
		for j := 0; j < n; j++ {
			if !finite(a[i][j]) || math.Abs(a[i][j]-a[j][i]) > tolerance*math.Max(1, math.Max(math.Abs(a[i][j]), math.Abs(a[j][i]))) {
				return false
			}
		}
	}
	l := make([][]float64, n)
	for i := range l {
		l[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			sum := a[i][j]
			for k := 0; k < j; k++ {
				sum -= l[i][k] * l[j][k]
			}
			if i == j {
				if sum <= tolerance || !finite(sum) {
					return false
				}
				l[i][j] = math.Sqrt(sum)
			} else {
				l[i][j] = sum / l[j][j]
			}
		}
	}
	return true
}

func validateName(s string) error {
	if len(s) == 0 || len(s) > 63 || !dnsLabelRE.MatchString(s) {
		return fmt.Errorf("must be a DNS-1123 label of at most 63 characters")
	}
	return nil
}
func validateSubdomain(s string) error {
	if len(s) == 0 || len(s) > 253 || !dnsSubdomainRE.MatchString(s) || strings.Contains(s, "..") {
		return fmt.Errorf("must be a DNS-1123 subdomain of at most 253 characters")
	}
	for _, part := range strings.Split(s, ".") {
		if err := validateName(part); err != nil {
			return fmt.Errorf("invalid DNS label %q", part)
		}
	}
	return nil
}
func finite(v float64) bool            { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func finitePositive(v float64) bool    { return finite(v) && v > 0 }
func finiteNonNegative(v float64) bool { return finite(v) && v >= 0 }
func prob(v float64) bool              { return finite(v) && v > 0 && v < 1 }
func probClosed(v float64) bool        { return finite(v) && v >= 0 && v <= 1 }
func contains[T comparable](xs []T, x T) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func CertificateFresh(p *KernelProfile, now time.Time) error {
	createdAt, err := ParseTime(p.Metadata.CreatedAt)
	if err != nil {
		return err
	}
	if now.Before(createdAt.Add(-5 * time.Minute)) {
		return fmt.Errorf("profile creation time is implausibly in the future")
	}
	if p.Seal != nil {
		sealedAt, err := ParseTime(p.Seal.SealedAt)
		if err != nil {
			return err
		}
		if now.Before(sealedAt.Add(-5 * time.Minute)) {
			return fmt.Errorf("profile seal time is implausibly in the future")
		}
	}
	if p.Metadata.ExpiresAt != "" {
		exp, err := ParseTime(p.Metadata.ExpiresAt)
		if err != nil {
			return err
		}
		if !now.Before(exp) {
			return fmt.Errorf("profile expired at %s", exp.Format(time.RFC3339Nano))
		}
	}
	cal, err := ParseTime(p.Spec.Latency.CalibratedAt)
	if err != nil {
		return err
	}
	if now.Before(cal.Add(-5 * time.Minute)) {
		return errorsf("calibration time is implausibly in the future")
	}
	if now.Sub(cal) > time.Duration(p.Spec.Latency.MaxAgeSeconds)*time.Second {
		return fmt.Errorf("latency certificate is stale: age %s exceeds %ds", now.Sub(cal), p.Spec.Latency.MaxAgeSeconds)
	}
	return nil
}
func errorsf(s string) error { return fmt.Errorf("%s", s) }

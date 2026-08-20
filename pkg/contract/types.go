// Package contract defines the Claim2Kernel resource-to-kernel contract.
package contract

import "time"

const (
	APIVersion    = "claim2kernel.dev/v1alpha1"
	ProfileKind   = "KernelProfile"
	RequestKind   = "KernelRequest"
	CatalogKind   = "KernelCatalog"
	SignatureKind = "KernelProfileSignature"
)

type ObjectMeta struct {
	Name      string `json:"name"`
	UID       string `json:"uid,omitempty"`
	Version   string `json:"version,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type KernelProfile struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   ObjectMeta  `json:"metadata"`
	Spec       ProfileSpec `json:"spec"`
	Seal       *Seal       `json:"seal,omitempty"`
}

type ProfileSpec struct {
	Artifact         ArtifactSpec         `json:"artifact"`
	Target           TargetSpec           `json:"target"`
	InputDomain      InputDomain          `json:"inputDomain"`
	Precision        PrecisionSpec        `json:"precision"`
	Resources        ResourceSpec         `json:"resources"`
	Numerical        NumericalCertificate `json:"numerical"`
	Latency          LatencyCertificate   `json:"latency"`
	Interference     InterferenceEnvelope `json:"interference"`
	Versions         map[string]string    `json:"versions"`
	DeviceAssertions []DeviceAssertion    `json:"deviceAssertions,omitempty"`
	OOD              OODCertificate       `json:"ood"`
	Policy           PolicySpec           `json:"policy"`
	Provenance       Provenance           `json:"provenance"`
}

type ArtifactSpec struct {
	Path            string   `json:"path"`
	Digest          string   `json:"digest"`
	SizeBytes       int64    `json:"sizeBytes"`
	MaxBytes        int64    `json:"maxBytes"`
	Protocol        string   `json:"protocol"`
	DefaultArgs     []string `json:"defaultArgs,omitempty"`
	ContainerDigest string   `json:"containerDigest,omitempty"`
}

type TargetSpec struct {
	Backend      string `json:"backend"`
	Vendor       string `json:"vendor"`
	Architecture string `json:"architecture"`
	DeviceClass  string `json:"deviceClass"`
}

type InputDomain struct {
	Features  map[string]FeatureSpec `json:"features"`
	Relations []Relation             `json:"relations,omitempty"`
}

type FeatureSpec struct {
	Kind       string   `json:"kind"`
	Required   bool     `json:"required"`
	Minimum    *float64 `json:"minimum,omitempty"`
	Maximum    *float64 `json:"maximum,omitempty"`
	Categories []string `json:"categories,omitempty"`
	OODFeature bool     `json:"oodFeature,omitempty"`
}

type Relation struct {
	Left  string `json:"left"`
	Op    string `json:"op"`
	Right string `json:"right"`
}

type PrecisionSpec struct {
	Storage      string `json:"storage"`
	Accumulation string `json:"accumulation"`
}

type ResourceSpec struct {
	DeviceCount    int64             `json:"deviceCount"`
	MinMemoryBytes int64             `json:"minMemoryBytes"`
	Counters       map[string]string `json:"counters,omitempty"`
}

type NumericalCertificate struct {
	Metric          string  `json:"metric"`
	UpperBound      float64 `json:"upperBound"`
	ObservedMax     float64 `json:"observedMax"`
	TestSampleCount int64   `json:"testSampleCount"`
}

type LatencyCertificate struct {
	Method                 string      `json:"method"`
	Quantile               float64     `json:"quantile"`
	Confidence             float64     `json:"confidence"`
	ResidualUpperUS        float64     `json:"residualUpperUS"`
	IOBudgetUS             float64     `json:"ioBudgetUS"`
	RuntimeJitterUS        float64     `json:"runtimeJitterUS"`
	CalibrationSampleCount int64       `json:"calibrationSampleCount"`
	TestSampleCount        int64       `json:"testSampleCount"`
	ObservedCoverage       float64     `json:"observedCoverage"`
	Model                  LinearModel `json:"model"`
	CalibratedAt           string      `json:"calibratedAt"`
	MaxAgeSeconds          int64       `json:"maxAgeSeconds"`
}

type LinearModel struct {
	InterceptUS  float64            `json:"interceptUS"`
	Coefficients map[string]float64 `json:"coefficients"`
	FeatureOrder []string           `json:"featureOrder"`
	RidgeLambda  float64            `json:"ridgeLambda"`
}

type InterferenceEnvelope struct {
	Metrics map[string]Range `json:"metrics"`
}

type Range struct {
	Minimum float64 `json:"minimum"`
	Maximum float64 `json:"maximum"`
}

type DeviceAssertion struct {
	Attribute string `json:"attribute"`
	Op        string `json:"op"`
	Value     Value  `json:"value"`
}

type OODCertificate struct {
	Method                 string      `json:"method"`
	Required               bool        `json:"required"`
	Features               []string    `json:"features"`
	Mean                   []float64   `json:"mean"`
	InverseCovariance      [][]float64 `json:"inverseCovariance"`
	Threshold              float64     `json:"threshold"`
	Coverage               float64     `json:"coverage"`
	CalibrationSampleCount int64       `json:"calibrationSampleCount"`
	ObservedTestInlierRate float64     `json:"observedTestInlierRate"`
	Regularization         float64     `json:"regularization"`
}

type PolicySpec struct {
	FailClosed       bool     `json:"failClosed"`
	FallbackProfiles []string `json:"fallbackProfiles,omitempty"`
}

type Provenance struct {
	Compiler       string `json:"compiler"`
	CompilerDigest string `json:"compilerDigest,omitempty"`
	SourceDigest   string `json:"sourceDigest"`
	DatasetDigest  string `json:"datasetDigest"`
	GitCommit      string `json:"gitCommit,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

type Seal struct {
	ContractDigest string `json:"contractDigest"`
	ModelDigest    string `json:"modelDigest"`
	SealedAt       string `json:"sealedAt"`
}

type KernelRequest struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   ObjectMeta  `json:"metadata"`
	Spec       RequestSpec `json:"spec"`
}

type RequestSpec struct {
	DeadlineUS        float64            `json:"deadlineUS"`
	SafetyMarginUS    float64            `json:"safetyMarginUS"`
	MaxNumericalError float64            `json:"maxNumericalError"`
	Inputs            map[string]Value   `json:"inputs"`
	Interference      map[string]float64 `json:"interference"`
	Versions          map[string]string  `json:"versions"`
	RequiredProfile   string             `json:"requiredProfile,omitempty"`
}

type KernelCatalog struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   ObjectMeta      `json:"metadata"`
	Profiles   []KernelProfile `json:"profiles"`
}

type SignatureEnvelope struct {
	APIVersion     string `json:"apiVersion"`
	Kind           string `json:"kind"`
	Algorithm      string `json:"algorithm"`
	KeyID          string `json:"keyID"`
	ContractDigest string `json:"contractDigest"`
	ArtifactDigest string `json:"artifactDigest"`
	SignedAt       string `json:"signedAt"`
	Signature      string `json:"signature"`
}

func ParseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

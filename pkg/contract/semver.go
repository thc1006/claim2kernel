package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type semVersion struct {
	major, minor, patch int64
	pre                 string
}

var semverRE = regexp.MustCompile(`^[vV]?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func parseSemVersion(s string) (semVersion, error) {
	m := semverRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return semVersion{}, fmt.Errorf("invalid semantic version %q", s)
	}
	parts := make([]int64, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseInt(m[i+1], 10, 64)
		if err != nil {
			return semVersion{}, fmt.Errorf("invalid semantic version %q: %w", s, err)
		}
		parts[i] = v
	}
	return semVersion{major: parts[0], minor: parts[1], patch: parts[2], pre: m[4]}, nil
}

func (v semVersion) compare(o semVersion) int {
	if v.major != o.major {
		if v.major < o.major {
			return -1
		}
		return 1
	}
	if v.minor != o.minor {
		if v.minor < o.minor {
			return -1
		}
		return 1
	}
	if v.patch != o.patch {
		if v.patch < o.patch {
			return -1
		}
		return 1
	}
	if v.pre == o.pre {
		return 0
	}
	if v.pre == "" {
		return 1
	}
	if o.pre == "" {
		return -1
	}
	return comparePrerelease(v.pre, o.pre)
}

func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		ai, aerr := strconv.ParseInt(as[i], 10, 64)
		bi, berr := strconv.ParseInt(bs[i], 10, 64)
		switch {
		case aerr == nil && berr == nil:
			if ai < bi {
				return -1
			}
			return 1
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		default:
			if as[i] < bs[i] {
				return -1
			}
			return 1
		}
	}
	if len(as) < len(bs) {
		return -1
	}
	if len(as) > len(bs) {
		return 1
	}
	return 0
}

// MatchVersionRange supports exact versions, wildcard "*", and comma-separated
// comparisons such as ">=1.0.0b2,<1.1.0". PEP-440 beta spelling like
// 1.0.0b2 is normalized to SemVer 1.0.0-b2 for Mojo version gates.
func MatchVersionRange(version, rangeExpr string) (bool, error) {
	version = normalizeBetaVersion(version)
	v, err := parseSemVersion(version)
	if err != nil {
		return false, err
	}
	rangeExpr = strings.TrimSpace(rangeExpr)
	if rangeExpr == "" || rangeExpr == "*" {
		return true, nil
	}
	for _, raw := range strings.Split(rangeExpr, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			return false, fmt.Errorf("empty comparator in range %q", rangeExpr)
		}
		op := "="
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(part, candidate) {
				op = candidate
				part = strings.TrimSpace(strings.TrimPrefix(part, candidate))
				break
			}
		}
		target, err := parseSemVersion(normalizeBetaVersion(part))
		if err != nil {
			return false, fmt.Errorf("range %q: %w", rangeExpr, err)
		}
		cmp := v.compare(target)
		ok := map[string]bool{"=": cmp == 0, ">": cmp > 0, ">=": cmp >= 0, "<": cmp < 0, "<=": cmp <= 0}[op]
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// pepPrereleaseRE accepts the compact PEP-440 spelling used by current Mojo
// builds (for example 1.0.0b2 and 1.0.0rc1). The numeric component is emitted
// as its own SemVer identifier so b10 correctly sorts after b2; treating the
// complete strings as lexical identifiers would invert that ordering.
var pepPrereleaseRE = regexp.MustCompile(`^([vV]?[0-9]+\.[0-9]+\.[0-9]+)(a|b|rc)([0-9]+)(.*)$`)

func normalizeBetaVersion(s string) string {
	s = strings.TrimSpace(s)
	m := pepPrereleaseRE.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	return m[1] + "-" + m[2] + "." + m[3] + m[4]
}

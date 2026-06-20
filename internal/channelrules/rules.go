package channelrules

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/gobwas/glob"
)

type Rules struct {
	AllowedVersionRanges        []string
	AllowedPrereleasePatterns   []string
	AllowedSourceBranchPatterns []string
	AllowedSourceTagPatterns    []string
}

type Input struct {
	Version      string
	SourceBranch string
	SourceTag    string
}

type Issue struct {
	Field   string
	Rule    string
	Message string
}

type Result struct {
	Valid  bool
	Issues []Issue
}

func NormalizeRules(rules Rules) (Rules, error) {
	var err error
	if rules.AllowedVersionRanges, err = normalizeList("allowedVersionRanges", rules.AllowedVersionRanges); err != nil {
		return Rules{}, err
	}
	if rules.AllowedPrereleasePatterns, err = normalizeList(
		"allowedPrereleasePatterns",
		rules.AllowedPrereleasePatterns,
	); err != nil {
		return Rules{}, err
	}
	if rules.AllowedSourceBranchPatterns, err = normalizeList(
		"allowedSourceBranches",
		rules.AllowedSourceBranchPatterns,
	); err != nil {
		return Rules{}, err
	}
	if rules.AllowedSourceTagPatterns, err = normalizeList(
		"allowedSourceTags",
		rules.AllowedSourceTagPatterns,
	); err != nil {
		return Rules{}, err
	}

	for _, versionRange := range rules.AllowedVersionRanges {
		if _, err := semver.NewConstraint(versionRange); err != nil {
			return Rules{}, fmt.Errorf("allowedVersionRanges contains invalid SemVer range %q: %w", versionRange, err)
		}
	}
	if err := validateGlobPatterns("allowedPrereleasePatterns", rules.AllowedPrereleasePatterns); err != nil {
		return Rules{}, err
	}
	if err := validateGlobPatterns("allowedSourceBranches", rules.AllowedSourceBranchPatterns); err != nil {
		return Rules{}, err
	}
	if err := validateGlobPatterns("allowedSourceTags", rules.AllowedSourceTagPatterns); err != nil {
		return Rules{}, err
	}
	return rules, nil
}

func Evaluate(rules Rules, input Input) (Result, error) {
	rules, err := NormalizeRules(rules)
	if err != nil {
		return Result{}, err
	}

	input.Version = strings.TrimSpace(input.Version)
	input.SourceBranch = strings.TrimSpace(input.SourceBranch)
	input.SourceTag = strings.TrimSpace(input.SourceTag)

	var issues []Issue
	version, err := semver.StrictNewVersion(input.Version)
	if err != nil {
		issues = append(issues, Issue{
			Field:   "version",
			Rule:    "semver",
			Message: "version must be a valid SemVer 2.0 version",
		})
	} else {
		issues = append(issues, evaluateVersionRanges(rules.AllowedVersionRanges, version)...)
		issues = append(issues, evaluatePrereleasePatterns(rules.AllowedPrereleasePatterns, version)...)
	}
	issues = append(issues, evaluateSourceRules(rules, input)...)

	return Result{Valid: len(issues) == 0, Issues: issues}, nil
}

func normalizeList(field string, values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, fmt.Errorf("%s contains an empty value", field)
		}
		if _, ok := seen[trimmed]; ok {
			return nil, fmt.Errorf("%s contains duplicate value %q", field, trimmed)
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result, nil
}

func validateGlobPatterns(field string, patterns []string) error {
	for _, pattern := range patterns {
		if _, err := glob.Compile(pattern); err != nil {
			return fmt.Errorf("%s contains invalid glob pattern %q: %w", field, pattern, err)
		}
	}
	return nil
}

func evaluateVersionRanges(ranges []string, version *semver.Version) []Issue {
	if len(ranges) == 0 {
		return nil
	}
	for _, versionRange := range ranges {
		constraint, err := semver.NewConstraint(versionRange)
		if err == nil && constraint.Check(version) {
			return nil
		}
	}
	return []Issue{{
		Field:   "version",
		Rule:    strings.Join(ranges, ", "),
		Message: "version does not match an allowed range",
	}}
}

func evaluatePrereleasePatterns(patterns []string, version *semver.Version) []Issue {
	prerelease := version.Prerelease()
	if prerelease == "" || len(patterns) == 0 {
		return nil
	}
	for _, pattern := range patterns {
		compiled := glob.MustCompile(pattern)
		if compiled.Match(prerelease) {
			return nil
		}
	}
	return []Issue{{
		Field:   "prerelease",
		Rule:    strings.Join(patterns, ", "),
		Message: "prerelease does not match an allowed pattern",
	}}
}

func evaluateSourceRules(rules Rules, input Input) []Issue {
	hasBranchRules := len(rules.AllowedSourceBranchPatterns) > 0
	hasTagRules := len(rules.AllowedSourceTagPatterns) > 0
	if !hasBranchRules && !hasTagRules {
		return nil
	}
	if input.SourceBranch == "" && input.SourceTag == "" {
		return []Issue{{
			Field:   "source",
			Rule:    "required",
			Message: "sourceBranch or sourceTag is required by this channel",
		}}
	}

	var issues []Issue
	if input.SourceBranch != "" {
		if !hasBranchRules {
			issues = append(issues, Issue{
				Field:   "sourceBranch",
				Rule:    "notAllowed",
				Message: "branch sources are not allowed by this channel",
			})
		} else if !matchesAny(rules.AllowedSourceBranchPatterns, input.SourceBranch) {
			issues = append(issues, Issue{
				Field:   "sourceBranch",
				Rule:    strings.Join(rules.AllowedSourceBranchPatterns, ", "),
				Message: "source branch does not match an allowed pattern",
			})
		}
	}
	if input.SourceTag != "" {
		if !hasTagRules {
			issues = append(issues, Issue{
				Field:   "sourceTag",
				Rule:    "notAllowed",
				Message: "tag sources are not allowed by this channel",
			})
		} else if !matchesAny(rules.AllowedSourceTagPatterns, input.SourceTag) {
			issues = append(issues, Issue{
				Field:   "sourceTag",
				Rule:    strings.Join(rules.AllowedSourceTagPatterns, ", "),
				Message: "source tag does not match an allowed pattern",
			})
		}
	}
	return issues
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if glob.MustCompile(pattern).Match(value) {
			return true
		}
	}
	return false
}

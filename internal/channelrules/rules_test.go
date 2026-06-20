package channelrules

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestNormalizeRulesTrimsAndValidatesDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		rules   Rules
		want    Rules
		wantErr string
	}{
		{
			name: "trims accepted rule lists",
			rules: Rules{
				AllowedVersionRanges:        []string{" >=1.0.0 <2.0.0 "},
				AllowedPrereleasePatterns:   []string{" rc.* "},
				AllowedSourceBranchPatterns: []string{" release/* "},
				AllowedSourceTagPatterns:    []string{" v* "},
			},
			want: Rules{
				AllowedVersionRanges:        []string{">=1.0.0 <2.0.0"},
				AllowedPrereleasePatterns:   []string{"rc.*"},
				AllowedSourceBranchPatterns: []string{"release/*"},
				AllowedSourceTagPatterns:    []string{"v*"},
			},
		},
		{
			name: "rejects empty range entries",
			rules: Rules{
				AllowedVersionRanges: []string{" "},
			},
			wantErr: "allowedVersionRanges contains an empty value",
		},
		{
			name: "rejects duplicate trimmed entries",
			rules: Rules{
				AllowedSourceBranchPatterns: []string{"release/*", " release/* "},
			},
			wantErr: "allowedSourceBranches contains duplicate value",
		},
		{
			name: "rejects invalid semantic version ranges",
			rules: Rules{
				AllowedVersionRanges: []string{">=>1.0.0"},
			},
			wantErr: "allowedVersionRanges contains invalid SemVer range",
		},
		{
			name: "rejects invalid prerelease patterns",
			rules: Rules{
				AllowedPrereleasePatterns: []string{"["},
			},
			wantErr: "allowedPrereleasePatterns contains invalid glob pattern",
		},
		{
			name: "rejects invalid branch globs",
			rules: Rules{
				AllowedSourceBranchPatterns: []string{"["},
			},
			wantErr: "allowedSourceBranches contains invalid glob pattern",
		},
		{
			name: "rejects invalid tag globs",
			rules: Rules{
				AllowedSourceTagPatterns: []string{"["},
			},
			wantErr: "allowedSourceTags contains invalid glob pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := NormalizeRules(tt.rules)

			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(got).To(Equal(tt.want))
			}
		})
	}
}

func TestEvaluateVersionRules(t *testing.T) {
	tests := []struct {
		name      string
		rules     Rules
		input     Input
		wantValid bool
		wantIssue Issue
	}{
		{
			name: "accepts strict semantic versions without configured ranges",
			input: Input{
				Version: "1.2.3+build.5",
			},
			wantValid: true,
		},
		{
			name: "rejects non semantic versions",
			input: Input{
				Version: "1.2",
			},
			wantIssue: Issue{
				Field: "version",
				Rule:  "semver",
			},
		},
		{
			name: "accepts versions inside any configured range",
			rules: Rules{
				AllowedVersionRanges: []string{">=1.0.0 <2.0.0", ">=3.0.0 <4.0.0"},
			},
			input: Input{
				Version: "3.1.0",
			},
			wantValid: true,
		},
		{
			name: "rejects versions outside all configured ranges",
			rules: Rules{
				AllowedVersionRanges: []string{">=1.0.0 <2.0.0"},
			},
			input: Input{
				Version: "2.0.0",
			},
			wantIssue: Issue{
				Field: "version",
				Rule:  ">=1.0.0 <2.0.0",
			},
		},
		{
			name: "accepts prereleases matching configured patterns",
			rules: Rules{
				AllowedPrereleasePatterns: []string{"rc.*"},
			},
			input: Input{
				Version: "1.2.3-rc.1",
			},
			wantValid: true,
		},
		{
			name: "rejects prereleases that miss configured patterns",
			rules: Rules{
				AllowedPrereleasePatterns: []string{"rc.*"},
			},
			input: Input{
				Version: "1.2.3-beta.1",
			},
			wantIssue: Issue{
				Field: "prerelease",
				Rule:  "rc.*",
			},
		},
		{
			name: "allows stable versions when prerelease patterns are configured",
			rules: Rules{
				AllowedPrereleasePatterns: []string{"rc.*"},
			},
			input: Input{
				Version: "1.2.3",
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			result, err := Evaluate(tt.rules, tt.input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Valid).To(Equal(tt.wantValid))
			if !tt.wantValid {
				g.Expect(result.Issues).NotTo(BeEmpty())
				g.Expect(result.Issues[0].Field).To(Equal(tt.wantIssue.Field))
				g.Expect(result.Issues[0].Rule).To(Equal(tt.wantIssue.Rule))
			}
		})
	}
}

func TestEvaluateSourceRules(t *testing.T) {
	tests := []struct {
		name      string
		rules     Rules
		input     Input
		wantValid bool
		wantIssue Issue
	}{
		{
			name: "accepts matching branch globs",
			rules: Rules{
				AllowedSourceBranchPatterns: []string{"main", "release/*"},
			},
			input: Input{
				Version:      "1.2.3",
				SourceBranch: "release/2026.06",
			},
			wantValid: true,
		},
		{
			name: "rejects non matching branch globs",
			rules: Rules{
				AllowedSourceBranchPatterns: []string{"main", "release/*"},
			},
			input: Input{
				Version:      "1.2.3",
				SourceBranch: "feature/demo",
			},
			wantIssue: Issue{
				Field: "sourceBranch",
				Rule:  "main, release/*",
			},
		},
		{
			name: "accepts matching tag globs",
			rules: Rules{
				AllowedSourceTagPatterns: []string{"v*", "release-*"},
			},
			input: Input{
				Version:   "1.2.3",
				SourceTag: "v1.2.3",
			},
			wantValid: true,
		},
		{
			name: "rejects non matching tag globs",
			rules: Rules{
				AllowedSourceTagPatterns: []string{"v*"},
			},
			input: Input{
				Version:   "1.2.3",
				SourceTag: "build-1.2.3",
			},
			wantIssue: Issue{
				Field: "sourceTag",
				Rule:  "v*",
			},
		},
		{
			name: "requires a source when source rules are configured",
			rules: Rules{
				AllowedSourceBranchPatterns: []string{"main"},
			},
			input: Input{
				Version: "1.2.3",
			},
			wantIssue: Issue{
				Field: "source",
				Rule:  "required",
			},
		},
		{
			name: "rejects tag sources when only branch rules are configured",
			rules: Rules{
				AllowedSourceBranchPatterns: []string{"main"},
			},
			input: Input{
				Version:   "1.2.3",
				SourceTag: "v1.2.3",
			},
			wantIssue: Issue{
				Field: "sourceTag",
				Rule:  "notAllowed",
			},
		},
		{
			name: "allows absent source when no source rules are configured",
			input: Input{
				Version: "1.2.3",
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			result, err := Evaluate(tt.rules, tt.input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Valid).To(Equal(tt.wantValid))
			if !tt.wantValid {
				g.Expect(result.Issues).NotTo(BeEmpty())
				g.Expect(result.Issues[0].Field).To(Equal(tt.wantIssue.Field))
				g.Expect(result.Issues[0].Rule).To(Equal(tt.wantIssue.Rule))
			}
		})
	}
}

func TestEvaluateSourceRulesDoesNotRequireVersion(t *testing.T) {
	g := NewWithT(t)

	result, err := EvaluateSource(Rules{
		AllowedSourceBranchPatterns: []string{"main"},
	}, Input{SourceBranch: "main"})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
}

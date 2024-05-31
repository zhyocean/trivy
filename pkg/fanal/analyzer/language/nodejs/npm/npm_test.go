package npm

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zhyocean/trivy/pkg/fanal/analyzer"
	"github.com/zhyocean/trivy/pkg/fanal/types"
	"github.com/zhyocean/trivy/pkg/log"
)

func TestMain(m *testing.M) {
	_ = log.InitLogger(false, true)
	os.Exit(m.Run())
}

func Test_npmLibraryAnalyzer_Analyze(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want *analyzer.AnalysisResult
	}{
		{
			name: "with node_modules",
			dir:  "testdata/happy",
			want: &analyzer.AnalysisResult{
				Applications: []types.Application{
					{
						Type:     types.Npm,
						FilePath: "package-lock.json",
						Libraries: []types.Package{
							{
								ID:       "array-flatten@1.1.1",
								Name:     "array-flatten",
								Version:  "1.1.1",
								Indirect: true,
								Locations: []types.Location{
									{
										StartLine: 12,
										EndLine:   16,
									},
								},
							},
							{
								ID:        "body-parser@1.18.3",
								Name:      "body-parser",
								Version:   "1.18.3",
								Indirect:  true,
								DependsOn: []string{"debug@2.6.9"},
								Licenses:  []string{"MIT"},
								Locations: []types.Location{
									{
										StartLine: 17,
										EndLine:   39,
									},
								},
							},
							{
								ID:        "debug@2.6.9",
								Name:      "debug",
								Version:   "2.6.9",
								Indirect:  true,
								DependsOn: []string{"ms@2.0.0"},
								Licenses:  []string{"MIT"},
								Locations: []types.Location{
									{
										StartLine: 25,
										EndLine:   32,
									},
									{
										StartLine: 48,
										EndLine:   55,
									},
								},
							},
							{
								ID:        "express@4.16.4",
								Name:      "express",
								Version:   "4.16.4",
								Indirect:  true,
								DependsOn: []string{"debug@2.6.9"},
								Licenses:  []string{"MIT"},
								Locations: []types.Location{
									{
										StartLine: 40,
										EndLine:   62,
									},
								},
							},
							{
								ID:       "ms@2.0.0",
								Name:     "ms",
								Version:  "2.0.0",
								Indirect: true,
								Licenses: []string{"MIT"},
								Locations: []types.Location{
									{
										StartLine: 33,
										EndLine:   37,
									},
									{
										StartLine: 56,
										EndLine:   60,
									},
								},
							},
							{
								ID:       "ms@2.1.1",
								Name:     "ms",
								Version:  "2.1.1",
								Indirect: true,
								Licenses: []string{"MIT"},
								Locations: []types.Location{
									{
										StartLine: 63,
										EndLine:   67,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "without node_modules",
			dir:  "testdata/no-node_modules",
			want: &analyzer.AnalysisResult{
				Applications: []types.Application{
					{
						Type:     types.Npm,
						FilePath: "package-lock.json",
						Libraries: []types.Package{
							{
								ID:       "ms@2.1.1",
								Name:     "ms",
								Version:  "2.1.1",
								Indirect: true,
								Locations: []types.Location{
									{
										StartLine: 6,
										EndLine:   10,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sad path",
			dir:  "testdata/sad",
			want: &analyzer.AnalysisResult{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := newNpmLibraryAnalyzer(analyzer.AnalyzerOptions{})
			require.NoError(t, err)

			got, err := a.PostAnalyze(context.Background(), analyzer.PostAnalysisInput{
				FS: os.DirFS(tt.dir),
			})

			assert.NoError(t, err)
			if len(got.Applications) > 0 {
				sortPkgs(got.Applications[0].Libraries)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func sortPkgs(libs []types.Package) {
	sort.Slice(libs, func(i, j int) bool {
		ret := strings.Compare(libs[i].Name, libs[j].Name)
		if ret == 0 {
			return libs[i].Version < libs[j].Version
		}
		return ret < 0
	})
	for _, lib := range libs {
		sortLocations(lib.Locations)
	}
}

func sortLocations(locs []types.Location) {
	sort.Slice(locs, func(i, j int) bool {
		return locs[i].StartLine < locs[j].StartLine
	})
}

func Test_nodePkgLibraryAnalyzer_Required(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{
			name:     "lock file",
			filePath: "npm/package-lock.json",
			want:     true,
		},
		{
			name:     "package.json",
			filePath: "npm/node_modules/ms/package.json",
			want:     true,
		},
		{
			name:     "sad path",
			filePath: "npm/node_modules/package.json",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := npmLibraryAnalyzer{}
			got := a.Required(tt.filePath, nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

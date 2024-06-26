package githubapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-test/deep"
	"github.com/google/go-github/v52/github"
	"github.com/migueleliasweb/go-github-mock/src/mock"
	log "github.com/sirupsen/logrus"
	cfg "github.com/wayfair-incubator/telefonistka/internal/pkg/configuration"
)

func generatePromotionPlanTestHelper(t *testing.T, config *cfg.Config, expectedPromotion map[string]PromotionInstance, mockedHTTPClient *http.Client) {
	t.Helper()
	ctx := context.Background()
	ghClientPair := GhClientPair{v3Client: github.NewClient(mockedHTTPClient)}
	labelName := "fast-promotion"

	ghPrClientDetails := GhPrClientDetails{
		Ctx:          ctx,
		GhClientPair: &ghClientPair,
		Owner:        "AnOwner",
		Repo:         "Arepo",
		PrNumber:     120,
		Ref:          "Abranch",
		PrLogger: log.WithFields(log.Fields{
			"repo":     "AnOwner/Arepo",
			"prNumber": 120,
		}),
		Labels: []*github.Label{
			{Name: &labelName},
		},
	}

	promotionPlan, err := GeneratePromotionPlan(ghPrClientDetails, config, "main")
	if err != nil {
		t.Fatalf("Failed to generate promotion plan: err=%s", err)
	}

	if diff := deep.Equal(expectedPromotion, promotionPlan); diff != nil {
		for _, l := range diff {
			t.Error(l)
		}
	}
}

func TestGeneratePromotionConditionalPlan(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/us-east-4/",
				Conditions: cfg.Condition{
					PrHasLabels: []string{
						"non-existing-label", // Fake label not used in the PR struct
					},
				},
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/not-selected-1/",
							"prod/not-selected-2/",
						},
					},
				},
			},
			{
				SourcePath: "prod/us-east-4/",
				Conditions: cfg.Condition{
					PrHasLabels: []string{
						"fast-promotion", // This label is used in the PR struct
					},
				},
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
			{
				SourcePath: "prod/us-east-4/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/not-selected-3/", // Just a catch-all that shouldn't be used
							"prod/not-selected-4/",
						},
					},
				},
			},
		},
	}

	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "prod/us-east-4/componentA",
				"prod/eu-west-1/componentA": "prod/us-east-4/componentA",
			},
		},
	}
	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/file2.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/aSubDir/file3.yaml")},
				{Filename: github.String(".ci-config/random-file.json")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestAggregatePromotionPlan(t *testing.T) {
	// This one tests two identical components that match the same source regex should only generate a single promotion
	// This is relevant for PRs that where generated by a multi target promotion plan.
	// It should only aggregate components that match the same SourcePath regex and targets , this is why dev/us-east-4 and dev/us-east-5 are combined
	// And lab/us-east-5 should not
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "dev/[^/]*/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
			{
				SourcePath: "lab/[^/]*/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/us-west-1/",
							"prod/us-east-1/",
						},
					},
				},
			},
		},
	}

	expectedPromotion := map[string]PromotionInstance{
		"dev/[^/]*/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "dev/us-east-5/componentA",
				"prod/eu-west-1/componentA": "dev/us-east-5/componentA",
			},
		},
		"lab/[^/]*/>prod/us-east-1/|prod/us-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/us-east-1/componentA": "lab/us-east-5/componentA",
				"prod/us-west-1/componentA": "lab/us-east-5/componentA",
			},
		},
	}

	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("dev/us-east-4/componentA/file.yaml")},
				{Filename: github.String("dev/us-east-5/componentA/file.yaml")},
				{Filename: github.String("lab/us-east-5/componentA/file.yaml")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)

	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGenerateSourceRegexPromotionPlan(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/[^/]*/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
		},
	}
	expectedPromotion := map[string]PromotionInstance{
		"prod/[^/]*/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "prod/us-east-4/componentA",
				"prod/eu-west-1/componentA": "prod/us-east-4/componentA",
			},
		},
	}

	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/file2.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/aSubDir/file3.yaml")},
				{Filename: github.String(".ci-config/random-file.json")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGeneratePromotionPlan(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/us-east-4/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
		},
	}
	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "prod/us-east-4/componentA",
				"prod/eu-west-1/componentA": "prod/us-east-4/componentA",
			},
		},
	}
	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/file2.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/aSubDir/file3.yaml")},
				{Filename: github.String(".ci-config/random-file.json")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGeneratePromotionPlanBlockList(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/us-east-4/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
		},
	}

	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "prod/us-east-4/componentA",
			},
		},
	}

	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String(".ci-config/random-file.json")},
			},
		),
		mock.WithRequestMatch(
			mock.GetReposContentsByOwnerByRepoByPath,
			github.RepositoryContent{
				Content: github.String("promotionTargetBlockList: [\"prod/eu-west-.*\"]"),
			},
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGeneratePromotionPlanAllowList(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/us-east-4/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
		},
	}

	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-west-1/componentA": "prod/us-east-4/componentA",
			},
		},
	}
	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String(".ci-config/random-file.json")},
			},
		),
		mock.WithRequestMatch(
			mock.GetReposContentsByOwnerByRepoByPath,
			github.RepositoryContent{
				Content: github.String("promotionTargetAllowList: [\"prod/eu-(west|foo|bar).*\"]"),
			},
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGeneratePromotionPlanTwoComponents(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/us-east-4/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
		},
	}
	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "prod/us-east-4/componentA",
				"prod/eu-west-1/componentA": "prod/us-east-4/componentA",
				"prod/eu-east-1/componentB": "prod/us-east-4/componentB",
				"prod/eu-west-1/componentB": "prod/us-east-4/componentB",
			},
		},
	}
	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String("prod/us-east-4/componentB/file.yaml")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGenerateNestedSourceRegexPromotionPlan(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath:              "prod/us-east-4/",
				ComponentPathExtraDepth: 2,
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
						},
					},
				},
			},
		},
	}
	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-west-1/teamA/namespaceB/componentA": "prod/us-east-4/teamA/namespaceB/componentA",
			},
		},
	}

	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatch(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/teamA/namespaceB/componentA/file.yaml")},
				{Filename: github.String("prod/us-east-4/teamA/namespaceB/componentA/aSubDir/file3.yaml")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

func TestGeneratePromotionPlanWithPagination(t *testing.T) {
	t.Parallel()
	config := &cfg.Config{
		PromotionPaths: []cfg.PromotionPath{
			{
				SourcePath: "prod/us-east-4/",
				PromotionPrs: []cfg.PromotionPr{
					{
						TargetPaths: []string{
							"prod/eu-west-1/",
							"prod/eu-east-1/",
						},
					},
				},
			},
		},
	}
	expectedPromotion := map[string]PromotionInstance{
		"prod/us-east-4/>prod/eu-east-1/|prod/eu-west-1/": {
			ComputedSyncPaths: map[string]string{
				"prod/eu-east-1/componentA": "prod/us-east-4/componentA",
				"prod/eu-west-1/componentA": "prod/us-east-4/componentA",
			},
		},
	}
	// Note the "relevant" files are in the second page, to ensure pagination is working
	mockedHTTPClient := mock.NewMockedHTTPClient(
		mock.WithRequestMatchPages(
			mock.GetReposPullsFilesByOwnerByRepoByPullNumber,
			[]github.CommitFile{
				{Filename: github.String(".ci-config/random-file.json")},
				{Filename: github.String(".ci-config/random-file2.json")},
			},
			[]github.CommitFile{
				{Filename: github.String("prod/us-east-4/componentA/file.yaml")},
				{Filename: github.String("prod/us-east-4/componentA/file2.yaml")},
			},
		),
		mock.WithRequestMatchHandler(
			mock.GetReposContentsByOwnerByRepoByPath,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mock.WriteError(
					w,
					http.StatusNotFound,
					"no *optional* in-component telefonistka config file",
				)
			}),
		),
	)
	generatePromotionPlanTestHelper(t, config, expectedPromotion, mockedHTTPClient)
}

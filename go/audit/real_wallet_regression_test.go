package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kevin/cryptotax/types"
)

type documentedFixtureGroup struct {
	Label                             string                  `json:"label"`
	Category                          string                  `json:"category"`
	Status                            string                  `json:"status"`
	CurrentBehaviorSummary            string                  `json:"current_behavior_summary"`
	GroupHumanConfirmationPlaceholder string                  `json:"group_human_confirmation_placeholder"`
	Cases                             []documentedFixtureCase `json:"cases"`
}

type documentedFixtureCase struct {
	Label                         string              `json:"label"`
	InputFile                     string              `json:"input_file"`
	DocumentedCurrentBehavior     string              `json:"documented_current_behavior"`
	HumanConfirmationPlaceholder  string              `json:"human_confirmation_placeholder"`
	CurrentNormalizedTransactions []types.Transaction `json:"current_normalized_transactions"`
}

// These fixtures freeze documented normalized rows from the audit snapshot; they
// are not source-truth or tax-correctness validations.
func TestDocumentedRealWalletFixturesMatchCurrentNormalizedRows(t *testing.T) {
	t.Parallel()

	expectationPaths, err := filepath.Glob(filepath.Join("testdata", "real-wallet", "*.expected.json"))
	if err != nil {
		t.Fatalf("glob expected fixtures: %v", err)
	}
	sort.Strings(expectationPaths)

	if len(expectationPaths) == 0 {
		t.Fatal("expected at least one documented real-wallet fixture")
	}

	for _, expectationPath := range expectationPaths {
		expectation := loadDocumentedFixtureGroup(t, expectationPath)

		if expectation.Status != "documented_current_normalization" {
			t.Fatalf("%s: unexpected status %q", expectationPath, expectation.Status)
		}
		if strings.TrimSpace(expectation.CurrentBehaviorSummary) == "" {
			t.Fatalf("%s: current_behavior_summary must be populated", expectationPath)
		}
		assertTODOPlaceholder(t, expectationPath, expectation.GroupHumanConfirmationPlaceholder)

		t.Run(expectation.Label, func(t *testing.T) {
			for _, documentedCase := range expectation.Cases {
				documentedCase := documentedCase
				t.Run(documentedCase.Label, func(t *testing.T) {
					if strings.TrimSpace(documentedCase.DocumentedCurrentBehavior) == "" {
						t.Fatalf("%s: documented_current_behavior must be populated", documentedCase.Label)
					}
					assertTODOPlaceholder(t, documentedCase.Label, documentedCase.HumanConfirmationPlaceholder)

					payload, err := LoadPayload(filepath.Join(filepath.Dir(expectationPath), documentedCase.InputFile))
					if err != nil {
						t.Fatalf("load fixture input %s: %v", documentedCase.InputFile, err)
					}

					if got, want := len(payload.Transactions), len(documentedCase.CurrentNormalizedTransactions); got != want {
						t.Fatalf("%s: expected %d transactions, got %d", documentedCase.Label, want, got)
					}

					for i, want := range documentedCase.CurrentNormalizedTransactions {
						got := payload.Transactions[i]
						if gotJSON, wantJSON := mustMarshalJSON(t, got), mustMarshalJSON(t, want); gotJSON != wantJSON {
							t.Fatalf("%s transaction %d mismatch\nexpected:\n%s\nactual:\n%s", documentedCase.Label, i, wantJSON, gotJSON)
						}
					}
				})
			}
		})
	}
}

func loadDocumentedFixtureGroup(t *testing.T, path string) documentedFixtureGroup {
	t.Helper()

	fixtureJSON, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documented fixture %s: %v", path, err)
	}

	var fixture documentedFixtureGroup
	if err := json.Unmarshal(fixtureJSON, &fixture); err != nil {
		t.Fatalf("unmarshal documented fixture %s: %v", path, err)
	}

	return fixture
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}

	return string(payload)
}

func assertTODOPlaceholder(t *testing.T, label, placeholder string) {
	t.Helper()

	placeholder = strings.TrimSpace(placeholder)
	if placeholder == "" {
		t.Fatalf("%s: placeholder must be populated", label)
	}
	if !strings.HasPrefix(placeholder, "TODO") {
		t.Fatalf("%s: placeholder must start with TODO, got %q", label, placeholder)
	}
}

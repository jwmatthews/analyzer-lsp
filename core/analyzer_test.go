package core

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/konveyor/analyzer-lsp/engine"
	"github.com/konveyor/analyzer-lsp/progress"
	"github.com/konveyor/analyzer-lsp/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzer_RuleLabels(t *testing.T) {
	// Test with no ruleset
	analyzer := &analyzer{
		log: logr.Discard(),
	}
	labels := analyzer.RuleLabels()
	assert.Empty(t, labels)
}

func TestAnalyzer_GetProviderForLanguage(t *testing.T) {
	analyzer := &analyzer{
		log: logr.Discard(),
		allConfigProviders: map[string]provider.InternalProviderClient{
			"builtin": &mockProviderClient{},
			"java":    &mockProviderClient{},
		},
	}

	tests := []struct {
		name         string
		language     string
		expectFound  bool
		expectedName string
	}{
		{
			name:         "find builtin provider",
			language:     "builtin",
			expectFound:  true,
			expectedName: "builtin",
		},
		{
			name:         "find java provider",
			language:     "java",
			expectFound:  true,
			expectedName: "java",
		},
		{
			name:        "provider not found",
			language:    "go",
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, found := analyzer.GetProviderForLanguage(tt.language)

			assert.Equal(t, tt.expectFound, found)
			if tt.expectFound {
				assert.Equal(t, tt.expectedName, prov.Name)
			}
		})
	}
}

func TestAnalyzer_GetProviders(t *testing.T) {
	analyzer := &analyzer{
		log:       logr.Discard(),
		providers: []Provider{},
	}

	// Before parsing rules, providers list is empty
	providers := analyzer.GetProviders()
	assert.Empty(t, providers)
}

func TestAnalyzer_Run_WithoutRules(t *testing.T) {
	analyzer := &analyzer{
		log: logr.Discard(),
	}

	// Run without parsing rules should return nil
	rulesets := analyzer.Run()
	assert.Nil(t, rulesets)
}

func TestAnalyzer_Run_WithoutProviders(t *testing.T) {
	analyzer := &analyzer{
		log:     logr.Discard(),
		ruleset: []engine.RuleSet{{Name: "test"}},
	}

	// Run without providers should return nil
	rulesets := analyzer.Run()
	assert.Nil(t, rulesets)
}

func TestAnalyzer_ProviderStart(t *testing.T) {
	analyzer := &analyzer{
		log: logr.Discard(),
	}

	// ProviderStart without ParseRules should fail
	err := analyzer.ProviderStart()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no providers to start")
}

func TestAnalyzer_GetProviders_WithFilters(t *testing.T) {
	// Create analyzer with mock providers
	analyzer := &analyzer{
		providers: []Provider{
			{
				Name: "java",
				provider: &mockProviderClient{
					capabilities: []provider.Capability{
						{Name: "dependency"},
					},
				},
			},
			{
				Name: "go",
				provider: &mockProviderClient{
					capabilities: []provider.Capability{
						{Name: "referenced"},
					},
				},
			},
			{
				Name: "python",
				provider: &mockProviderClient{
					capabilities: []provider.Capability{
						{Name: "dependency"},
						{Name: "referenced"},
					},
				},
			},
		},
		log: logr.Discard(),
	}

	tests := []struct {
		name          string
		filters       []Filter
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "no filters returns all providers",
			filters:       []Filter{},
			expectedCount: 3,
			expectedNames: []string{"java", "go", "python"},
		},
		{
			name: "filter by dependency capability",
			filters: []Filter{
				FilterByCapability("dependency"),
			},
			expectedCount: 2,
			expectedNames: []string{"java", "python"},
		},
		{
			name: "filter by referenced capability",
			filters: []Filter{
				FilterByCapability("referenced"),
			},
			expectedCount: 2,
			expectedNames: []string{"go", "python"},
		},
		{
			name: "filter by non-existent capability",
			filters: []Filter{
				FilterByCapability("non-existent"),
			},
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := analyzer.GetProviders(tt.filters...)

			assert.Equal(t, tt.expectedCount, len(providers))

			names := make([]string, len(providers))
			for i, p := range providers {
				names[i] = p.Name
			}

			for _, expectedName := range tt.expectedNames {
				assert.Contains(t, names, expectedName)
			}
		})
	}
}

func TestAnalyzer_ParseRules_UsesExplicitPaths(t *testing.T) {
	// Set up a progress instance
	prog, err := progress.New()
	require.NoError(t, err)

	// Create analyzer with rulePaths pointing to one file
	a := &analyzer{
		log: logr.Discard(),
		parserConfig: parserConfig{
			rulePaths: []string{"../parser/testdata/rule-simple-default.yaml"},
		},
		allConfigProviders: map[string]provider.InternalProviderClient{
			"builtin": &mockProviderClient{
				capabilities: []provider.Capability{
					{Name: "file"},
					{Name: "filecontent"},
					{Name: "xml"},
					{Name: "json"},
					{Name: "tag"},
				},
			},
		},
		progress: prog,
	}

	// Call ParseRules with an explicit different path
	_, err = a.ParseRules("../parser/testdata/valid-tag-rule.yaml")
	require.NoError(t, err)

	// Should have parsed the explicit path (tag rule), not the struct path
	require.Len(t, a.ruleset, 1)
	// The tag rule has ruleID "tag-001"
	found := false
	for _, rs := range a.ruleset {
		for _, r := range rs.Rules {
			if r.RuleID == "tag-001" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected rules from explicit path (tag-001), not from struct path (file-001)")
}

// newTestAnalyzerForProviderStart creates an analyzer with the given providers
// and timeout, suitable for testing ProviderStart behavior.
func newTestAnalyzerForProviderStart(t *testing.T, providers []Provider, timeout *time.Duration) *analyzer {
	t.Helper()
	prog, err := progress.New()
	require.NoError(t, err)

	return &analyzer{
		log:                 logr.Discard(),
		ctx:                 context.Background(),
		providers:           providers,
		allConfigProviders:  map[string]provider.InternalProviderClient{"test": &mockProviderClient{}},
		collector:           &mockReporter{},
		progress:            prog,
		providerInitTimeout: timeout,
	}
}

func TestProviderStart_FastProvider_CompletesBeforeTimeout(t *testing.T) {
	timeout := 5 * time.Second
	a := newTestAnalyzerForProviderStart(t, []Provider{
		{Name: "fast-provider", provider: &mockProviderClient{}},
	}, &timeout)

	start := time.Now()
	err := a.ProviderStart()
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "fast provider should complete well before timeout")
}

func TestProviderStart_SlowProvider_TimesOut(t *testing.T) {
	timeout := 200 * time.Millisecond
	a := newTestAnalyzerForProviderStart(t, []Provider{
		{Name: "slow-provider", provider: &mockProviderClient{initDelay: 10 * time.Second}},
	}, &timeout)

	start := time.Now()
	err := a.ProviderStart()
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.InDelta(t, timeout.Seconds(), elapsed.Seconds(), 0.2,
		"should return close to the timeout duration, not wait for provider to finish")
}

func TestProviderStart_ContextCancelled_ReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	timeout := 5 * time.Minute
	prog, err := progress.New()
	require.NoError(t, err)

	a := &analyzer{
		log:                 logr.Discard(),
		ctx:                 ctx,
		providers:           []Provider{{Name: "slow-provider", provider: &mockProviderClient{initDelay: 10 * time.Second}}},
		allConfigProviders:  map[string]provider.InternalProviderClient{"test": &mockProviderClient{}},
		collector:           &mockReporter{},
		progress:            prog,
		providerInitTimeout: &timeout,
	}

	// Cancel the context after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err = a.ProviderStart()
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
	assert.Less(t, elapsed, 2*time.Second, "should return promptly after cancellation, not wait for provider")
}

func TestProviderStart_NoTimeout_WaitsForCompletion(t *testing.T) {
	zeroTimeout := time.Duration(0)
	delay := 300 * time.Millisecond
	a := newTestAnalyzerForProviderStart(t, []Provider{
		{Name: "medium-provider", provider: &mockProviderClient{initDelay: delay}},
	}, &zeroTimeout)

	start := time.Now()
	err := a.ProviderStart()
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, delay, "with zero timeout (no timeout), should wait for provider to finish")
}

func TestProviderStart_NilTimeout_DoesNotFailWithDefault(t *testing.T) {
	// Smoke test: when providerInitTimeout is nil, ProviderStart uses a default
	// timeout and does not fail or immediately expire.

	a := newTestAnalyzerForProviderStart(t, []Provider{
		{Name: "fast-provider", provider: &mockProviderClient{}},
	}, nil) // nil = default 8 min timeout

	err := a.ProviderStart()
	assert.NoError(t, err, "fast provider with default timeout should succeed")
}

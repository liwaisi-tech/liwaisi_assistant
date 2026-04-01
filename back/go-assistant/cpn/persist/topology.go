package persist

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// CPNTopology is the serializable representation of a CPN instance.
type CPNTopology struct {
	ID          string                        `json:"id"`
	Role        string                        `json:"role"`
	Depth       int                           `json:"depth"`
	Mode        string                        `json:"mode"`
	Places      map[string]PlaceTopology      `json:"places"`
	Transitions map[string]TransitionTopology `json:"transitions"`
}

// PlaceTopology is the serializable representation of a CPN place.
type PlaceTopology struct {
	ID    string `json:"id"`
	Color string `json:"color"`
	Space string `json:"space"`
}

// TransitionTopology is the serializable representation of a CPN transition.
type TransitionTopology struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	InputPlaces  []string `json:"inputPlaces"`
	OutputPlaces []string `json:"outputPlaces"`
	ErrorPlace   string   `json:"errorPlace,omitempty"`

	GuardFunc    string `json:"guardFunc,omitempty"`
	ExecutorFunc string `json:"executorFunc,omitempty"`
	FactoryFunc  string `json:"factoryFunc,omitempty"`

	EventFilterFunc string `json:"eventFilterFunc,omitempty"`
	RetryOnFunc     string `json:"retryOnFunc,omitempty"`

	SystemPrompt   string                  `json:"systemPrompt,omitempty"`
	ToolName       string                  `json:"toolName,omitempty"`
	LLMConfig      *LLMConfigTopology      `json:"llmConfig,omitempty"`
	LLMTools       []string                `json:"llmTools,omitempty"`
	ValidateConfig *ValidateConfigTopology `json:"validateConfig,omitempty"`
	HITLConfig     *HITLConfigTopology     `json:"hitlConfig,omitempty"`
	ObservedCPNID  string                  `json:"observedCPNID,omitempty"`

	Retry          *RetryPolicyTopology `json:"retry,omitempty"`
	SubNetTopology *CPNTopology         `json:"subNetTopology,omitempty"`
}

// LLMConfigTopology is the serializable representation of LLMConfig.
type LLMConfigTopology struct {
	Model          string                      `json:"model,omitempty"`
	FallbackModels []string                    `json:"fallbackModels,omitempty"`
	Endpoint       string                      `json:"endpoint,omitempty"`
	MaxTokens      int                         `json:"maxTokens,omitempty"`
	Temperature    float64                     `json:"temperature,omitempty"`
	StreamOutput   bool                        `json:"streamOutput,omitempty"`
	RequireJSON    bool                        `json:"requireJSON,omitempty"`
	JSONSchema     *JSONSchemaConfigTopology   `json:"jsonSchema,omitempty"`
	Budget         float64                     `json:"budget,omitempty"`
	Provider       *ProviderConfigTopology     `json:"provider,omitempty"`
	Trace          *TraceConfigTopology        `json:"trace,omitempty"`
	Plugins        []string                    `json:"plugins,omitempty"`
	Reasoning      *ReasoningConfigTopology    `json:"reasoning,omitempty"`
	CacheControl   *CacheControlConfigTopology `json:"cacheControl,omitempty"`
	SkipHistory    bool                        `json:"skipHistory,omitempty"`
}

// JSONSchemaConfigTopology mirrors cpn.JSONSchemaConfig.
type JSONSchemaConfigTopology struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ProviderConfigTopology mirrors cpn.ProviderConfig.
type ProviderConfigTopology struct {
	Order             []string                  `json:"order,omitempty"`
	Only              []string                  `json:"only,omitempty"`
	Ignore            []string                  `json:"ignore,omitempty"`
	AllowFallbacks    *bool                     `json:"allowFallbacks,omitempty"`
	Sort              string                    `json:"sort,omitempty"`
	MaxPrice          *ProviderMaxPriceTopology `json:"maxPrice,omitempty"`
	DataCollection    string                    `json:"dataCollection,omitempty"`
	ZDR               bool                      `json:"zdr,omitempty"`
	RequireParameters bool                      `json:"requireParameters,omitempty"`
}

// ProviderMaxPriceTopology mirrors cpn.ProviderMaxPrice.
type ProviderMaxPriceTopology struct {
	Prompt     string `json:"prompt,omitempty"`
	Completion string `json:"completion,omitempty"`
	Image      string `json:"image,omitempty"`
	Audio      string `json:"audio,omitempty"`
	Request    string `json:"request,omitempty"`
}

// TraceConfigTopology mirrors cpn.TraceConfig.
type TraceConfigTopology struct {
	TraceID        string `json:"traceId,omitempty"`
	TraceName      string `json:"traceName,omitempty"`
	SpanName       string `json:"spanName,omitempty"`
	GenerationName string `json:"generationName,omitempty"`
	ParentSpanID   string `json:"parentSpanId,omitempty"`
}

// ReasoningConfigTopology mirrors cpn.ReasoningConfig.
type ReasoningConfigTopology struct {
	Effort       string `json:"effort,omitempty"`
	Summary      string `json:"summary,omitempty"`
	MaxTokens    int    `json:"maxTokens,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
	Exclude      *bool  `json:"exclude,omitempty"`
	BudgetTokens int    `json:"budgetTokens,omitempty"`
}

// CacheControlConfigTopology mirrors cpn.CacheControlConfig.
type CacheControlConfigTopology struct {
	TTL string `json:"ttl,omitempty"`
}

// ValidateConfigTopology holds func registry names and serializable config.
type ValidateConfigTopology struct {
	SchemaFunc      string `json:"schemaFunc,omitempty"`
	ValidateFunc    string `json:"validateFunc,omitempty"`
	OnSuccessFunc   string `json:"onSuccessFunc,omitempty"`
	MaxCorrections  int    `json:"maxCorrections,omitempty"`
	CorrectionLLMID string `json:"correctionLLMID,omitempty"`
}

// HITLConfigTopology holds serializable HITL fields (excludes Channel).
type HITLConfigTopology struct {
	Prompt          string `json:"prompt,omitempty"`
	RevisionLoop    bool   `json:"revisionLoop,omitempty"`
	CorrectionLLMID string `json:"correctionLLMID,omitempty"`
	MaxRevisions    int    `json:"maxRevisions,omitempty"`
}

// RetryPolicyTopology holds retry configuration with optional circuit breaker.
type RetryPolicyTopology struct {
	MaxAttempts    int                           `json:"maxAttempts"`
	InitialWaitMs  int64                         `json:"initialWaitMs"`
	MaxWaitMs      int64                         `json:"maxWaitMs"`
	Multiplier     float64                       `json:"multiplier"`
	RetryOnFunc    string                        `json:"retryOnFunc,omitempty"`
	CircuitBreaker *CircuitBreakerConfigTopology `json:"circuitBreaker,omitempty"`
}

// CircuitBreakerConfigTopology mirrors cpn.CircuitBreakerConfig.
type CircuitBreakerConfigTopology struct {
	FailureThreshold int   `json:"failureThreshold"`
	OpenDurationMs   int64 `json:"openDurationMs"`
}

// MarshalCPN converts a live CPN to a serializable CPNTopology.
// Returns error if any non-nil func is not found in the registry.
func MarshalCPN(c *cpn.CPN, registry *FuncRegistry) (*CPNTopology, error) {
	topo := &CPNTopology{
		ID:          c.ID,
		Role:        c.Role,
		Depth:       c.Depth,
		Mode:        string(c.Mode),
		Places:      make(map[string]PlaceTopology, len(c.Places)),
		Transitions: make(map[string]TransitionTopology, len(c.Transitions)),
	}

	for id, p := range c.Places {
		topo.Places[id] = PlaceTopology{
			ID:    p.ID,
			Color: string(p.Color),
			Space: string(p.Space),
		}
	}

	for id, t := range c.Transitions {
		tt, err := marshalTransition(t, registry)
		if err != nil {
			return nil, fmt.Errorf("transition %q: %w", id, err)
		}
		topo.Transitions[id] = tt
	}

	return topo, nil
}

func marshalTransition(t *cpn.Transition, reg *FuncRegistry) (TransitionTopology, error) {
	tt := TransitionTopology{
		ID:            t.ID,
		Kind:          string(t.Kind),
		InputPlaces:   t.InputPlaces,
		OutputPlaces:  t.OutputPlaces,
		ErrorPlace:    t.ErrorPlace,
		SystemPrompt:  t.SystemPrompt,
		ToolName:      t.ToolName,
		LLMTools:      t.LLMTools,
		ObservedCPNID: t.ObservedCPNID,
	}

	// Guard func
	if t.Guard != nil {
		name, ok := reg.ReverseLookupGuard(t.Guard)
		if !ok {
			return tt, fmt.Errorf("guard func not registered")
		}
		tt.GuardFunc = name
	}

	// Executor func
	if t.Executor != nil {
		name, ok := reg.ReverseLookupExecutor(t.Executor)
		if !ok {
			return tt, fmt.Errorf("executor func not registered")
		}
		tt.ExecutorFunc = name
	}

	// SubNetFactory / SubNet
	if t.SubNetFactory != nil {
		name, ok := reg.ReverseLookupSubNetFactory(t.SubNetFactory)
		if !ok {
			return tt, fmt.Errorf("subnet factory func not registered")
		}
		tt.FactoryFunc = name
	} else if t.SubNet != nil {
		sub, err := MarshalCPN(t.SubNet, reg)
		if err != nil {
			return tt, fmt.Errorf("subnet: %w", err)
		}
		tt.SubNetTopology = sub
	}

	// EventFilter func
	if t.EventFilter != nil {
		name, ok := reg.ReverseLookupEventFilter(t.EventFilter)
		if !ok {
			return tt, fmt.Errorf("event filter func not registered")
		}
		tt.EventFilterFunc = name
	}

	// LLMConfig
	if t.LLMConfig != nil {
		tt.LLMConfig = marshalLLMConfig(t.LLMConfig)
	}

	// ValidateConfig
	if t.ValidateConfig != nil {
		vc, err := marshalValidateConfig(t.ValidateConfig, reg)
		if err != nil {
			return tt, err
		}
		tt.ValidateConfig = vc
	}

	// HITLConfig
	if t.HITLConfig != nil {
		tt.HITLConfig = marshalHITLConfig(t.HITLConfig)
	}

	// RetryPolicy
	if t.Retry != nil {
		rp, err := marshalRetryPolicy(t.Retry, reg)
		if err != nil {
			return tt, err
		}
		tt.Retry = rp
	}

	return tt, nil
}

func marshalLLMConfig(c *cpn.LLMConfig) *LLMConfigTopology {
	lc := &LLMConfigTopology{
		Model:          c.Model,
		FallbackModels: c.FallbackModels,
		Endpoint:       string(c.Endpoint),
		MaxTokens:      c.MaxTokens,
		Temperature:    c.Temperature,
		StreamOutput:   c.StreamOutput,
		RequireJSON:    c.RequireJSON,
		Budget:         c.Budget,
		SkipHistory:    c.SkipHistory,
	}

	if c.JSONSchema != nil {
		lc.JSONSchema = &JSONSchemaConfigTopology{
			Name:        c.JSONSchema.Name,
			Description: c.JSONSchema.Description,
			Schema:      c.JSONSchema.Schema,
			Strict:      c.JSONSchema.Strict,
		}
	}

	if c.Provider != nil {
		pc := &ProviderConfigTopology{
			Order:             c.Provider.Order,
			Only:              c.Provider.Only,
			Ignore:            c.Provider.Ignore,
			AllowFallbacks:    c.Provider.AllowFallbacks,
			Sort:              c.Provider.Sort,
			DataCollection:    c.Provider.DataCollection,
			ZDR:               c.Provider.ZDR,
			RequireParameters: c.Provider.RequireParameters,
		}
		if c.Provider.MaxPrice != nil {
			pc.MaxPrice = &ProviderMaxPriceTopology{
				Prompt:     c.Provider.MaxPrice.Prompt,
				Completion: c.Provider.MaxPrice.Completion,
				Image:      c.Provider.MaxPrice.Image,
				Audio:      c.Provider.MaxPrice.Audio,
				Request:    c.Provider.MaxPrice.Request,
			}
		}
		lc.Provider = pc
	}

	if c.Trace != nil {
		lc.Trace = &TraceConfigTopology{
			TraceID:        c.Trace.TraceID,
			TraceName:      c.Trace.TraceName,
			SpanName:       c.Trace.SpanName,
			GenerationName: c.Trace.GenerationName,
			ParentSpanID:   c.Trace.ParentSpanID,
		}
	}

	if len(c.Plugins) > 0 {
		lc.Plugins = make([]string, len(c.Plugins))
		copy(lc.Plugins, c.Plugins)
	}

	if c.Reasoning != nil {
		lc.Reasoning = &ReasoningConfigTopology{
			Effort:       c.Reasoning.Effort,
			Summary:      c.Reasoning.Summary,
			MaxTokens:    c.Reasoning.MaxTokens,
			Enabled:      c.Reasoning.Enabled,
			Exclude:      c.Reasoning.Exclude,
			BudgetTokens: c.Reasoning.BudgetTokens,
		}
	}

	if c.CacheControl != nil {
		lc.CacheControl = &CacheControlConfigTopology{
			TTL: c.CacheControl.TTL,
		}
	}

	return lc
}

func marshalValidateConfig(vc *cpn.ValidateConfig, reg *FuncRegistry) (*ValidateConfigTopology, error) {
	vt := &ValidateConfigTopology{
		MaxCorrections:  vc.MaxCorrections,
		CorrectionLLMID: vc.CorrectionLLMID,
	}

	if vc.ValidateFunc != nil {
		name, ok := reg.ReverseLookupValidateFunc(vc.ValidateFunc)
		if !ok {
			return nil, fmt.Errorf("validateFunc not registered")
		}
		vt.ValidateFunc = name
	}

	if vc.OnSuccess != nil {
		name, ok := reg.ReverseLookupOnSuccess(vc.OnSuccess)
		if !ok {
			return nil, fmt.Errorf("onSuccess func not registered")
		}
		vt.OnSuccessFunc = name
	}

	return vt, nil
}

func marshalHITLConfig(hc *cpn.HITLConfig) *HITLConfigTopology {
	return &HITLConfigTopology{
		Prompt:          hc.Prompt,
		RevisionLoop:    hc.RevisionLoop,
		CorrectionLLMID: hc.CorrectionLLMID,
		MaxRevisions:    hc.MaxRevisions,
	}
}

func marshalRetryPolicy(rp *cpn.RetryPolicy, reg *FuncRegistry) (*RetryPolicyTopology, error) {
	rt := &RetryPolicyTopology{
		MaxAttempts:   rp.MaxAttempts,
		InitialWaitMs: rp.InitialWait.Milliseconds(),
		MaxWaitMs:     rp.MaxWait.Milliseconds(),
		Multiplier:    rp.Multiplier,
	}

	if rp.RetryOn != nil {
		name, ok := reg.ReverseLookupRetryOn(rp.RetryOn)
		if !ok {
			return nil, fmt.Errorf("retryOn func not registered")
		}
		rt.RetryOnFunc = name
	}

	if rp.CircuitBreaker != nil {
		rt.CircuitBreaker = &CircuitBreakerConfigTopology{
			FailureThreshold: rp.CircuitBreaker.FailureThreshold,
			OpenDurationMs:   rp.CircuitBreaker.OpenDuration.Milliseconds(),
		}
	}

	return rt, nil
}

// UnmarshalCPN converts a CPNTopology back to a live CPN.
// Returns error if func names are non-empty but not found in the registry.
func UnmarshalCPN(t *CPNTopology, registry *FuncRegistry) (*cpn.CPN, error) {
	c := &cpn.CPN{
		ID:          t.ID,
		Role:        t.Role,
		Depth:       t.Depth,
		Mode:        cpn.Mode(t.Mode),
		State:       cpn.StateIdle,
		Places:      make(map[string]*cpn.Place, len(t.Places)),
		Transitions: make(map[string]*cpn.Transition, len(t.Transitions)),
	}

	for id, pt := range t.Places {
		c.Places[id] = &cpn.Place{
			ID:    pt.ID,
			Color: cpn.ColorSet(pt.Color),
			Space: cpn.SpaceKind(pt.Space),
		}
	}

	for id := range t.Transitions {
		tt := t.Transitions[id]
		tr, err := unmarshalTransition(&tt, registry)
		if err != nil {
			return nil, fmt.Errorf("transition %q: %w", id, err)
		}
		c.Transitions[id] = tr
	}

	return c, nil
}

func unmarshalTransition(tt *TransitionTopology, reg *FuncRegistry) (*cpn.Transition, error) {
	t := &cpn.Transition{
		ID:            tt.ID,
		Kind:          cpn.NodeKind(tt.Kind),
		InputPlaces:   tt.InputPlaces,
		OutputPlaces:  tt.OutputPlaces,
		ErrorPlace:    tt.ErrorPlace,
		SystemPrompt:  tt.SystemPrompt,
		ToolName:      tt.ToolName,
		LLMTools:      tt.LLMTools,
		ObservedCPNID: tt.ObservedCPNID,
	}

	// Guard func
	if tt.GuardFunc != "" {
		fn, ok := reg.LookupGuard(tt.GuardFunc)
		if !ok {
			return nil, fmt.Errorf("guard %q not found in registry", tt.GuardFunc)
		}
		t.Guard = fn
	}

	// Executor func
	if tt.ExecutorFunc != "" {
		fn, ok := reg.LookupExecutor(tt.ExecutorFunc)
		if !ok {
			return nil, fmt.Errorf("executor %q not found in registry", tt.ExecutorFunc)
		}
		t.Executor = fn
	}

	// SubNetFactory / SubNet
	if tt.FactoryFunc != "" {
		fn, ok := reg.LookupSubNetFactory(tt.FactoryFunc)
		if !ok {
			return nil, fmt.Errorf("subnet factory %q not found in registry", tt.FactoryFunc)
		}
		t.SubNetFactory = fn
	} else if tt.SubNetTopology != nil {
		sub, err := UnmarshalCPN(tt.SubNetTopology, reg)
		if err != nil {
			return nil, fmt.Errorf("subnet: %w", err)
		}
		t.SubNet = sub
	}

	// EventFilter func
	if tt.EventFilterFunc != "" {
		fn, ok := reg.LookupEventFilter(tt.EventFilterFunc)
		if !ok {
			return nil, fmt.Errorf("event filter %q not found in registry", tt.EventFilterFunc)
		}
		t.EventFilter = fn
	}

	// LLMConfig
	if tt.LLMConfig != nil {
		t.LLMConfig = unmarshalLLMConfig(tt.LLMConfig)
	}

	// ValidateConfig
	if tt.ValidateConfig != nil {
		vc, err := unmarshalValidateConfig(tt.ValidateConfig, reg)
		if err != nil {
			return nil, err
		}
		t.ValidateConfig = vc
	}

	// HITLConfig — Channel is set to nil (runtime-only)
	if tt.HITLConfig != nil {
		t.HITLConfig = &cpn.HITLConfig{
			Prompt:          tt.HITLConfig.Prompt,
			RevisionLoop:    tt.HITLConfig.RevisionLoop,
			CorrectionLLMID: tt.HITLConfig.CorrectionLLMID,
			MaxRevisions:    tt.HITLConfig.MaxRevisions,
		}
	}

	// RetryPolicy
	if tt.Retry != nil {
		rp, err := unmarshalRetryPolicy(tt.Retry, reg)
		if err != nil {
			return nil, err
		}
		t.Retry = rp
	}

	return t, nil
}

func unmarshalLLMConfig(lc *LLMConfigTopology) *cpn.LLMConfig {
	c := &cpn.LLMConfig{
		Model:          lc.Model,
		FallbackModels: lc.FallbackModels,
		Endpoint:       cpn.LLMEndpoint(lc.Endpoint),
		MaxTokens:      lc.MaxTokens,
		Temperature:    lc.Temperature,
		StreamOutput:   lc.StreamOutput,
		RequireJSON:    lc.RequireJSON,
		Budget:         lc.Budget,
		SkipHistory:    lc.SkipHistory,
	}

	if lc.JSONSchema != nil {
		c.JSONSchema = &cpn.JSONSchemaConfig{
			Name:        lc.JSONSchema.Name,
			Description: lc.JSONSchema.Description,
			Schema:      lc.JSONSchema.Schema,
			Strict:      lc.JSONSchema.Strict,
		}
	}

	if lc.Provider != nil {
		pc := &cpn.ProviderConfig{
			Order:             lc.Provider.Order,
			Only:              lc.Provider.Only,
			Ignore:            lc.Provider.Ignore,
			AllowFallbacks:    lc.Provider.AllowFallbacks,
			Sort:              lc.Provider.Sort,
			DataCollection:    lc.Provider.DataCollection,
			ZDR:               lc.Provider.ZDR,
			RequireParameters: lc.Provider.RequireParameters,
		}
		if lc.Provider.MaxPrice != nil {
			pc.MaxPrice = &cpn.ProviderMaxPrice{
				Prompt:     lc.Provider.MaxPrice.Prompt,
				Completion: lc.Provider.MaxPrice.Completion,
				Image:      lc.Provider.MaxPrice.Image,
				Audio:      lc.Provider.MaxPrice.Audio,
				Request:    lc.Provider.MaxPrice.Request,
			}
		}
		c.Provider = pc
	}

	if lc.Trace != nil {
		c.Trace = &cpn.TraceConfig{
			TraceID:        lc.Trace.TraceID,
			TraceName:      lc.Trace.TraceName,
			SpanName:       lc.Trace.SpanName,
			GenerationName: lc.Trace.GenerationName,
			ParentSpanID:   lc.Trace.ParentSpanID,
		}
	}

	if len(lc.Plugins) > 0 {
		c.Plugins = make([]cpn.PluginConfig, len(lc.Plugins))
		copy(c.Plugins, lc.Plugins)
	}

	if lc.Reasoning != nil {
		c.Reasoning = &cpn.ReasoningConfig{
			Effort:       lc.Reasoning.Effort,
			Summary:      lc.Reasoning.Summary,
			MaxTokens:    lc.Reasoning.MaxTokens,
			Enabled:      lc.Reasoning.Enabled,
			Exclude:      lc.Reasoning.Exclude,
			BudgetTokens: lc.Reasoning.BudgetTokens,
		}
	}

	if lc.CacheControl != nil {
		c.CacheControl = &cpn.CacheControlConfig{
			TTL: lc.CacheControl.TTL,
		}
	}

	return c
}

func unmarshalValidateConfig(vt *ValidateConfigTopology, reg *FuncRegistry) (*cpn.ValidateConfig, error) {
	vc := &cpn.ValidateConfig{
		MaxCorrections:  vt.MaxCorrections,
		CorrectionLLMID: vt.CorrectionLLMID,
	}

	if vt.SchemaFunc != "" {
		fn, ok := reg.LookupSchema(vt.SchemaFunc)
		if !ok {
			return nil, fmt.Errorf("schema %q not found in registry", vt.SchemaFunc)
		}
		vc.Schema = fn()
	}

	if vt.ValidateFunc != "" {
		fn, ok := reg.LookupValidateFunc(vt.ValidateFunc)
		if !ok {
			return nil, fmt.Errorf("validateFunc %q not found in registry", vt.ValidateFunc)
		}
		vc.ValidateFunc = fn
	}

	if vt.OnSuccessFunc != "" {
		fn, ok := reg.LookupOnSuccess(vt.OnSuccessFunc)
		if !ok {
			return nil, fmt.Errorf("onSuccess %q not found in registry", vt.OnSuccessFunc)
		}
		vc.OnSuccess = fn
	}

	return vc, nil
}

func unmarshalRetryPolicy(rt *RetryPolicyTopology, reg *FuncRegistry) (*cpn.RetryPolicy, error) {
	rp := &cpn.RetryPolicy{
		MaxAttempts: rt.MaxAttempts,
		InitialWait: time.Duration(rt.InitialWaitMs) * time.Millisecond,
		MaxWait:     time.Duration(rt.MaxWaitMs) * time.Millisecond,
		Multiplier:  rt.Multiplier,
	}

	if rt.RetryOnFunc != "" {
		fn, ok := reg.LookupRetryOn(rt.RetryOnFunc)
		if !ok {
			return nil, fmt.Errorf("retryOn %q not found in registry", rt.RetryOnFunc)
		}
		rp.RetryOn = fn
	}

	if rt.CircuitBreaker != nil {
		rp.CircuitBreaker = &cpn.CircuitBreakerConfig{
			FailureThreshold: rt.CircuitBreaker.FailureThreshold,
			OpenDuration:     time.Duration(rt.CircuitBreaker.OpenDurationMs) * time.Millisecond,
		}
	}

	return rp, nil
}

// TopologyHash computes a deterministic SHA-256 hex hash of the topology structure.
// Ignores runtime state (Mode, State). Stable across process restarts.
func TopologyHash(t *CPNTopology) string {
	h := sha256.New()

	// Sort places by ID for determinism
	placeIDs := make([]string, 0, len(t.Places))
	for id := range t.Places {
		placeIDs = append(placeIDs, id)
	}
	sort.Strings(placeIDs)

	for _, id := range placeIDs {
		p := t.Places[id]
		fmt.Fprintf(h, "P:%s:%s:%s;", p.ID, p.Color, p.Space)
	}

	// Sort transitions by ID for determinism
	transIDs := make([]string, 0, len(t.Transitions))
	for id := range t.Transitions {
		transIDs = append(transIDs, id)
	}
	sort.Strings(transIDs)

	for _, id := range transIDs {
		tr := t.Transitions[id]
		fmt.Fprintf(h, "T:%s:%s:", tr.ID, tr.Kind)
		for _, ip := range tr.InputPlaces {
			fmt.Fprintf(h, "I:%s;", ip)
		}
		for _, op := range tr.OutputPlaces {
			fmt.Fprintf(h, "O:%s;", op)
		}
		fmt.Fprintf(h, "E:%s;", tr.ErrorPlace)
		fmt.Fprintf(h, "G:%s;X:%s;F:%s;EF:%s;", tr.GuardFunc, tr.ExecutorFunc, tr.FactoryFunc, tr.EventFilterFunc)
		fmt.Fprintf(h, "SP:%s;TN:%s;OB:%s;", tr.SystemPrompt, tr.ToolName, tr.ObservedCPNID)

		for _, tool := range tr.LLMTools {
			fmt.Fprintf(h, "LT:%s;", tool)
		}

		if tr.LLMConfig != nil {
			b, _ := json.Marshal(tr.LLMConfig)
			fmt.Fprintf(h, "LLC:%s;", b)
		}
		if tr.ValidateConfig != nil {
			b, _ := json.Marshal(tr.ValidateConfig)
			fmt.Fprintf(h, "VC:%s;", b)
		}
		if tr.HITLConfig != nil {
			b, _ := json.Marshal(tr.HITLConfig)
			fmt.Fprintf(h, "HC:%s;", b)
		}
		if tr.Retry != nil {
			b, _ := json.Marshal(tr.Retry)
			fmt.Fprintf(h, "R:%s;", b)
		}
		if tr.SubNetTopology != nil {
			fmt.Fprintf(h, "SN:%s;", TopologyHash(tr.SubNetTopology))
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

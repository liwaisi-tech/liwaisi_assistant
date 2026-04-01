package main

import (
	"fmt"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// defaultTopologyFactory creates a minimal CPN topology for a session:
// p-input → t-llm → p-output.
func defaultTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-output": cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}
	tLLM := cpn.NewTransition("t-llm", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-output"})
	tLLM.SystemPrompt = envOr("PROMPT_DIRECT", "You are a helpful assistant. Be concise.")
	tLLM.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-llm": tLLM,
	}
	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// hitlTopologyFactory creates a CPN topology with a human-review gate:
// p-input → t-plan (LLM) → p-plan → t-review (HITL) → p-reviewed → t-execute (LLM) → p-output.
//
// The assistant first presents a plan, waits for user approval, then executes.
// Channel on t-review is left nil — wired by SessionService.CreateSession.
func hitlTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":    cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-plan":     cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed": cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":   cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	tPlan := cpn.NewTransition("t-plan", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-plan"})
	tPlan.SystemPrompt = envOr("PROMPT_PLAN", "You are a helpful assistant. Analyze the user's request and present a clear, concise plan. "+
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\"")
	tPlan.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_PLAN", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}

	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please review the plan above.",
	}

	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = envOr("PROMPT_EXECUTE", "You are a helpful assistant. The user approved the following plan. "+
		"Execute it thoroughly and provide the final result.")
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_EXECUTE", 8192),
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-plan":    tPlan,
		"t-review":  tReview,
		"t-execute": tExecute,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

// unifiedTopologyFactory creates a CPN topology with an intent classifier router:
//
//	p-input → t-classify (LLM, classifier model, JSON)
//	  → p-classified
//	    ├→ t-direct (guard: conversation) → p-output
//	    └→ t-plan (guard: task) → p-plan → t-review (HITL) → p-reviewed → t-execute → p-output
//
// The classifier routes greetings/questions to a direct response and complex tasks
// to the plan-review-execute HITL flow. Follows the Router Pattern (Arize, BSWEN 2026).
func unifiedTopologyFactory(sessionID string) *cpn.CPN {
	places := map[string]*cpn.Place{
		"p-input":      cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
		"p-classified": cpn.NewPlace("p-classified", cpn.ColorJSON, cpn.SpaceSurface),
		"p-plan":       cpn.NewPlace("p-plan", cpn.ColorArtifact, cpn.SpaceSurface),
		"p-reviewed":   cpn.NewPlace("p-reviewed", cpn.ColorHuman, cpn.SpaceComputation),
		"p-output":     cpn.NewPlace("p-output", cpn.ColorArtifact, cpn.SpaceSurface),
	}

	// t-classify: fast intent classifier using lightweight model.
	tClassify := cpn.NewTransition("t-classify", cpn.NodeKindLLM,
		[]string{"p-input"}, []string{"p-classified"})
	tClassify.SystemPrompt = envOr("PROMPT_CLASSIFIER", `You are an intent classifier. Classify the user's message into exactly one category.
Respond with JSON only, no explanation.

Categories:
- "conversation": greetings, casual chat, simple factual questions, clarifications, thank you messages
- "task": requests requiring planning, multi-step work, code generation, document creation, analysis, implementation

Examples:
User: "Hola, ¿cómo estás?" → {"intent":"conversation"}
User: "What is a Petri net?" → {"intent":"conversation"}
User: "Create a plan to implement a CNN in R" → {"intent":"task"}
User: "Refactor the authentication module" → {"intent":"task"}
User: "Thanks!" → {"intent":"conversation"}

Respond ONLY with the JSON object.`)
	tClassify.LLMConfig = &cpn.LLMConfig{
		Model:        "classifier",
		MaxTokens:    64,
		Temperature:  0.0,
		RequireJSON:  true,
		StreamOutput: false,
		SkipHistory:  true,
	}

	// t-direct: fires for conversation intent — direct streaming response.
	tDirect := cpn.NewTransition("t-direct", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-output"})
	tDirect.SystemPrompt = envOr("PROMPT_DIRECT", "You are a helpful, friendly assistant. Respond naturally and concisely.")
	tDirect.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_DIRECT", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}
	tDirect.Guard = func(tokens []*cpn.Token) bool {
		for _, tok := range tokens {
			if s, ok := tok.Payload.(string); ok {
				return !strings.Contains(strings.ToLower(s), `"task"`)
			}
		}
		return true
	}

	// t-plan: fires ONLY for explicit task intent — presents a plan for review.
	tPlan := cpn.NewTransition("t-plan", cpn.NodeKindLLM,
		[]string{"p-classified"}, []string{"p-plan"})
	tPlan.SystemPrompt = envOr("PROMPT_PLAN", "You are a helpful assistant. Analyze the user's request and present a clear, concise plan. "+
		"Format the plan as a numbered list of steps. End with: \"Would you like me to proceed?\"")
	tPlan.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_PLAN", 4096),
		Temperature:  0.7,
		StreamOutput: true,
	}
	tPlan.Guard = func(tokens []*cpn.Token) bool {
		for _, tok := range tokens {
			if s, ok := tok.Payload.(string); ok {
				return strings.Contains(strings.ToLower(s), `"task"`)
			}
		}
		return false
	}

	// t-review: HITL gate — waits for user approval.
	tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
		[]string{"p-plan"}, []string{"p-reviewed"})
	tReview.HITLConfig = &cpn.HITLConfig{
		Prompt: "Please review the plan above.",
	}

	// t-execute: fires after approval — executes the plan.
	tExecute := cpn.NewTransition("t-execute", cpn.NodeKindLLM,
		[]string{"p-reviewed"}, []string{"p-output"})
	tExecute.SystemPrompt = envOr("PROMPT_EXECUTE", "You are a helpful assistant. The user approved the following plan. "+
		"Execute it thoroughly and provide the final result.")
	tExecute.LLMConfig = &cpn.LLMConfig{
		MaxTokens:    envInt("MAX_TOKENS_EXECUTE", 8192),
		Temperature:  0.7,
		StreamOutput: true,
	}

	transitions := map[string]*cpn.Transition{
		"t-classify": tClassify,
		"t-direct":   tDirect,
		"t-plan":     tPlan,
		"t-review":   tReview,
		"t-execute":  tExecute,
	}

	c := cpn.NewCPN(
		fmt.Sprintf("cpn-%s", sessionID),
		"assistant",
		0,
		cpn.ModeMAS,
		sessionID,
		places,
		transitions,
	)
	c.ContextWindowSize = 10
	return c
}

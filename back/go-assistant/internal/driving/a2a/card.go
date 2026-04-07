package a2a

// PersonalityProvider supplies agent personality metadata for the AgentCard.
type PersonalityProvider interface {
	// Name returns the agent's display name.
	Name() string

	// Description returns the agent's description.
	Description() string
}

// ToolInfo describes a registered tool for AgentCard skill generation.
type ToolInfo struct {
	Name        string
	Namespace   string
	Description string
}

// ToolProvider supplies tool metadata for the AgentCard.
type ToolProvider interface {
	// Tools returns the list of registered tools.
	Tools() []ToolInfo
}

// BuildAgentCard constructs an AgentCard from the configuration, personality,
// tool registry, and version string. The card is served at
// GET /.well-known/agent-card.json per REQ-001.
//
// Skills are derived from the known CPN topologies (unified, simple, hitl).
// The security scheme matches the existing Google OAuth Bearer token
// authentication per REQ-023.
func BuildAgentCard(cfg Config, personality PersonalityProvider, tools ToolProvider, version string) *AgentCard {
	name := "BRAE"
	description := "AI assistant powered by Coloured Petri Net execution engine"
	if personality != nil {
		if n := personality.Name(); n != "" {
			name = n
		}
		if d := personality.Description(); d != "" {
			description = d
		}
	}

	// Static skills matching the three known topologies.
	skills := []AgentSkill{
		{
			ID:          "unified",
			Name:        "Intelligent Conversation & Task Execution",
			Description: "Automatically classifies input as conversation or task. Conversations get direct responses; tasks get planned, reviewed by human, then executed.",
			Tags:        []string{"conversation", "task", "planning", "hitl"},
			Examples: []string{
				"Hello, how are you?",
				"Create a plan to build a REST API",
				"Teach me about design patterns",
			},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "simple",
			Name:        "Direct LLM Conversation",
			Description: "Single LLM call with streaming response. No classification or planning.",
			Tags:        []string{"conversation", "simple"},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain"},
		},
		{
			ID:          "hitl",
			Name:        "Plan-Review-Execute with Human Gate",
			Description: "Plans a response, pauses for human approval, then executes. Always requires HITL.",
			Tags:        []string{"planning", "hitl", "review"},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain"},
		},
	}

	// Append tool-based skills if a tool provider is available.
	if tools != nil {
		for _, t := range tools.Tools() {
			skills = append(skills, AgentSkill{
				ID:          t.Namespace + "/" + t.Name,
				Name:        t.Name,
				Description: t.Description,
				Tags:        []string{"tool", t.Namespace},
				InputModes:  []string{"text/plain"},
				OutputModes: []string{"text/plain", "application/json"},
			})
		}
	}

	// Security scheme matching existing Google OAuth Bearer per REQ-023.
	securitySchemes := map[string]any{
		"google_oauth": map[string]any{
			"type": "oauth2",
			"flows": map[string]any{
				"authorizationCode": map[string]any{
					"authorizationUrl": "https://accounts.google.com/o/oauth2/auth",
					"tokenUrl":         "https://oauth2.googleapis.com/token",
					"scopes": map[string]string{
						"openid":  "OpenID Connect",
						"email":   "Email address",
						"profile": "User profile",
					},
				},
			},
		},
	}

	return &AgentCard{
		Name:        name,
		Description: description,
		Version:     version,
		Provider: AgentProvider{
			Organization: "liwaisi-tech",
			URL:          "https://liwaisi.com",
		},
		SupportedInterfaces: []AgentInterface{
			{
				URL:             cfg.BaseURL + "/a2a",
				ProtocolBinding: "JSONRPC",
				ProtocolVersion: "1.0",
			},
		},
		Capabilities: AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
			ExtendedAgentCard: false,
		},
		SecuritySchemes:      securitySchemes,
		SecurityRequirements: [][]string{{"google_oauth"}},
		DefaultInputModes:    []string{"text/plain"},
		DefaultOutputModes:   []string{"text/plain", "application/json"},
		Skills:               skills,
	}
}

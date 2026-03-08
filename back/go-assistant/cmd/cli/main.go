// Package main is the entry point for the liwaisi CLI application.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/planner"
	appservice "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/service"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/session"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/skill"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/skill/builtin"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	envtool "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/env"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/filemanagement"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/shellexec"
	subagenttools "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/web"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driven/filesystem"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driven/openrouter"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driven/persistence/sqlite"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/commands"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/home"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/telemetry"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

const securityPromptSuffix = "NEVER attempt to read, print, echo, or display the contents of environment variables " +
	"that contain secrets or API keys. " +
	"When using credentials in commands, ALWAYS use variable references " +
	"(e.g., $CLICKUP_API_KEY) instead of literal values. " +
	"NEVER include secret values in your responses, code, or file contents. " +
	"If a command output appears to contain a redacted value like [REDACTED:KEY_NAME], " +
	"do not attempt to recover or circumvent the redaction."

const eagerSystemPrompt = "You are liwaisi, a helpful AI assistant in the terminal. " +
	"Be concise, use markdown formatting when helpful. " +
	"You have access to file management tools that operate within your workspace directory. " +
	"All file paths are relative to the workspace root. " +
	"Use tree to understand project structure (set depth wisely — lower for large/unknown dirs). " +
	"Use list_directory for flat listing, read_file to inspect a single file, " +
	"read_files to inspect multiple files or directories at once, " +
	"write_file to create or update files, create_directory to create new directories, " +
	"delete to remove files or directories (supports batch deletion), " +
	"find to search for files by name pattern (like find command), " +
	"and grep to search for content inside files (like grep command). " +
	"Use move to relocate files/directories and rename to change names in place. " +
	"Use execute_command to run OS commands within the workspace " +
	"(builds, scripts, API calls, tests, git operations, inspecting the environment). " +
	"Commands run via sh -c with full shell features: pipes (|), redirects (>), " +
	"env vars (KEY=val cmd), and chaining (&&). " +
	"Set timeout_seconds appropriately for long-running commands (e.g., 120 for test suites, 60 for builds). " +
	"Certain dangerous commands (sudo, rm -rf /, etc.) are blocked by security policy. " +
	"Prefer execute_command for operations like running tests, building projects, " +
	"checking git status, and querying APIs over suggesting manual steps to the user. " +
	"You also have access to skills — packaged workflows that guide you through multi-step tasks. " +
	"Skills are different from tools: tools perform actions, skills teach you how to accomplish goals. " +
	"When a user's request matches a skill's description, activate it with the activate_skill tool " +
	"and follow its instructions. " +
	"Use web_fetch to read web pages — it fetches a URL and returns clean Markdown content. " +
	"Great for reading documentation, READMEs, articles, and API references. " +
	"Use raw=true for index/listing pages where Readability might strip too much content. " +
	"Use check_env_variable to verify that required API keys or credentials are available " +
	"before attempting operations that need them. " +
	"If a variable is not set, advise the user to run 'liwaisi env set <KEY>' to configure it. " +
	"Use reload_env when the user says they have updated their credentials " +
	"and want them to take effect without restarting. " +
	securityPromptSuffix

const jitSystemPrompt = "You are liwaisi, a helpful AI assistant in the terminal. " +
	"Be concise, use markdown formatting when helpful. " +
	"You start with discovery tools. Use find_tools to load tool categories as needed:\n" +
	"- filemanagement: read, write, search, navigate files in the workspace\n" +
	"- shellexec: run OS commands (builds, tests, git, scripts)\n" +
	"- web: fetch web pages as markdown\n" +
	"- env: check and reload environment variables\n" +
	"Load only what you need — call find_tools with action='load' and the category name. " +
	"Once loaded, tools stay available for the rest of this conversation. " +
	"Use find_skills to discover and activate packaged workflows for multi-step tasks. " +
	"Skills teach you HOW to accomplish goals; tools perform discrete actions. " +
	"You also have spawn_subagent, list_subagents, evaluate_team, and create_subagent tools always available for delegating tasks to specialized subagents. " +
	"Use evaluate_team to determine if a task would benefit from multiple specialist agents. " +
	"Use create_subagent to define and persist new specialist agents as SUBAGENT.md files. " +
	securityPromptSuffix

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	h, err := home.New("")
	if err != nil {
		return fmt.Errorf("resolving liwaisi home: %w", err)
	}
	if err := h.Init(); err != nil {
		return fmt.Errorf("initializing liwaisi home: %w", err)
	}

	ctx := context.Background()
	logDir := filepath.Join(h.Root(), "logs")
	shutdown, logLevel, err := telemetry.InitCLI(ctx, telemetry.CLIConfig{
		LogDir:         logDir,
		ServiceVersion: version.Version,
	})
	if err != nil {
		return fmt.Errorf("initializing telemetry: %w", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = shutdown.Execute(shutCtx)
	}()

	slog.Info("liwaisi starting", "version", version.Version)

	dbPath := filepath.Join(h.Path(home.Data), "liwaisi.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sqlite.NewConnection(dbPath)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()

	healthRepo := sqlite.NewHealthRepository(db)
	healthSvc := appservice.NewHealthService(healthRepo, version.Version)

	envFilePath := filepath.Join(h.Path(home.Config), "env.yaml")
	envStore := env.NewStore(envFilePath, nil)
	loaded, err := envStore.Load(ctx)
	if err != nil {
		slog.Warn("loading env file", "error", err)
	}
	if loaded > 0 {
		slog.Info("environment loaded from file", "variables", loaded)
	}

	sharedMemory := memory.NewConversationMemory()
	cwd, _ := os.Getwd()
	sessionsDir := filepath.Join(h.Path(home.Data), "sessions")
	sessionStore := session.NewStore(sessionsDir, cwd)

	var cachedAgentSvc input.AgentService
	var cachedPlannerSvc input.PlannerService
	var cachedModelName string
	var cachedErr error
	var servicesInitialized bool

	servicesFactory := func() (input.AgentService, input.PlannerService, string, error) {
		if !servicesInitialized {
			cachedAgentSvc, cachedPlannerSvc, cachedModelName, cachedErr = buildServices(h, h.Path(home.Workspace), envStore, sharedMemory)
			servicesInitialized = true
		}
		return cachedAgentSvc, cachedPlannerSvc, cachedModelName, cachedErr
	}

	agentFactory := func() (input.AgentService, string, error) {
		agentSvc, _, modelName, err := servicesFactory()
		return agentSvc, modelName, err
	}

	plannerFactory := func() (input.PlannerService, error) {
		_, plannerSvc, _, err := servicesFactory()
		return plannerSvc, err
	}

	opts := &commands.Options{
		Home:           h,
		HealthSvc:      healthSvc,
		AgentFactory:   agentFactory,
		PlannerFactory: plannerFactory,
		Memory:         sharedMemory,
		SessionStore:   sessionStore,
		EnvStore:       envStore,
		LogLevel:       logLevel,
	}

	return commands.Execute(opts)
}

func buildServices(h *home.Home, workspacePath string, envStore *env.Store, mem *memory.ConversationMemory) (input.AgentService, input.PlannerService, string, error) { //nolint:funlen,cyclop // wiring function

	llmCfg, err := configs.LoadLLMConfig()
	if err != nil {
		return nil, nil, "", err
	}

	llmClient, err := openrouter.NewClient(&openrouter.ClientConfig{
		APIKey:  llmCfg.APIKey,
		BaseURL: llmCfg.BaseURL,
		Model:   llmCfg.Model,
		Timeout: llmCfg.Timeout,
	})
	if err != nil {
		return nil, nil, "", fmt.Errorf("creating LLM client: %w", err)
	}

	agentInfo := &tool.AgentInfo{
		Name:        "liwaisi",
		Version:     version.Version,
		Description: "AI agent assistant for the terminal, built with Go and powered by OpenRouter.",
		Model:       llmCfg.Model,
		Capabilities: []string{
			"Interactive chat with streaming responses",
			"Single-shot queries",
			"Tool calling for extended capabilities",
			"Conversation memory with session management",
			"Markdown rendering in terminal",
			"File management within workspace (read, write, delete, batch read)",
			"File and directory discovery (list, find, tree)",
			"Content search within files (grep)",
			"File and directory move and rename operations",
			"Skill system with progressive disclosure (activate, list, read resources)",
			"Built-in skill-creator for authoring new skills",
			"OS command execution within workspace (sandboxed, with timeout and security policy)",
			"Web content fetching and HTML-to-Markdown conversion",
			"Environment variable management (check availability, reload from file)",
			"SubAgent delegation via spawn_subagent for specialized tasks",
			"Dynamic team formation with evaluate_team and create_subagent",
			"Built-in subagent-creator skill for authoring new subagent definitions",
			"Built-in planner for decomposing and executing multi-step tasks",
		},
		CreatedBy: "Liwaisi Engineering",
	}

	fileMgmt, err := filemanagement.NewToolSet(workspacePath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("initializing file management tools: %w", err)
	}

	shellExec, err := shellexec.NewToolSet(workspacePath, shellexec.WithRedactor(envStore.Redactor()))
	if err != nil {
		return nil, nil, "", fmt.Errorf("initializing shell execution tools: %w", err)
	}

	webTools := web.NewToolSet()
	envTools := envtool.NewToolSet(envStore)

	skillRegistry := skill.NewRegistry()
	if err := skillRegistry.LoadEmbedded(builtin.SkillsFS, "."); err != nil {
		slog.Warn("loading built-in skills", "error", err)
	}
	skillsDir := filepath.Join(workspacePath, "skills")
	if err := skillRegistry.LoadFromDir(skillsDir); err != nil {
		slog.Warn("loading user skills", "error", err)
	}

	router := subagent.NewRouter()
	router.RegisterBuiltIn(&appservice.PlannerBuiltInSpec)

	clientFactory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return llmClient, llmCfg.Model
	}
	memFactory := memory.NewSubAgentMemoryFactory(mem)
	runner := subagent.NewRunner(clientFactory, memFactory)

	cache := subagent.NewCache(subagent.WithTTL(30 * time.Minute))
	defer cache.Close()

	subagentRegistry := subagent.NewRegistry()
	subagentsDir := filepath.Join(workspacePath, "subagents")
	if err := subagentRegistry.LoadFromDir(subagentsDir); err != nil {
		slog.Warn("loading persisted subagents", "error", err)
	}

	detector := subagent.NewHybridTeamDetector(clientFactory)

	svcOpts := []appservice.SubAgentServiceOption{
		appservice.WithSubAgentCache(cache),
		appservice.WithSubAgentRegistry(subagentRegistry),
		appservice.WithTeamDetector(detector),
		appservice.WithSubAgentsDir(subagentsDir),
	}

	agentCfg := appservice.AgentConfig{
		Model:             llmCfg.Model,
		Temperature:       llmCfg.Temperature,
		MaxTokens:         llmCfg.MaxTokens,
		MaxToolIterations: llmCfg.MaxToolIterations,
	}

	var baseRegistry tool.SessionExecutorProvider
	var agentSvc input.AgentService
	switch llmCfg.ToolLoadingStrategy {
	case configs.ToolLoadingEager:
		var reg *tool.Registry
		agentSvc, reg = buildEagerAgent(llmClient, agentCfg, agentInfo, fileMgmt, shellExec, webTools, envTools, skillRegistry, mem, nil)
		baseRegistry = reg
	default:
		var store *tool.SessionRegistryStore
		agentSvc, store = buildJITAgent(llmClient, agentCfg, agentInfo, fileMgmt, shellExec, webTools, envTools, skillRegistry, mem, nil)
		baseRegistry = store
	}

	subagentSvc := appservice.NewSubAgentService(router, runner, baseRegistry, svcOpts...)

	// Inject subagentSvc into the agent's registry/store.
	if reg, ok := baseRegistry.(*tool.Registry); ok {
		subagenttools.RegisterSubAgentTools(reg, subagentSvc, nil)
	} else if store, ok := baseRegistry.(*tool.SessionRegistryStore); ok {
		store.UpdateSubAgentSvc(subagentSvc)
	}

	decomp := planner.NewDecomposer(llmClient, llmCfg.Model)
	teamAssembler := planner.NewTeamAssembler(llmClient, llmCfg.Model)
	enhancedDecomp := planner.NewEnhancedDecomposer(llmClient, llmCfg.Model)
	evaluator := planner.NewPlanEvaluator(llmClient, llmCfg.Model)

	plansDir := filepath.Join(h.Path(home.Data), "plans")
	planStore, err := filesystem.NewFilePlanStore(plansDir)
	if err != nil {
		slog.Warn("plan persistence disabled", "error", err)
	}

	plannerSvc := appservice.NewImprovedPlannerService(appservice.PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     decomp,
		EnhancedDecomp: enhancedDecomp,
		TeamAssembler:  teamAssembler,
		Evaluator:      evaluator,
		SubAgentSvc:    subagentSvc,
		Store:          planStore,
	})

	return agentSvc, plannerSvc, agentCfg.Model, nil
}

func buildEagerAgent(
	llmClient *openrouter.Client,
	agentCfg appservice.AgentConfig,
	agentInfo *tool.AgentInfo,
	fileMgmt *filemanagement.ToolSet,
	shellExec *shellexec.ToolSet,
	webTools *web.ToolSet,
	envTools *envtool.ToolSet,
	skillRegistry *skill.Registry,
	mem *memory.ConversationMemory,
	_ input.SubAgentService, // legacy, kept for signature compatibility during refactor if needed, but not used now
) (svc input.AgentService, registry *tool.Registry) {
	slog.Info("tool loading strategy: eager")

	registry = tool.NewRegistry()
	tool.RegisterWhoAmI(registry, agentInfo)
	fileMgmt.Register(registry)
	shellExec.Register(registry)
	webTools.Register(registry)
	envTools.Register(registry)
	skill.RegisterSkillTools(registry, skillRegistry)

	// subagenttools.RegisterSubAgentTools is now called in buildServices after subagentSvc is created.

	agentCfg.SystemPrompt = eagerSystemPrompt + skillRegistry.SystemPromptFragment()

	svc = appservice.NewAgentService(llmClient, agentCfg, registry, mem)
	return svc, registry
}

func buildJITAgent(
	llmClient *openrouter.Client,
	agentCfg appservice.AgentConfig,
	agentInfo *tool.AgentInfo,
	fileMgmt *filemanagement.ToolSet,
	shellExec *shellexec.ToolSet,
	webTools *web.ToolSet,
	envTools *envtool.ToolSet,
	skillRegistry *skill.Registry,
	mem *memory.ConversationMemory,
	_ input.SubAgentService,
) (svc input.AgentService, store *tool.SessionRegistryStore) {
	slog.Info("tool loading strategy: jit")

	catalog := tool.NewCatalog()
	catalog.RegisterCategory(fileMgmt.CatalogEntry())
	catalog.RegisterCategory(shellExec.CatalogEntry())
	catalog.RegisterCategory(webTools.CatalogEntry())
	catalog.RegisterCategory(envTools.CatalogEntry())

	store = tool.NewSessionRegistryStore(catalog, func(ar *tool.ActiveRegistry, cat *tool.Catalog, subagentSvc input.SubAgentService) {
		tool.RegisterWhoAmI(ar, agentInfo)
		tool.RegisterFindToolsOnActive(ar, cat)
		skill.RegisterFindSkills(ar, skillRegistry)
		subagenttools.RegisterSubAgentTools(ar, subagentSvc, nil)
	})

	agentCfg.SystemPrompt = jitSystemPrompt + skillRegistry.SystemPromptFragment()

	svc = appservice.NewAgentService(llmClient, agentCfg, nil, mem,
		appservice.WithSessionStore(store),
	)
	return svc, store
}

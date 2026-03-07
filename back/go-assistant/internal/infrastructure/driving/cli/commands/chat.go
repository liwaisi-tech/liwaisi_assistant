package commands

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/session"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/home"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

func newChatCmd(h *home.Home, agentSvc input.AgentService, mem *memory.ConversationMemory, ss *session.Store, modelName string) *cobra.Command { //nolint:funlen // wiring function
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive chat session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			listSessions, _ := cmd.Flags().GetBool("list-sessions")
			if listSessions {
				return handleListSessions(ss)
			}

			deleteSession, _ := cmd.Flags().GetString("delete-session")
			if deleteSession != "" {
				return handleDeleteSession(ss, deleteSession)
			}

			cfg, err := configs.LoadCLIConfig(configPath(h))
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			themeName, _ := cmd.Flags().GetString("theme")
			if !cmd.Flags().Changed("theme") {
				themeName = cfg.Theme
			}
			th, err := theme.Get(themeName)
			if err != nil {
				th, err = theme.Get("dark")
				if err != nil {
					return fmt.Errorf("no theme available: %w", err)
				}
			}

			resumeID, _ := cmd.Flags().GetString("resume")

			app, err := cli.NewApp(&cli.AppConfig{
				Theme:           th,
				HistorySize:     cfg.HistorySize,
				AgentSvc:        agentSvc,
				Memory:          mem,
				SessionStore:    ss,
				ModelName:       modelName,
				ResumeSessionID: resumeID,
			})
			if err != nil {
				return fmt.Errorf("creating app: %w", err)
			}

			p := tea.NewProgram(app)
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().StringP("resume", "r", "", "resume a previous session (ID or 'latest')")
	cmd.Flags().Bool("list-sessions", false, "list all saved sessions and exit")
	cmd.Flags().String("delete-session", "", "delete a saved session by ID and exit")

	return cmd
}

func handleListSessions(ss *session.Store) error {
	if ss == nil {
		fmt.Println("Session persistence is not configured.")
		return nil
	}

	summaries, err := ss.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(summaries) == 0 {
		fmt.Println("No saved sessions.")
		return nil
	}

	fmt.Printf("%-24s  %-8s  %-10s  %s\n", "SESSION ID", "MSGS", "MODEL", "FIRST MESSAGE")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	for _, s := range summaries {
		preview := s.FirstUserMsg
		if preview == "" {
			preview = "(empty)"
		}
		fmt.Printf("%-24s  %-8d  %-10s  %s\n", s.SessionID, s.MessageCount, s.Model, preview)
	}

	return nil
}

func handleDeleteSession(ss *session.Store, sessionID string) error {
	if ss == nil {
		fmt.Println("Session persistence is not configured.")
		return nil
	}

	if err := ss.DeleteSession(sessionID); err != nil {
		return fmt.Errorf("deleting session %q: %w", sessionID, err)
	}

	fmt.Printf("Session %q deleted.\n", sessionID)
	return nil
}

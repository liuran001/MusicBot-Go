package kugou

import (
	"context"
	"fmt"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
	if err := platformplugins.Register("kugou", buildContribution); err != nil {
		panic(err)
	}
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	persist := func(pairs map[string]string) error {
		return cfg.PersistPluginConfig("kugou", pairs)
	}
	client := NewClient(cfg.GetPluginString("kugou", "cookie"), logger)
	concept := loadConceptSessionFromConfig(cfg.GetPluginString, cfg.GetPluginBool, cfg.GetPluginInt)
	manager := NewConceptSessionManager(logger, persist, concept)
	manager.SetBaseURL(cfg.GetPluginString("kugou", "concept_base_url"))
	manager.StartAutoRefreshDaemon(context.Background())
	client.AttachConcept(manager)
	commands := BuildAdminCommands(client)
	contrib := &platformplugins.Contribution{Platform: NewPlatform(client)}
	if len(commands) > 0 {
		contrib.Commands = make([]admincmd.Command, 0, len(commands))
		contrib.Commands = append(contrib.Commands, commands...)
	}
	return contrib, nil
}

package kugou

import (
	"context"
	"fmt"

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
	client := NewClient("", logger)
	apiProxyCfg := loadKugouAPIProxyConfig(cfg)
	if err := client.SetAPIProxy(apiProxyCfg); err != nil {
		return nil, err
	}
	if err := client.SetSearchProxy(cfg.GetPluginString("kugou", "search_proxy")); err != nil {
		return nil, err
	}
	concept := loadConceptSessionFromConfig(cfg.GetPluginString, cfg.GetPluginBool, cfg.GetPluginInt)
	manager := NewConceptSessionManager(logger, persist, concept)
	manager.SetHTTPClient(client.apiHTTPClient)
	manager.SetBaseURL(cfg.GetPluginString("kugou", "concept_base_url"))
	manager.StartAutoRefreshDaemon(context.Background())
	client.AttachConcept(manager)
	contrib := &platformplugins.Contribution{Platform: NewPlatform(client)}
	return contrib, nil
}

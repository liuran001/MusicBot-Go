package kugou

import (
	"time"

	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
)

const defaultKugouProxyTimeout = 20 * time.Second

type apiProxyConfigResolver interface {
	ResolveAPIProxyConfig(plugin string) httpproxy.Config
}

func loadKugouAPIProxyConfig(resolver apiProxyConfigResolver) httpproxy.Config {
	if resolver == nil {
		return httpproxy.Config{}
	}
	return resolver.ResolveAPIProxyConfig("kugou")
}

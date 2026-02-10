package handler

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/ini.v1"
)

type Whitelist struct {
	enabled    bool
	chatIDs    map[int64]struct{}
	adminIDs   map[int64]struct{}
	mu         sync.RWMutex
	configPath string
}

func NewWhitelist(enabled bool, chatIDs map[int64]struct{}, adminIDs map[int64]struct{}, configPath string) *Whitelist {
	chatIDCopy := make(map[int64]struct{}, len(chatIDs))
	for id := range chatIDs {
		chatIDCopy[id] = struct{}{}
	}
	adminIDCopy := make(map[int64]struct{}, len(adminIDs))
	for id := range adminIDs {
		adminIDCopy[id] = struct{}{}
	}
	return &Whitelist{
		enabled:    enabled,
		chatIDs:    chatIDCopy,
		adminIDs:   adminIDCopy,
		configPath: strings.TrimSpace(configPath),
	}
}

func (w *Whitelist) IsAllowed(chatID int64, userID int64) bool {
	if w == nil {
		return true
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.enabled {
		return true
	}
	if _, ok := w.adminIDs[userID]; ok {
		return true
	}
	if _, ok := w.chatIDs[chatID]; ok {
		return true
	}
	return false
}

func (w *Whitelist) Add(chatID int64) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.chatIDs[chatID]; exists {
		return false
	}
	w.chatIDs[chatID] = struct{}{}
	return true
}

func (w *Whitelist) Remove(chatID int64) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.chatIDs[chatID]; !exists {
		return false
	}
	delete(w.chatIDs, chatID)
	return true
}

func (w *Whitelist) List() []int64 {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	ids := make([]int64, 0, len(w.chatIDs))
	for id := range w.chatIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids
}

func (w *Whitelist) Persist() error {
	if w == nil {
		return nil
	}
	configPath := strings.TrimSpace(w.configPath)
	if configPath == "" {
		return nil
	}

	ids := w.List()
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, strconv.FormatInt(id, 10))
	}

	cfg, err := ini.Load(configPath)
	if err != nil {
		return err
	}
	section := cfg.Section("")
	section.Key("WhitelistChatIDs").SetValue(strings.Join(values, ","))
	return cfg.SaveTo(configPath)
}

func (w *Whitelist) Enabled() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.enabled
}

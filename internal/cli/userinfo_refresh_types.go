package cli

import "time"

const userInfoRefreshDebounceDelay = 3 * time.Second

type userInfoRefreshSource int

const (
	userInfoRefreshSourceStartup userInfoRefreshSource = iota
	userInfoRefreshSourceManual
	userInfoRefreshSourceAgentPremium
)

type userInfoRefreshDueMsg struct {
	seq int
}

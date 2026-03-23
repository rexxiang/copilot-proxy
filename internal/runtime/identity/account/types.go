package account

import "time"

// AccountDTO exposes account metadata for UI/exports.
type AccountDTO struct {
	User      string
	AppID     string
	HasToken  bool
	IsDefault bool
}

// LoginChallenge describes the device flow challenge returned to users.
type LoginChallenge struct {
	Seq             int64
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresAt       time.Time
}

// LoginResult contains the token and login retrieved after polling.
type LoginResult struct {
	Seq   int64
	Token string
	Login string
}

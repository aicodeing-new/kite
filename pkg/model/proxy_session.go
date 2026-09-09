package model

import "time"

// Only hashes of bearer credentials are persisted. Device sessions are separate
// from browser cookies and never authorize the dashboard/admin APIs.
type ProxySession struct {
	Model
	UserID          uint       `json:"-" gorm:"index;not null"`
	DeviceName      string     `json:"deviceName" gorm:"size:100"`
	AccessHash      string     `json:"-" gorm:"size:64;uniqueIndex"`
	RefreshHash     string     `json:"-" gorm:"size:64;uniqueIndex"`
	AccessExpiresAt time.Time  `json:"-"`
	ExpiresAt       time.Time  `json:"expiresAt" gorm:"index"`
	LastSeenAt      time.Time  `json:"lastSeenAt"`
	RevokedAt       *time.Time `json:"revokedAt"`
}

type ProxyAuthorizationCode struct {
	Model
	UserID      uint      `gorm:"index;not null"`
	CodeHash    string    `gorm:"size:64;uniqueIndex"`
	Challenge   string    `gorm:"size:128"`
	RedirectURI string    `gorm:"size:255"`
	DeviceName  string    `gorm:"size:100"`
	ExpiresAt   time.Time `gorm:"index"`
}

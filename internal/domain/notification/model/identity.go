package model

import (
	"errors"
	"strings"
)

var (
	ErrWorkspaceIDRequired = errors.New("notification: workspace id is required")
	ErrUserIDRequired      = errors.New("notification: user id is required")
)

// WorkspaceID is the tenant boundary carried by every workspace-owned value.
// It is deliberately independent of a host identity aggregate.
type WorkspaceID string

func NewWorkspaceID(value string) (WorkspaceID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrWorkspaceIDRequired
	}
	return WorkspaceID(value), nil
}

func (id WorkspaceID) String() string { return string(id) }

// UserID identifies a recipient or actor without importing a host user model.
type UserID string

func NewUserID(value string) (UserID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrUserIDRequired
	}
	return UserID(value), nil
}

func (id UserID) String() string { return string(id) }

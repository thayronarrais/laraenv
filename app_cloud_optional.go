package main

import "lara4ever/internal/cloud"

// CloudSetEnabled preserves the saved cloud profile and all local capabilities.
func (a *App) CloudSetEnabled(enabled bool) error {
	if a.cloud == nil {
		return cloud.ErrDisabled
	}
	return a.cloud.SetEnabled(enabled)
}

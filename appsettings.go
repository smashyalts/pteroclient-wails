package main

// App settings: the handful of choices that are about how this app behaves
// rather than about any one panel or server.
//
// Kept deliberately small. A setting is a promise to keep supporting both
// answers, so each one here exists because two reasonable people wanted
// different things — not because a default was hard to pick.

import (
	"errors"
	"fmt"
	"strconv"
)

// Setting keys.
const (
	// SettingDeleteConfirm is how much ceremony a delete needs.
	//
	// Deleting something the recycle bin cannot take back used to mean typing
	// DELETE, always. That is right when there is no way back and tedious when
	// there is, and people disagreed about which case a disabled recycle bin
	// is — hence a setting rather than a guess.
	SettingDeleteConfirm = "deleteConfirm"

	// SettingSFTPStreams is how many transfers run at once by default.
	SettingSFTPStreams = "sftpStreams"
)

// Values for SettingDeleteConfirm.
const (
	// DeleteConfirmTyped is the default: type DELETE when the delete cannot be
	// undone, two clicks when it can.
	DeleteConfirmTyped = "typed"
	// DeleteConfirmDouble is two clicks for everything, including deletes with
	// no copy kept. The dialog still says plainly what will not be recoverable.
	DeleteConfirmDouble = "double"
)

// AppSettings is the whole set, with defaults filled in.
type AppSettings struct {
	DeleteConfirm string `json:"delete_confirm"`
	SFTPStreams   int    `json:"sftp_streams"`
	// RecycleBinDefault is here as well as in the Vault because this is where
	// people will look for it.
	RecycleBinDefault bool `json:"recycle_bin_default"`
}

// GetAppSettings returns the current settings.
func (a *App) GetAppSettings() (*AppSettings, error) {
	if a.config == nil {
		return nil, errors.New("not configured")
	}

	streams, err := strconv.Atoi(a.config.AppSetting(SettingSFTPStreams, "6"))
	if err != nil || streams < 1 {
		streams = 6
	}

	return &AppSettings{
		DeleteConfirm:     a.config.AppSetting(SettingDeleteConfirm, DeleteConfirmTyped),
		SFTPStreams:       streams,
		RecycleBinDefault: a.config.RecycleBinDefaultEnabled(),
	}, nil
}

// SetAppSetting stores one setting. Values are checked here rather than trusted
// from the window, so a stored setting is always one the app knows how to
// honour.
func (a *App) SetAppSetting(key, value string) error {
	if a.config == nil {
		return errors.New("not configured")
	}

	switch key {
	case SettingDeleteConfirm:
		if value != DeleteConfirmTyped && value != DeleteConfirmDouble {
			return fmt.Errorf("%q is not a delete confirmation style", value)
		}

	case SettingSFTPStreams:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%q is not a number", value)
		}
		if n < 1 || n > 16 {
			return errors.New("parallel transfers has to be between 1 and 16")
		}

	default:
		// Not a silent no-op: a typo in a key would otherwise look like it
		// worked and then do nothing for ever.
		return fmt.Errorf("there is no setting called %q", key)
	}

	return a.config.SetAppSetting(key, value)
}

// deleteConfirmStyle is what the delete flow asks for.
func (a *App) deleteConfirmStyle() string {
	if a.config == nil {
		return DeleteConfirmTyped
	}
	return a.config.AppSetting(SettingDeleteConfirm, DeleteConfirmTyped)
}

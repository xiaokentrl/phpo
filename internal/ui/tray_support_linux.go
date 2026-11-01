//go:build linux && !android && !server

package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const statusNotifierWatcher = "org.kde.StatusNotifierWatcher"

// DetectTraySupport checks whether a desktop status notifier is available.
func DetectTraySupport() (TraySupport, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return TraySupport{}, fmt.Errorf("connect to session D-Bus: %w", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var available bool
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, statusNotifierWatcher).Store(&available); err != nil {
		return TraySupport{}, fmt.Errorf("check system tray watcher: %w", err)
	}
	return linuxTraySupport(available), nil
}

func linuxTraySupport(available bool) TraySupport {
	if !available {
		return TraySupport{}
	}
	return TraySupport{
		Available: true,
	}
}

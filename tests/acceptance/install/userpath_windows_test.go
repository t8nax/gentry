//go:build windows

package install_test

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// environmentKey holds the environment variables of the user.
const environmentKey = `Environment`

// readUserPath reads the variable Path of the user as it is in the registry:
// with its references %VAR% and its type.
func readUserPath() (userPath, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, environmentKey, registry.QUERY_VALUE)
	if err != nil {
		return userPath{}, err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return userPath{}, nil
	}
	if err != nil {
		return userPath{}, err
	}
	return userPath{value: v, expand: typ == registry.EXPAND_SZ, exists: true}, nil
}

// writeUserPath writes the variable Path of the user, or removes it, with its
// type, and tells the windows the environment has changed, as
// [Environment]::SetEnvironmentVariable does: new processes of Explorer then
// get the variable as written.
func writeUserPath(p userPath) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, environmentKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	switch {
	case !p.exists:
		err = k.DeleteValue("Path")
		if errors.Is(err, registry.ErrNotExist) {
			err = nil
		}
	case p.expand:
		err = k.SetExpandStringValue("Path", p.value)
	default:
		err = k.SetStringValue("Path", p.value)
	}
	if err != nil {
		return err
	}
	return broadcastEnvironment()
}

// The message Windows sends to the windows when the environment changes.
const (
	hwndBroadcast    = 0xffff
	wmSettingChange  = 0x001a
	smtoAbortIfHung  = 0x0002
	broadcastTimeout = 5000 // milliseconds
)

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// broadcastEnvironment tells the windows the environment has changed.
func broadcastEnvironment() error {
	name, err := windows.UTF16PtrFromString(environmentKey)
	if err != nil {
		return err
	}
	var result uintptr
	if r, _, err := sendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(name)),
		smtoAbortIfHung, broadcastTimeout, uintptr(unsafe.Pointer(&result))); r == 0 {
		return fmt.Errorf("SendMessageTimeout WM_SETTINGCHANGE: %w", err)
	}
	return nil
}

// machinePath reads the variable Path of the machine as it is in the
// registry.
func machinePath() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	return v, err
}

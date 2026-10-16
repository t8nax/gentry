//go:build windows

package install_test

import (
	"errors"

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

// writeUserPath writes the variable Path of the user, or removes it.
func writeUserPath(p userPath) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, environmentKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	switch {
	case !p.exists:
		if err := k.DeleteValue("Path"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	case p.expand:
		return k.SetExpandStringValue("Path", p.value)
	default:
		return k.SetStringValue("Path", p.value)
	}
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

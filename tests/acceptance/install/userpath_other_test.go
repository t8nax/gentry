//go:build !windows

package install_test

import "errors"

// The variable Path of the user is in the registry of Windows: the cases
// about it run only there.
var errNoRegistry = errors.New("the variable Path of the user is in the registry of Windows")

func readUserPath() (userPath, error) { return userPath{}, errNoRegistry }

func writeUserPath(userPath) error { return errNoRegistry }

func machinePath() (string, error) { return "", errNoRegistry }

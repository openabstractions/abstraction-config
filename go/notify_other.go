//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package config

import "errors"

const notifier = ""

// This platform is asked rather than telling. Said out loud rather than hidden
// in a latency: How reports it, and a window can say so.
func notifyDirs([]string) (<-chan struct{}, func(), error) {
	return nil, nil, errors.New("this platform has no directory notification")
}

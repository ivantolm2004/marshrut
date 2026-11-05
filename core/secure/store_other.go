//go:build !windows

package secure

import "os"

func replaceFile(from, to string) error { return os.Rename(from, to) }

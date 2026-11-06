//go:build !windows

package catalogue

import (
	"errors"
	"marshrut/secure"
)

func Update(path string, codec secure.Codec, initial Snapshot, change func(*Snapshot) error) (Snapshot, error) {
	return Snapshot{}, errors.New("Каталог доступен в Windows")
}

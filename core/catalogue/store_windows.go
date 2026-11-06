//go:build windows

// Package catalogue stores the local routing catalogue separately from VPN credentials.
package catalogue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"marshrut/route"
	"marshrut/secure"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Update takes an immediate SQLite transaction. The encrypted snapshot gives rules,
// catalogue records and application metadata one atomic commit, including migration.
// No paths, domains, names or VPN credentials appear in SQLite's unencrypted pages.
func Update(path string, codec secure.Codec, initial Snapshot, change func(*Snapshot) error) (Snapshot, error) {
	var result Snapshot
	if codec == nil {
		return result, errors.New("Защищённое хранилище каталога недоступно")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return result, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(absolute)}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return result, errors.New("Не удалось открыть базу маршрутизации")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return result, errors.New("База маршрутизации занята или повреждена")
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var version int
	if err = conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return result, err
	}
	if version != 0 && version != 1 {
		return result, errors.New("Эта версия базы маршрутизации не поддерживается")
	}
	if _, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS routing_state (id INTEGER PRIMARY KEY CHECK(id=1), payload BLOB NOT NULL)"); err != nil {
		return result, err
	}
	var sealed []byte
	err = conn.QueryRowContext(ctx, "SELECT payload FROM routing_state WHERE id=1").Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		if version != 0 {
			return result, errors.New("База маршрутизации повреждена: отсутствуют данные")
		}
		// Detach slices so a rejected transaction cannot mutate the caller's seed.
		plain, e := json.Marshal(initial)
		if e != nil {
			return result, e
		}
		err = json.Unmarshal(plain, &result)
		clear(plain)
	} else if err == nil {
		plain, e := codec.Open(sealed)
		if e != nil {
			return result, errors.New("Не удалось расшифровать базу маршрутизации")
		}
		err = json.Unmarshal(plain, &result)
		clear(plain)
	}
	if err != nil {
		return Snapshot{}, errors.New("База маршрутизации повреждена")
	}
	if result.Schema != 1 {
		return Snapshot{}, errors.New("Эта версия каталога не поддерживается")
	}
	if change != nil {
		if err = change(&result); err != nil {
			return Snapshot{}, err
		}
	}
	if _, err = route.NormalizeRules(result.Rules); err != nil {
		return Snapshot{}, errors.New("В базе маршрутизации повреждены правила")
	}
	if len(result.Applications) > 512 {
		return Snapshot{}, errors.New("Превышен размер каталога")
	}
	if _, err = route.NormalizeCatalogueEntries(result.Entries); err != nil {
		return Snapshot{}, err
	}
	plain, err := json.Marshal(result)
	if err != nil {
		return Snapshot{}, err
	}
	sealed, err = codec.Seal(plain)
	clear(plain)
	if err != nil {
		return Snapshot{}, errors.New("Не удалось зашифровать каталог")
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO routing_state(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", sealed); err != nil {
		return Snapshot{}, errors.New("Не удалось сохранить каталог")
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
		return Snapshot{}, err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return Snapshot{}, errors.New("Не удалось сохранить каталог")
	}
	return result, nil
}

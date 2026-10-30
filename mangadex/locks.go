package mangadex

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"
)

const writeLockName = "database-write"

// LockedError reports that another Kansho instance holds the database write
// lock. It is safe for the UI to show its message directly.
type LockedError struct {
	Owner      string
	AcquiredAt string
}

func (e *LockedError) Error() string {
	return fmt.Sprintf(
		"Kansho database is locked by %q since %s. If the other instance is not running, use Database > Clear Transaction Locks.",
		e.Owner, e.AcquiredAt,
	)
}

// withWriteTx acquires the advisory Kansho write lock, runs fn inside an
// immediate SQLite transaction, commits, and releases the lock. The immediate
// transaction mode plus busy timeout prevents two writers from corrupting the
// database when the file is in a synced directory.
func (s *Store) withWriteTx(fn func(*sql.Tx) error) error {
	owner, err := s.acquireWriteLock()
	if err != nil {
		return err
	}
	defer s.releaseWriteLock(owner)

	tx, err := s.conn.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin database transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("commit database transaction: %w", err)
	}
	s.checkpoint()
	return nil
}

func (s *Store) acquireWriteLock() (string, error) {
	owner := lockOwner()
	_, err := s.conn.Exec(
		`INSERT INTO kansho_locks (lock_name, owner, acquired_at) VALUES (?, ?, CURRENT_TIMESTAMP)`,
		writeLockName, owner,
	)
	if err == nil {
		return owner, nil
	}

	locked, lookupErr := s.currentLock()
	if lookupErr == nil && locked != nil {
		return "", locked
	}
	return "", fmt.Errorf("acquire database write lock: %w", err)
}

func (s *Store) releaseWriteLock(owner string) {
	if _, err := s.conn.Exec(
		`DELETE FROM kansho_locks WHERE lock_name = ? AND owner = ?`,
		writeLockName, owner,
	); err != nil {
		log.Printf("[mangadex] release database write lock: %v", err)
	}
}

func (s *Store) currentLock() (*LockedError, error) {
	var owner, acquiredAt string
	err := s.conn.QueryRow(
		`SELECT owner, acquired_at FROM kansho_locks WHERE lock_name = ?`,
		writeLockName,
	).Scan(&owner, &acquiredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &LockedError{Owner: owner, AcquiredAt: acquiredAt}, nil
}

// ClearLocks removes every Kansho advisory transaction lock. It must only be
// used after the user confirms that no other Kansho instance is writing.
func (s *Store) ClearLocks() (int64, error) {
	result, err := s.conn.Exec(`DELETE FROM kansho_locks`)
	if err != nil {
		return 0, fmt.Errorf("clear Kansho transaction locks: %w", err)
	}
	count, _ := result.RowsAffected()
	return count, nil
}

// LockCount returns the number of advisory transaction locks currently stored.
func (s *Store) LockCount() (int, error) {
	var count int
	if err := s.conn.QueryRow(`SELECT COUNT(*) FROM kansho_locks`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count Kansho transaction locks: %w", err)
	}
	return count, nil
}

func (s *Store) checkpoint() {
	if _, err := s.conn.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		log.Printf("[mangadex] checkpoint database: %v", err)
	}
}

func lockOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d:%d", host, os.Getpid(), time.Now().UnixNano())
}

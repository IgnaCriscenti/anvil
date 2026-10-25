// Package session keeps the open connections to the user's databases:
// one *sql.DB per connection (SPEC §3.3).
package session

import (
	"context"
	"database/sql"
	"sync"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// Session is an open connection. Conn is a copy of the configuration at
// connect time.
//
// If the connection has no database configured (drivers.AllDatabases), DB is
// the maintenance database pool and each server database is opened in its
// own pool when requested with Pool.
type Session struct {
	Conn   models.DBConn
	Driver drivers.Driver
	DB     *sql.DB

	// The password stays in memory only while the session is open
	// (SPEC §3.2), to open the pools of the other databases.
	password string

	mu    sync.Mutex
	pools map[string]*sql.DB
}

// AllDatabases reports whether the session shows every server database.
func (s *Session) AllDatabases() bool {
	return drivers.AllDatabases(s.Conn)
}

// Databases lists the server databases.
func (s *Session) Databases(ctx context.Context) ([]string, error) {
	dbs, err := s.Driver.ListDatabases(ctx, s.DB)
	if err != nil {
		return nil, appErrors.Error(
			"Failed to list databases.",
			"The databases of the server could not be read: "+err.Error(),
			err,
		)
	}
	return dbs, nil
}

// Pool returns the pool for database. With "" (or if the connection has a
// database configured) it returns the main pool.
func (s *Session) Pool(ctx context.Context, database string) (*sql.DB, error) {
	if database == "" || !s.AllDatabases() || database == s.Driver.MaintenanceDB() {
		return s.DB, nil
	}

	s.mu.Lock()
	db, ok := s.pools[database]
	s.mu.Unlock()
	if ok {
		return db, nil
	}

	c := s.Conn
	c.DB = database
	db, err := drivers.Connect(ctx, c, s.password)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pools == nil {
		// Close already ran.
		db.Close()
		return nil, errSessionClosed
	}
	if existing, ok := s.pools[database]; ok {
		db.Close()
		return existing, nil
	}
	s.pools[database] = db
	return db, nil
}

var errSessionClosed = appErrors.Error("Connection closed.", "The connection was closed.", sql.ErrConnDone)

// Tables lists the tables and views of database ("" = the main database).
func (s *Session) Tables(ctx context.Context, database string) ([]drivers.Table, error) {
	db, err := s.Pool(ctx, database)
	if err != nil {
		return nil, err
	}
	tables, err := s.Driver.ListTables(ctx, db)
	if err != nil {
		return nil, appErrors.Error(
			"Failed to list tables.",
			"The tables of the database could not be read: "+err.Error(),
			err,
		)
	}
	return tables, nil
}

// Columns returns the columns of a table or view of database.
func (s *Session) Columns(ctx context.Context, database string, t drivers.Table) ([]drivers.Column, error) {
	db, err := s.Pool(ctx, database)
	if err != nil {
		return nil, err
	}
	cols, err := s.Driver.Columns(ctx, db, t)
	if err != nil {
		return nil, appErrors.Error(
			"Failed to read columns.",
			"The columns of "+t.Name+" could not be read: "+err.Error(),
			err,
		)
	}
	return cols, nil
}

// Close closes the main pool and the pool of each database.
func (s *Session) Close() error {
	s.mu.Lock()
	pools := s.pools
	s.pools = nil
	s.mu.Unlock()

	for _, db := range pools {
		db.Close()
	}
	return s.DB.Close()
}

// Manager is safe for concurrent use: it is called from worker goroutines,
// never from the UI thread with I/O.
type Manager struct {
	mu       sync.Mutex
	sessions map[uint]*Session
}

func NewManager() *Manager {
	return &Manager{sessions: map[uint]*Session{}}
}

// Connect opens the connection (Open + Ping). If it was already open, it
// returns the existing session.
func (m *Manager) Connect(ctx context.Context, conn models.DBConn, password string) (*Session, error) {
	if s, ok := m.Get(conn.ID); ok {
		return s, nil
	}

	d, err := drivers.For(conn.Engine)
	if err != nil {
		return nil, err
	}
	db, err := drivers.Connect(ctx, conn, password)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[conn.ID]; ok {
		// Another goroutine connected in the meantime.
		db.Close()
		return s, nil
	}
	s := &Session{Conn: conn, Driver: d, DB: db, password: password, pools: map[string]*sql.DB{}}
	m.sessions[conn.ID] = s
	return s, nil
}

func (m *Manager) Get(connID uint) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[connID]
	return s, ok
}

// Disconnect closes the connection if it was open.
func (m *Manager) Disconnect(connID uint) error {
	m.mu.Lock()
	s, ok := m.sessions[connID]
	delete(m.sessions, connID)
	m.mu.Unlock()

	if !ok {
		return nil
	}
	return s.Close()
}

// CloseAll closes every connection (when the app exits).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[uint]*Session{}
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}

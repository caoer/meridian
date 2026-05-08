package watch

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// StatusServer listens on a Unix socket for status queries.
type StatusServer struct {
	listener  net.Listener
	stats     *DaemonStats
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// SocketPath returns the socket path for a given config path.
func SocketPath(configPath string) string {
	h := sha256.Sum256([]byte(configPath))
	return fmt.Sprintf("/tmp/meridian-%x.sock", h[:8])
}

// NewStatusServer creates and starts a Unix socket server.
func NewStatusServer(sockPath string, stats *DaemonStats) (*StatusServer, error) {
	// Remove stale socket
	os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("status socket: %w", err)
	}

	s := &StatusServer{
		listener: ln,
		stats:    stats,
		done:     make(chan struct{}),
	}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Close shuts down the status server and removes the socket.
// Safe to call multiple times.
func (s *StatusServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		err = s.listener.Close()
	})
	s.wg.Wait()
	// Clean up socket file
	if addr, ok := s.listener.Addr().(*net.UnixAddr); ok {
		os.Remove(addr.Name)
	}
	return err
}

// Path returns the socket path.
func (s *StatusServer) Path() string {
	return s.listener.Addr().String()
}

func (s *StatusServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *StatusServer) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	snap := s.stats.Snapshot()

	resp := struct {
		RunningSince    string `json:"running_since"`
		EventsProcessed int   `json:"events_processed"`
		LastEvent       string `json:"last_event,omitempty"`
		HooksFired      int   `json:"hooks_fired"`
	}{}
	resp.RunningSince = snap.RunningSince.Format(time.RFC3339)
	resp.EventsProcessed = snap.EventsProcessed
	if !snap.LastEvent.IsZero() {
		resp.LastEvent = snap.LastEvent.Format(time.RFC3339)
	}
	resp.HooksFired = snap.HooksFired

	json.NewEncoder(conn).Encode(resp)
}

// QueryStatus connects to a daemon's status socket and returns the response.
func QueryStatus(sockPath string) ([]byte, error) {
	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("no daemon running (socket %s): %w", sockPath, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	data, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("reading status: %w", err)
	}
	return data, nil
}

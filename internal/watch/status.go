package watch

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// StatusServer listens on a Unix socket for status queries.
type StatusServer struct {
	listener net.Listener
	stats    *DaemonStats
	done     chan struct{}
	wg       sync.WaitGroup
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
func (s *StatusServer) Close() error {
	close(s.done)
	err := s.listener.Close()
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

	snap := s.stats.Snapshot()

	resp := struct {
		Version string `json:"version"`
		Data    struct {
			RunningSince    string `json:"running_since"`
			EventsProcessed int   `json:"events_processed"`
			LastEvent       string `json:"last_event"`
			HooksFired      int   `json:"hooks_fired"`
		} `json:"data"`
	}{
		Version: "0.1",
	}
	resp.Data.RunningSince = snap.RunningSince.Format("2006-01-02T15:04:05Z07:00")
	resp.Data.EventsProcessed = snap.EventsProcessed
	if !snap.LastEvent.IsZero() {
		resp.Data.LastEvent = snap.LastEvent.Format("2006-01-02T15:04:05Z07:00")
	}
	resp.Data.HooksFired = snap.HooksFired

	json.NewEncoder(conn).Encode(resp)
}

// QueryStatus connects to a daemon's status socket and returns the response.
func QueryStatus(sockPath string) ([]byte, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("no daemon running (socket %s): %w", sockPath, err)
	}
	defer conn.Close()

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && n == 0 {
		return nil, fmt.Errorf("reading status: %w", err)
	}
	return buf[:n], nil
}

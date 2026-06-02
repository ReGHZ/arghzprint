package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ReGHZ/arghzprint/pkg/protocol"
)

const (
	writeTimeout = 10 * time.Second
	pongTimeout  = 60 * time.Second
	pingInterval = 30 * time.Second
)

// runWebSocket establishes one WebSocket session and handles it until
// the connection drops or ctx is cancelled. Returns the disconnect error.
func (a *Agent) runWebSocket(ctx context.Context) error {
	cfg := a.cfg.Get()

	wsURL, err := buildWSURL(cfg.BackendURL, cfg.WSPath)
	if err != nil {
		return fmt.Errorf("build ws url: %w", err)
	}

	headers := http.Header{
		"Authorization": []string{"Bearer " + cfg.PrinterToken},
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	if a.onStatus != nil {
		a.onStatus(true)
	}
	slog.Info("websocket connected", "url", wsURL)

	// identify before anything else. Written directly, not via sendCh: the write
	// loop that owns conn writes hasn't started yet, and fetchPending below must
	// not run before the backend knows which types this agent handles.
	hello, _ := json.Marshal(a.helloMessage())
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := conn.WriteMessage(websocket.TextMessage, hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// pull pending jobs that arrived while we were offline
	if err := a.fetchPending(ctx); err != nil {
		slog.Warn("failed to fetch pending jobs on connect", "err", err)
	}

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongTimeout))
	})

	errCh := make(chan error, 1)

	// reader goroutine
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}

			var msg protocol.InboundMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Warn("unreadable message from backend", "err", err)
				continue
			}

			switch msg.Event {
			case protocol.EventPrintJob:
				a.claimAndEnqueue(ctx, msg.Data)
			case protocol.EventPing:
				// backend-initiated ping (not WS-level ping, but app-level)
				a.SendOut(protocol.OutboundMessage{Event: protocol.EventPong})
			}
		}
	}()

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(writeTimeout),
			)
			return nil

		case msg := <-a.sendCh:
			data, _ := json.Marshal(msg)
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return fmt.Errorf("write: %w", err)
			}

		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return fmt.Errorf("ping: %w", err)
			}

		case err := <-errCh:
			return err
		}
	}
}

func buildWSURL(backendURL, wsPath string) (string, error) {
	u, err := url.Parse(backendURL)
	if err != nil {
		return "", err
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already correct
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	u.Path = wsPath
	return u.String(), nil
}

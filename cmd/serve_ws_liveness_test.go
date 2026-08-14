package cmd

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The server-side half of connection liveness (nibs-y59n; the client half is
// nibs-bcif). A client that vanishes without a close frame — laptop sleep,
// network drop — leaves the server holding its goroutine and subscription
// registration until the OS TCP timeout. gqlgen only notices if the transport
// is configured to ping and enforce a pong deadline, and that configuration is
// subprotocol-specific: KeepAlivePingInterval applies ONLY to the legacy
// graphql-ws subprotocol, while the web client (graphql-ws v6) speaks
// graphql-transport-ws, which reads PingPongInterval instead. Misconfigure that
// and the server sends nothing and reaps nothing — measured in the field as
// zero server-initiated frames over 60 seconds.
//
// These tests drive a real WebSocket against the real handler because the bug
// is configuration: only the transport's own run loop knows which setting it
// honors for which subprotocol.

// wsTestInterval is deliberately short so a full ping → missed-pong → reap
// cycle fits in well under a second; detection is bounded by 2x this value.
const wsTestInterval = 250 * time.Millisecond

// dialTransportWS connects to the handler speaking graphql-transport-ws and
// completes the connection_init / connection_ack handshake.
func dialTransportWS(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()

	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", "graphql-transport-ws")
	conn, _, err := (&websocket.Dialer{}).Dial(
		"ws"+strings.TrimPrefix(serverURL, "http")+"/graphql", header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "connection_init"}); err != nil {
		t.Fatalf("connection_init: %v", err)
	}
	var ack map[string]any
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read connection_ack: %v", err)
	}
	if ack["type"] != "connection_ack" {
		t.Fatalf("expected connection_ack, got %v", ack["type"])
	}
	return conn
}

// isOwnTimeout reports whether a read error is this test's own read deadline
// expiring, as opposed to the server closing the connection.
func isOwnTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// A graphql-transport-ws client that stops answering must be pinged and then
// reaped within a bounded time — not held until the OS TCP timeout.
func TestWebSocketServerReapsSilentClient(t *testing.T) {
	app := setupServeTestApp(t)
	server := httptest.NewServer(newGraphQLHandler(app, wsTestInterval))
	defer server.Close()

	conn := dialTransportWS(t, server.URL)

	// Generous ceiling: the mechanism closes at 2x the interval; 20x means a
	// failure here is absence of the mechanism, not scheduling jitter.
	ceiling := 20 * wsTestInterval
	if err := conn.SetReadDeadline(time.Now().Add(ceiling)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	pingSeen := false
	for {
		var msg map[string]any
		err := conn.ReadJSON(&msg)
		if err == nil {
			if msg["type"] == "ping" {
				pingSeen = true // deliberately never answered
			}
			continue
		}
		if isOwnTimeout(err) {
			t.Fatalf("server never closed the silent client within %v (ping seen: %v)", ceiling, pingSeen)
		}
		// The server closed the connection — the reap this test exists for.
		break
	}

	// The close must be the missed-pong deadline doing its job, not something
	// incidental: no ping means the liveness probe never ran at all.
	if !pingSeen {
		t.Fatalf("server closed the connection after %v without ever sending a ping", time.Since(start))
	}
}

// The reap must not be over-eager: a client that answers every ping is healthy
// and stays connected indefinitely.
func TestWebSocketServerKeepsPongingClientConnected(t *testing.T) {
	app := setupServeTestApp(t)
	server := httptest.NewServer(newGraphQLHandler(app, wsTestInterval))
	defer server.Close()

	conn := dialTransportWS(t, server.URL)

	// Long enough for several full ping/deadline cycles; a healthy client
	// surviving one cycle but not three would still be a broken deadline reset.
	window := 8 * wsTestInterval
	if err := conn.SetReadDeadline(time.Now().Add(window)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	pings := 0
	for {
		var msg map[string]any
		err := conn.ReadJSON(&msg)
		if err == nil {
			if msg["type"] == "ping" {
				pings++
				if werr := conn.WriteJSON(map[string]any{"type": "pong"}); werr != nil {
					t.Fatalf("write pong: %v", werr)
				}
			}
			continue
		}
		if isOwnTimeout(err) {
			break // survived the whole window — the success path
		}
		t.Fatalf("server closed a client that answered every ping, after %v (%d pings)", time.Since(start), pings)
	}

	// Without at least two full cycles the assertion above is decoration: one
	// ping could precede a close that simply hadn't landed yet.
	if pings < 2 {
		t.Fatalf("only %d ping(s) in %v — the window did not exercise repeated deadline resets", pings, window)
	}
}

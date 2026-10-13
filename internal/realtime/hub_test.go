package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPublishSubscribe(t *testing.T) {
	h := &Hub{}
	ch, cancel := h.Subscribe("books")
	defer cancel()
	h.Publish(context.Background(), "books", map[string]string{"action": "created"})
	select {
	case m := <-ch:
		if m.Topic != "books" || !strings.Contains(string(m.Payload), "created") {
			t.Fatalf("got %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no message")
	}
	cancel()
	h.Publish(context.Background(), "books", "after cancel")
	if len(h.subs) != 0 {
		t.Fatalf("subscriptions leaked: %v", h.subs)
	}
}

func TestServeJSONAndWebSocket(t *testing.T) {
	h := &Hub{Heartbeat: 50 * time.Millisecond}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Serve(w, r, Options{Topics: []string{"books"}, Snapshot: func(context.Context) (any, error) {
			return map[string]int{"total": 3}, nil
		}})
	}))
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]int
	_ = json.NewDecoder(res.Body).Decode(&snap)
	res.Body.Close()
	if snap["total"] != 3 {
		t.Fatalf("snapshot %v", snap)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f Frame
	_ = json.Unmarshal(data, &f)
	if f.Type != "snapshot" || !strings.Contains(string(f.Data), `"total":3`) {
		t.Fatalf("first frame %s", data)
	}
	// Give the server a moment to subscribe before publishing.
	time.Sleep(20 * time.Millisecond)
	h.Publish(ctx, "books", map[string]string{"action": "updated", "id": "b1"})
	_, data, err = c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(data, &f)
	if f.Type != "event" || f.Topic != "books" || !strings.Contains(string(f.Data), "b1") {
		t.Fatalf("event frame %s", data)
	}
}

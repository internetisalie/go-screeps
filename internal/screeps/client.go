package screeps

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RoomObject represents a live object in a Screeps room.
type RoomObject struct {
	ID       string         `json:"_id"`
	Type     string         `json:"type"`
	Room     string         `json:"room"`
	X        int            `json:"x"`
	Y        int            `json:"y"`
	User     string         `json:"user,omitempty"`
	Name     string         `json:"name,omitempty"`
	Hits     int            `json:"hits,omitempty"`
	HitsMax  int            `json:"hitsMax,omitempty"`
	Level    int            `json:"level,omitempty"`
	Energy   int            `json:"energy,omitempty"`
	Store    map[string]any `json:"store,omitempty"`
	Spawning map[string]any `json:"spawning,omitempty"`
	Raw      map[string]any `json:"-"`
}

// VisualOp represents a single RoomVisual drawing command from a bot.
type VisualOp struct {
	Type string
	X    float32
	Y    float32
	X2   float32
	Y2   float32
	W    float32
	H    float32
	R    float32
	Text string
	Pts  [][2]float32
	Fill string
	Col  string
	Opac float32
	SW   float32
}

// RoomSnapshot is an immutable snapshot of a room for rendering.
type RoomSnapshot struct {
	Room     string
	Terrain  string // 2500 chars: '0' plain, '1' wall, '2' swamp
	GameTime int64
	Objects  []RoomObject
	Visuals  []VisualOp
	Users    map[string]string // userId -> username
	Status   string
}

// Client manages HTTP + WebSocket communication with a Screeps private server.
type Client struct {
	baseURL  string
	email    string
	password string
	http     *http.Client

	mu       sync.RWMutex
	token    string
	room     string
	terrain  string
	gameTime int64
	objects  map[string]map[string]any
	visuals  []VisualOp
	users    map[string]string
	status   string

	wsMu sync.Mutex
	conn *websocket.Conn

	onUpdate func()
}

// NewClient creates a new Screeps client targeting baseURL (e.g. http://localhost:21025).
func NewClient(baseURL, email, password string, onUpdate func()) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		email:    email,
		password: password,
		http:     &http.Client{Timeout: 10 * time.Second},
		objects:  make(map[string]map[string]any),
		users:    map[string]string{"2": "Invader", "3": "Source Keeper"},
		status:   "Connecting...",
		onUpdate: onUpdate,
	}
}

// Snapshot returns a thread-safe copy of the current room state.
func (c *Client) Snapshot() RoomSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	objs := make([]RoomObject, 0, len(c.objects))
	for id, raw := range c.objects {
		obj := parseRoomObject(id, raw)
		objs = append(objs, obj)
	}
	users := make(map[string]string, len(c.users))
	for k, v := range c.users {
		users[k] = v
	}
	visuals := make([]VisualOp, len(c.visuals))
	copy(visuals, c.visuals)
	return RoomSnapshot{
		Room:     c.room,
		Terrain:  c.terrain,
		GameTime: c.gameTime,
		Objects:  objs,
		Visuals:  visuals,
		Users:    users,
		Status:   c.status,
	}
}

// Start connects to the server, signs in, fetches terrain for initialRoom, and streams ticks.
func (c *Client) Start(ctx context.Context, initialRoom string) {
	c.SwitchRoom(initialRoom)

	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.runSession(ctx); err != nil && ctx.Err() == nil {
			c.setStatus(fmt.Sprintf("Disconnected (%v) — retrying...", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// SwitchRoom changes the active room subscription and fetches its terrain.
func (c *Client) SwitchRoom(room string) {
	room = strings.ToUpper(strings.TrimSpace(room))
	if room == "" {
		return
	}

	c.mu.Lock()
	oldRoom := c.room
	if oldRoom == room && c.terrain != "" {
		c.mu.Unlock()
		return
	}
	c.room = room
	c.terrain = ""
	c.objects = make(map[string]map[string]any)
	c.visuals = nil
	c.mu.Unlock()

	go func() {
		terrain, err := c.fetchTerrain(room)
		c.mu.Lock()
		if c.room == room {
			if err != nil {
				c.status = fmt.Sprintf("Terrain error (%s): %v", room, err)
			} else {
				c.terrain = terrain
			}
		}
		c.mu.Unlock()
		c.notify()
	}()

	c.wsMu.Lock()
	conn := c.conn
	if conn != nil {
		if oldRoom != "" && oldRoom != room {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("unsubscribe room:"+oldRoom))
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("subscribe room:"+room))
	}
	c.wsMu.Unlock()
	c.notify()
}

func (c *Client) runSession(ctx context.Context) error {
	token, err := c.signIn()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.token = token
	room := c.room
	c.mu.Unlock()

	if room != "" {
		if terrain, err := c.fetchTerrain(room); err == nil {
			c.mu.Lock()
			if c.room == room {
				c.terrain = terrain
			}
			c.mu.Unlock()
		}
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/socket/websocket"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}

	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stop()

	defer func() {
		c.wsMu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.wsMu.Unlock()
		_ = conn.Close()
	}()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("auth "+token)); err != nil {
		return err
	}

	c.wsMu.Lock()
	c.conn = conn
	c.wsMu.Unlock()

	c.setStatus("Connected")

	// Wait briefly for auth ok, then subscribe to current room.
	subscribed := false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		msg := string(raw)
		if strings.HasPrefix(msg, "gz:") {
			dec, err := decodeGzip(msg[3:])
			if err == nil {
				msg = dec
			}
		}

		if strings.HasPrefix(msg, "auth ok") {
			c.mu.RLock()
			curRoom := c.room
			c.mu.RUnlock()
			if curRoom != "" {
				c.wsMu.Lock()
				_ = conn.WriteMessage(websocket.TextMessage, []byte("subscribe room:"+curRoom))
				c.wsMu.Unlock()
				subscribed = true
			}
			continue
		}

		if !subscribed && strings.HasPrefix(msg, "time ") {
			c.mu.RLock()
			curRoom := c.room
			c.mu.RUnlock()
			if curRoom != "" {
				c.wsMu.Lock()
				_ = conn.WriteMessage(websocket.TextMessage, []byte("subscribe room:"+curRoom))
				c.wsMu.Unlock()
				subscribed = true
			}
		}

		if strings.HasPrefix(msg, "[") {
			c.handleEvent(msg)
		}
	}
}

func (c *Client) handleEvent(msg string) {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(msg), &arr); err != nil || len(arr) < 2 {
		return
	}
	var channel string
	if err := json.Unmarshal(arr[0], &channel); err != nil {
		return
	}
	if !strings.HasPrefix(channel, "room:") {
		return
	}
	roomName := strings.TrimPrefix(channel, "room:")

	var payload struct {
		GameTime int64                     `json:"gameTime"`
		Objects  map[string]map[string]any `json:"objects"`
		Visual   string                    `json:"visual"`
		Users    map[string]struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(arr[1], &payload); err != nil {
		return
	}

	parsedVisuals := parseVisuals(payload.Visual)

	c.mu.Lock()
	if c.room != roomName {
		c.mu.Unlock()
		return
	}
	if payload.GameTime > 0 {
		c.gameTime = payload.GameTime
	}
	c.visuals = parsedVisuals
	for uid, u := range payload.Users {
		if u.Username != "" {
			c.users[uid] = u.Username
		}
	}
	for id, diff := range payload.Objects {
		if diff == nil {
			delete(c.objects, id)
			continue
		}
		existing, ok := c.objects[id]
		if !ok {
			existing = make(map[string]any, len(diff)+1)
			existing["_id"] = id
			c.objects[id] = existing
		}
		for k, v := range diff {
			if v == nil {
				delete(existing, k)
			} else {
				existing[k] = v
			}
		}
	}
	c.mu.Unlock()
	c.notify()
}

func (c *Client) signIn() (string, error) {
	body, _ := json.Marshal(map[string]string{
		"email":    c.email,
		"password": c.password,
	})
	resp, err := c.http.Post(c.baseURL+"/api/auth/signin", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		OK    int    `json:"ok"`
		Token string `json:"token"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Token == "" {
		return "", fmt.Errorf("signin failed: %s", res.Error)
	}
	return res.Token, nil
}

func (c *Client) fetchTerrain(room string) (string, error) {
	u := fmt.Sprintf("%s/api/game/room-terrain?room=%s&encoded=1", c.baseURL, url.QueryEscape(room))
	resp, err := c.http.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		OK      int `json:"ok"`
		Terrain []struct {
			Room    string `json:"room"`
			Terrain string `json:"terrain"`
		} `json:"terrain"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if len(res.Terrain) == 0 || len(res.Terrain[0].Terrain) < 2500 {
		return "", fmt.Errorf("no terrain data for %s", room)
	}
	return res.Terrain[0].Terrain, nil
}

func (c *Client) setStatus(s string) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()
	c.notify()
}

func (c *Client) notify() {
	if c.onUpdate != nil {
		c.onUpdate()
	}
}

func decodeGzip(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	r, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseVisuals(raw string) []VisualOp {
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]VisualOp, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		op := VisualOp{Opac: 0.5, SW: 0.1}
		if t, ok := m["t"].(string); ok {
			op.Type = t
		}
		if x, ok := m["x"].(float64); ok {
			op.X = float32(x)
		}
		if y, ok := m["y"].(float64); ok {
			op.Y = float32(y)
		}
		if x2, ok := m["x2"].(float64); ok {
			op.X2 = float32(x2)
		}
		if y2, ok := m["y2"].(float64); ok {
			op.Y2 = float32(y2)
		}
		if w, ok := m["w"].(float64); ok {
			op.W = float32(w)
		}
		if h, ok := m["h"].(float64); ok {
			op.H = float32(h)
		}
		if txt, ok := m["text"].(string); ok {
			op.Text = txt
		}
		if pts, ok := m["points"].([]any); ok {
			for _, pt := range pts {
				if pair, ok := pt.([]any); ok && len(pair) >= 2 {
					px, _ := pair[0].(float64)
					py, _ := pair[1].(float64)
					op.Pts = append(op.Pts, [2]float32{float32(px), float32(py)})
				}
			}
		}
		if s, ok := m["s"].(map[string]any); ok {
			if r, ok := s["radius"].(float64); ok {
				op.R = float32(r)
			}
			if f, ok := s["fill"].(string); ok {
				op.Fill = f
			}
			if c, ok := s["stroke"].(string); ok {
				op.Col = c
			}
			if c, ok := s["color"].(string); ok && op.Col == "" {
				op.Col = c
			}
			if o, ok := s["opacity"].(float64); ok {
				op.Opac = float32(o)
			}
			if sw, ok := s["strokeWidth"].(float64); ok {
				op.SW = float32(sw)
			}
		}
		out = append(out, op)
	}
	return out
}

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = deepCopyValue(v)
	}
	return cp
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		if val == nil {
			return nil
		}
		cp := make([]any, len(val))
		for i, elem := range val {
			cp[i] = deepCopyValue(elem)
		}
		return cp
	default:
		return v
	}
}

func parseRoomObject(id string, raw map[string]any) RoomObject {
	rawCopy := deepCopyMap(raw)
	obj := RoomObject{
		ID:  id,
		Raw: rawCopy,
	}
	if v, ok := rawCopy["type"].(string); ok {
		obj.Type = v
	}
	if v, ok := rawCopy["room"].(string); ok {
		obj.Room = v
	}
	if v, ok := rawCopy["x"].(float64); ok {
		obj.X = int(v)
	}
	if v, ok := rawCopy["y"].(float64); ok {
		obj.Y = int(v)
	}
	if v, ok := rawCopy["user"].(string); ok {
		obj.User = v
	}
	if v, ok := rawCopy["name"].(string); ok {
		obj.Name = v
	}
	if v, ok := rawCopy["hits"].(float64); ok {
		obj.Hits = int(v)
	}
	if v, ok := rawCopy["hitsMax"].(float64); ok {
		obj.HitsMax = int(v)
	}
	if v, ok := rawCopy["level"].(float64); ok {
		obj.Level = int(v)
	}
	if v, ok := rawCopy["energy"].(float64); ok {
		obj.Energy = int(v)
	}
	if v, ok := rawCopy["store"].(map[string]any); ok {
		obj.Store = v
	}
	if v, ok := rawCopy["spawning"].(map[string]any); ok {
		obj.Spawning = v
	}
	return obj
}

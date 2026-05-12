package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tajchert/suuntool/internal/api"
	"github.com/tajchert/suuntool/internal/session"
)

// startTestServer builds an MCP server with deps pointing at the given
// Suunto-compatible httptest URL and (optional) session, then connects an
// in-memory client and returns the client session. Write+destructive tools
// are registered by default; use startTestServerGated to control tiering.
func startTestServer(t *testing.T, baseURL string, timelineURL string, sess *session.Session) *sdkmcp.ClientSession {
	return startTestServerGated(t, baseURL, timelineURL, sess, true, true)
}

// startTestServerGated is like startTestServer but lets the caller decide
// which tool tiers to expose. Used by the destructive-gating test.
func startTestServerGated(t *testing.T, baseURL string, timelineURL string, sess *session.Session, allowWrite, allowDestructive bool) *sdkmcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	sk := ""
	if sess != nil {
		sk = sess.SessionKey
	}
	cl := api.NewClient(baseURL, sk, time.Second)
	var tl *api.Client
	if timelineURL != "" {
		tl = api.NewClient(timelineURL, sk, time.Second)
	} else {
		tl = api.NewClient(baseURL, sk, time.Second)
	}
	d := &deps{client: cl, timelineClient: tl, session: sess}

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "suuntool", Version: "0"}, nil)
	registerAll(srv, d, allowWrite, allowDestructive)

	clientT, serverT := sdkmcp.NewInMemoryTransports()
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx, serverT)
	}()
	_ = serverT
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func authSession() *session.Session {
	return &session.Session{Email: "you@example.com", Username: "alice", SessionKey: "k1", OffsetMS: 0}
}

func callTool(t *testing.T, cs *sdkmcp.ClientSession, name string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func mustOK(t *testing.T, res *sdkmcp.CallToolResult) {
	t.Helper()
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func newSuuntoStub(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if body, ok := routes[key]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		// Also match path-only (any method) as a convenience.
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		http.Error(w, "no route", http.StatusNotFound)
	}))
}

// Per-tool tests

func TestTool_Whoami(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/user": `{"payload":{"username":"alice","userKey":"uk1","email":"a@b","emailVerified":true},"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	mustOK(t, callTool(t, cs, "whoami", nil))
}

func TestTool_Whoami_AuthExpired(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{}) // no routes — should not be hit.
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", nil)
	res := callTool(t, cs, "whoami", nil)
	if !res.IsError {
		t.Fatal("expected AUTH_EXPIRED error result")
	}
	if got := res.StructuredContent.(map[string]any)["code"]; got != "AUTH_EXPIRED" {
		t.Fatalf("expected code AUTH_EXPIRED, got %v", got)
	}
}

func TestTool_ProfileSettings(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/user/settings": `{"payload":{"theme":"dark","weight":75.5},"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	res := callTool(t, cs, "profile_settings", nil)
	mustOK(t, res)
	if res.StructuredContent == nil {
		t.Fatal("expected structured content")
	}
}

func TestTool_ProfileFollow(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/user/follow": `{"payload":{"followers":1,"followings":2,"blocked":0,"blockedBy":0},"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	mustOK(t, callTool(t, cs, "profile_follow", nil))
}

func TestTool_ProfileUser(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/user/name/bob": `{"payload":{"username":"bob","userKey":"uk2","emailVerified":true},"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	mustOK(t, callTool(t, cs, "profile_user", map[string]any{"username": "bob"}))
}

func TestTool_WorkoutsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/workouts") {
			t.Fatalf("bad path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"payload":[{"key":"w1","username":"alice","activityId":1,"startTime":1000,"stopTime":2000,"totalTime":1,"totalDistance":100}],"error":null,"metadata":{"until":2000}}`))
	}))
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	res := callTool(t, cs, "workouts_list", map[string]any{"limit": 10})
	mustOK(t, res)
	sc := res.StructuredContent.(map[string]any)
	items := sc["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["activityName"] != "RUNNING" {
		t.Fatalf("expected activityName=RUNNING, got %v", first["activityName"])
	}
}

func TestTool_WorkoutsGet(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/workouts/w1": `{"payload":{"key":"w1","username":"alice","activityId":1,"startTime":1000,"stopTime":2000,"totalTime":1,"totalDistance":100},"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	res := callTool(t, cs, "workouts_get", map[string]any{"key": "w1"})
	mustOK(t, res)
	sc := res.StructuredContent.(map[string]any)
	if sc["activityName"] != "RUNNING" {
		t.Fatalf("expected activityName=RUNNING, got %v", sc["activityName"])
	}
}

func TestTool_WorkoutsCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workouts/count" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"payload":{"count":5,"totalCount":42},"error":null,"metadata":null}`))
	}))
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	mustOK(t, callTool(t, cs, "workouts_count", map[string]any{"until_ms": 1000, "sharing_flags": 0}))
}

func TestTool_WorkoutsStats(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/workouts/alice/stats": `{"payload":{"totalDistanceSum":1000,"totalTimeSum":600,"totalEnergyConsumptionSum":500,"totalNumberOfWorkoutsSum":3,"totalDays":1,"allStats":[{"activityId":2,"count":3,"distance":1000,"duration":600,"energy":500}]},"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	res := callTool(t, cs, "workouts_stats", nil)
	mustOK(t, res)
	sc := res.StructuredContent.(map[string]any)
	all := sc["allStats"].([]any)
	if len(all) != 1 {
		t.Fatalf("expected 1 allStats entry, got %d", len(all))
	}
	first := all[0].(map[string]any)
	if first["activityName"] != "CYCLING" {
		t.Fatalf("expected activityName=CYCLING, got %v", first["activityName"])
	}
}

func TestTool_ActivityTypeName(t *testing.T) {
	cs := startTestServer(t, "http://unused/", "", authSession())
	res := callTool(t, cs, "activity_type_name", map[string]any{"id": 22})
	mustOK(t, res)
	sc := res.StructuredContent.(map[string]any)
	if sc["name"] != "TRAIL_RUNNING" {
		t.Fatalf("expected name=TRAIL_RUNNING, got %v", sc["name"])
	}
	// Unknown id falls back to act=<id>.
	res = callTool(t, cs, "activity_type_name", map[string]any{"id": 9999})
	mustOK(t, res)
	sc = res.StructuredContent.(map[string]any)
	if sc["name"] != "act=9999" {
		t.Fatalf("expected fallback act=9999, got %v", sc["name"])
	}
}

func TestTool_WorkoutsSML(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/workouts/w1/sml": `{"hello":"sml"}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	res := callTool(t, cs, "workouts_sml", map[string]any{"key": "w1"})
	mustOK(t, res)
	sc := res.StructuredContent.(map[string]any)
	if sc["base64"] == "" {
		t.Fatal("expected base64 content")
	}

	// save_to=auto spills to a temp file and returns its path; no base64.
	res = callTool(t, cs, "workouts_sml", map[string]any{"key": "w1", "save_to": "auto"})
	mustOK(t, res)
	sc = res.StructuredContent.(map[string]any)
	if _, ok := sc["base64"]; ok {
		t.Fatal("expected base64 absent when save_to set")
	}
	path, _ := sc["path"].(string)
	if path == "" {
		t.Fatal("expected path in response")
	}
	defer os.Remove(path)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spilled file: %v", err)
	}
	if string(body) != `{"hello":"sml"}` {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestTool_WorkoutsFIT(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/workout/exportFit/w1": "BINARYFITDATA",
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	res := callTool(t, cs, "workouts_fit", map[string]any{"key": "w1"})
	mustOK(t, res)
}

func TestTool_WorkoutsComments(t *testing.T) {
	srv := newSuuntoStub(t, map[string]string{
		"/v1/workouts/comments/w1": `{"payload":[{"commentId":1,"author":"bob","comment":"hi","timestamp":123}],"error":null,"metadata":null}`,
	})
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", "", authSession())
	mustOK(t, callTool(t, cs, "workouts_comments", map[string]any{"key": "w1"}))
}

// Wellness uses the timeline client. The timeline path is "v1/{stream}/export".
// We construct the timeline client from baseURL+"/" so paths land under the
// stub's root: /v1/{stream}/export.
func TestTool_WellnessSleep(t *testing.T) {
	body := gzipLines(t, []string{
		`{"timestamp":"2026-05-10T00:00:00Z","entryData":{"k":1}}`,
		`{"timestamp":"2026-05-12T00:00:00Z","entryData":{"k":3}}`,
		`{"timestamp":"2026-05-11T00:00:00Z","entryData":{"k":2}}`,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sleep/export" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", srv.URL+"/", authSession())

	// Default order: newest first, capped by limit.
	res := callTool(t, cs, "wellness_sleep", map[string]any{"since_ms": 1000, "limit": 2})
	mustOK(t, res)
	sc := res.StructuredContent.(map[string]any)
	items := sc["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if got := items[0].(map[string]any)["timestamp"]; got != "2026-05-12T00:00:00Z" {
		t.Fatalf("expected newest first, got %v", got)
	}
	if got := sc["order"]; got != "desc" {
		t.Fatalf("expected order=desc, got %v", got)
	}

	// Ascending order returns oldest first.
	res = callTool(t, cs, "wellness_sleep", map[string]any{"order": "asc"})
	mustOK(t, res)
	items = res.StructuredContent.(map[string]any)["items"].([]any)
	if got := items[0].(map[string]any)["timestamp"]; got != "2026-05-10T00:00:00Z" {
		t.Fatalf("expected oldest first with order=asc, got %v", got)
	}
}

func TestTool_WellnessActivity(t *testing.T) {
	body := gzipLines(t, []string{`{"x":1}`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/activity/export" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", srv.URL+"/", authSession())
	mustOK(t, callTool(t, cs, "wellness_activity", nil))
}

func TestTool_WellnessRecovery(t *testing.T) {
	body := gzipLines(t, []string{`{"x":1}`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/recovery/export" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", srv.URL+"/", authSession())
	mustOK(t, callTool(t, cs, "wellness_recovery", nil))
}

func TestTool_WellnessSleepStages(t *testing.T) {
	body := gzipLines(t, []string{`{"x":1}`})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sleepstages/export" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	cs := startTestServer(t, srv.URL+"/v1/", srv.URL+"/", authSession())
	mustOK(t, callTool(t, cs, "wellness_sleepstages", nil))
}

// gzipLines compresses NDJSON lines (mirrors gzipNDJSON in endpoints tests).
func gzipLines(t *testing.T, lines []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	for _, l := range lines {
		if _, err := w.Write([]byte(l + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

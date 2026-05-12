package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"sort"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tajchert/suuntool/internal/api"
	"github.com/tajchert/suuntool/internal/api/endpoints"
)

// emptyArgs is the shared no-input args struct.
type emptyArgs struct{}

// doctorArgs has no input — the doctor probe is parameterless.
type doctorArgs struct{}

type profileUserArgs struct {
	Username string `json:"username" jsonschema:"the Suunto/Sports-Tracker username to look up"`
}

type workoutsListArgs struct {
	SinceMS int64 `json:"since_ms,omitempty" jsonschema:"workouts modified after this unix-millisecond timestamp (0 = all)"`
	Limit   int   `json:"limit,omitempty" jsonschema:"page size (default 20, server max 100)"`
	Offset  int   `json:"offset,omitempty" jsonschema:"pagination offset"`
}

type workoutKeyArgs struct {
	Key string `json:"key" jsonschema:"the workout key (e.g. 6634ab12cd34ef5678901234)"`
}

// workoutBlobArgs covers SML / FIT fetches whose bodies easily exceed the 1MB
// MCP tool-result cap. save_to lets the caller spill the body to disk instead
// of inlining base64 in the response.
type workoutBlobArgs struct {
	Key    string `json:"key" jsonschema:"the workout key (e.g. 6634ab12cd34ef5678901234)"`
	SaveTo string `json:"save_to,omitempty" jsonschema:"path to write the body to instead of returning it inline; 'auto' = a temp file. When set, the response contains {key, path, size_bytes} and no base64."`
}

type workoutsCountArgs struct {
	UntilMS      int64 `json:"until_ms,omitempty" jsonschema:"upper bound timestamp (unix ms); 0 means now"`
	SharingFlags int   `json:"sharing_flags,omitempty" jsonschema:"sharing-flag bitmask required by the server (use 0 for default)"`
}

type workoutsStatsArgs struct {
	Username string `json:"username,omitempty" jsonschema:"username to fetch stats for; empty = authenticated user"`
}

type wellnessArgs struct {
	SinceMS int64  `json:"since_ms,omitempty" jsonschema:"unix-millisecond cursor; 0 = all history"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max entries to return after ordering (0 = no limit)"`
	Order   string `json:"order,omitempty" jsonschema:"sort order by entry timestamp: 'desc' = newest first (default), 'asc' = oldest first"`
}

// authGate returns AUTH_EXPIRED when no session is loaded. Returns nil if ok.
func authGate(d *deps) *sdkmcp.CallToolResult {
	if d.session == nil {
		return mapErrorToCallToolResult(&api.Error{
			Code: "AUTH_EXPIRED", Message: "no session", Hint: "Run: suuntool login", HTTP: 401, Exit: 4,
		})
	}
	return nil
}

// readNDJSON decodes one JSON object per line from r.
func readNDJSON(r io.ReadCloser) ([]map[string]any, error) {
	defer r.Close()
	dec := json.NewDecoder(r)
	var out []map[string]any
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// orderAndLimit sorts NDJSON-decoded entries by their "timestamp" field and
// optionally truncates. order="asc" → oldest first; anything else → newest
// first (the default — matches the natural "how was my X recently" intent).
// Timestamps are ISO-8601 strings on these streams, so lexicographic sort is
// equivalent to chronological sort.
func orderAndLimit(items []map[string]any, order string, limit int) []map[string]any {
	tsOf := func(m map[string]any) string {
		if s, ok := m["timestamp"].(string); ok {
			return s
		}
		return ""
	}
	if order == "asc" {
		sort.SliceStable(items, func(i, j int) bool { return tsOf(items[i]) < tsOf(items[j]) })
	} else {
		sort.SliceStable(items, func(i, j int) bool { return tsOf(items[i]) > tsOf(items[j]) })
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func orderLabel(order string) string {
	if order == "asc" {
		return "asc"
	}
	return "desc"
}

// deliverBlob handles SML/FIT bodies that frequently exceed the 1MB MCP cap.
//   - saveTo == ""        → read fully and inline as base64 (legacy behaviour)
//   - saveTo == "auto"    → write to a temp file and return its path
//   - saveTo == "/path"   → write to that path verbatim
//
// In file modes the response is {key, path, size_bytes} so the LLM can hand
// off to Read/Bash for further processing. Errors from this helper get
// converted to a structured "blob_error" entry that the LLM can react to.
func deliverBlob(rc io.ReadCloser, key, saveTo, tmpPattern string) map[string]any {
	defer rc.Close()
	if saveTo == "" {
		b, err := io.ReadAll(rc)
		if err != nil {
			return map[string]any{"key": key, "blob_error": err.Error()}
		}
		return map[string]any{"key": key, "size_bytes": len(b), "base64": base64.StdEncoding.EncodeToString(b)}
	}

	var f *os.File
	var err error
	if saveTo == "auto" {
		f, err = os.CreateTemp("", tmpPattern)
	} else {
		f, err = os.Create(saveTo)
	}
	if err != nil {
		return map[string]any{"key": key, "blob_error": err.Error()}
	}
	n, copyErr := io.Copy(f, rc)
	closeErr := f.Close()
	if copyErr != nil {
		return map[string]any{"key": key, "path": f.Name(), "blob_error": copyErr.Error()}
	}
	if closeErr != nil {
		return map[string]any{"key": key, "path": f.Name(), "blob_error": closeErr.Error()}
	}
	return map[string]any{"key": key, "path": f.Name(), "size_bytes": n}
}

// readRegistrars returns the read-only (tierRead) tool registrars.
func readRegistrars() []toolRegistrar {
	return []toolRegistrar{
		// doctor — unauthed health probe.
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "doctor",
				Description: "Suunto server health probe (GET /v1/servertime). Unauthenticated.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ doctorArgs) (*sdkmcp.CallToolResult, any, error) {
				v, err := endpoints.FetchServerTime(ctx, d.client)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, v, nil
			})
		},

		// whoami
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "whoami",
				Description: "Return the authenticated user's profile (GET /v1/user).",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.Whoami(ctx, d.client)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, v, nil
			})
		},

		// profile_settings
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "profile_settings",
				Description: "Return the authenticated user's settings (GET /v1/user/settings).",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				raw, err := endpoints.Settings(ctx, d.client)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				var v map[string]any
				if err := json.Unmarshal(raw, &v); err != nil {
					return mapErrorToCallToolResult(&api.Error{Code: "BAD_ENVELOPE", Message: err.Error(), Exit: 5}), nil, nil
				}
				return nil, v, nil
			})
		},

		// profile_follow
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "profile_follow",
				Description: "Return social follow/block counts for the authenticated user (GET /v1/user/follow).",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ emptyArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.Follow(ctx, d.client)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, v, nil
			})
		},

		// profile_user — lookup by username
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "profile_user",
				Description: "Look up a user profile by username (GET /v1/user/name/{username}).",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args profileUserArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.UserByName(ctx, d.client, args.Username)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, v, nil
			})
		},

		// workouts_list
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_list",
				Description: "List workouts (GET /v1/workouts). Paginated by since/limit/offset. Each item is enriched with activityName alongside the numeric activityId.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutsListArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.ListWorkouts(ctx, d.client, endpoints.ListWorkoutsOpts{
					Since: args.SinceMS, Limit: args.Limit, Offset: args.Offset,
				})
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, enrichWorkoutList(v), nil
			})
		},

		// workouts_get
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_get",
				Description: "Fetch a single workout summary by key (GET /v1/workouts/{key}). Response includes activityName alongside the numeric activityId.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutKeyArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.GetWorkout(ctx, d.client, args.Key)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				if v == nil {
					return nil, nil, nil
				}
				enriched := enrichWorkout(*v)
				return nil, enriched, nil
			})
		},

		// workouts_count
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_count",
				Description: "Return workout counts (GET /v1/workouts/count). Both until and sharingFlags are required by the server.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutsCountArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.CountWorkouts(ctx, d.client, args.UntilMS, args.SharingFlags)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, v, nil
			})
		},

		// workouts_stats
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_stats",
				Description: "Per-activity totals for a user (GET /v1/workouts/{username}/stats). Empty username defaults to the authenticated user. Each allStats entry is enriched with activityName.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutsStatsArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				username := args.Username
				if username == "" {
					username = d.session.Username
				}
				if username == "" {
					return mapErrorToCallToolResult(&api.Error{Code: "USAGE", Message: "username required (session has no username)", Exit: 2}), nil, nil
				}
				v, err := endpoints.Stats(ctx, d.client, username)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, enrichWorkoutStats(v), nil
			})
		},

		// workouts_sml
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_sml",
				Description: "Fetch the full per-workout SML JSON blob (GET /v1/workouts/{key}/sml). SML bodies routinely exceed the 1MB MCP tool-result cap (a hike with GPS+HR samples is ~3–8MB), so prefer passing save_to='auto' (or an explicit path) to spill the body to disk and process it via Read/Bash — only omit save_to for very short workouts.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutBlobArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				rc, err := endpoints.FetchSML(ctx, d.client, args.Key)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, deliverBlob(rc, args.Key, args.SaveTo, "sml-"+args.Key+"-*.json"), nil
			})
		},

		// workouts_fit
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_fit",
				Description: "Fetch the binary FIT export for a workout (GET /v1/workout/exportFit/{key}). FIT bodies can exceed the 1MB MCP tool-result cap; pass save_to='auto' (or a path) to spill to disk, otherwise the body is returned base64-encoded.",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutBlobArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				rc, err := endpoints.FetchFIT(ctx, d.client, args.Key)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, deliverBlob(rc, args.Key, args.SaveTo, "fit-"+args.Key+"-*.fit"), nil
			})
		},

		// workouts_comments
		func(s *sdkmcp.Server, d *deps) {
			sdkmcp.AddTool(s, &sdkmcp.Tool{
				Name:        "workouts_comments",
				Description: "List comments on a workout (GET /v1/workouts/{key}/comments).",
			}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args workoutKeyArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				v, err := endpoints.ListComments(ctx, d.client, args.Key)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				return nil, v, nil
			})
		},

		// wellness_sleep
		makeWellnessTool("wellness_sleep", "List sleep records from Suunto 24/7 wellness (nightly sleep sessions and naps with duration, REM/deep/light split, HR, HRV, SpO2). Use this for questions like 'how was my sleep recently', 'last week's sleep', 'sleep history'. Returns newest first by default (order=desc); pass order=asc for chronological. Use limit to cap entries and since_ms to filter by start time.", endpoints.StreamSleep),
		// wellness_activity
		makeWellnessTool("wellness_activity", "List daily activity records from Suunto 24/7 wellness (steps, calories, intensity buckets). Use this for questions like 'my activity yesterday' or 'weekly steps'. Returns newest first by default (order=desc); pass order=asc for chronological. Use limit to cap entries and since_ms to filter by start time.", endpoints.StreamActivity),
		// wellness_recovery
		makeWellnessTool("wellness_recovery", "List recovery / resources records from Suunto 24/7 wellness (body resources, stress, recovery score over time). Returns newest first by default (order=desc); pass order=asc for chronological. Use limit to cap entries and since_ms to filter by start time.", endpoints.StreamRecovery),
		// wellness_sleepstages
		makeWellnessTool("wellness_sleepstages", "List per-night sleep-stage timeline entries from Suunto 24/7 wellness (awake/REM/light/deep transitions). Returns newest first by default (order=desc); pass order=asc for chronological. Use limit to cap entries and since_ms to filter by start time.", endpoints.StreamSleepStages),

		// activity_type_name (unauthed lookup; uses the embedded ActivityType table)
		registerActivityNameTool,
	}
}

func makeWellnessTool(name, desc string, stream endpoints.WellnessStream) toolRegistrar {
	return func(s *sdkmcp.Server, d *deps) {
		sdkmcp.AddTool(s, &sdkmcp.Tool{Name: name, Description: desc},
			func(ctx context.Context, _ *sdkmcp.CallToolRequest, args wellnessArgs) (*sdkmcp.CallToolResult, any, error) {
				if e := authGate(d); e != nil {
					return e, nil, nil
				}
				rc, err := endpoints.FetchWellness(ctx, d.timelineClient, stream, args.SinceMS)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				items, err := readNDJSON(rc)
				if err != nil {
					return mapErrorToCallToolResult(err), nil, nil
				}
				items = orderAndLimit(items, args.Order, args.Limit)
				return nil, map[string]any{"items": items, "order": orderLabel(args.Order)}, nil
			})
	}
}

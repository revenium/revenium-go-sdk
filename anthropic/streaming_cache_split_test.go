package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const firstPassCacheWriteStart = `{"type":"message_start","message":{"id":"msg_split","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001",` +
	`"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":0,` +
	`"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":0}}}}`

func cacheWriteDelta(total int, split string) string {
	usage := fmt.Sprintf(`"input_tokens":30,"output_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":%d`, total)
	if split != "" {
		usage += `,"cache_creation":` + split
	}
	return `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{` + usage + `}}`
}

func cacheWriteStreamEvents(delta string) []string {
	return []string{
		firstPassCacheWriteStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"content_block_stop","index":0}`,
		delta,
		`{"type":"message_stop"}`,
	}
}

func serveStreamEvents(t *testing.T, events []string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(event), &typed)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
}

type cacheWriteCase struct {
	name       string
	delta      string
	total      float64
	fiveMinute interface{}
	oneHour    interface{}
}

var cacheWriteCases = []cacheWriteCase{
	{"a later pass without a split drops the first pass's", cacheWriteDelta(250, ""), 250, nil, nil},
	{"a later pass's own split replaces the first pass's", cacheWriteDelta(250, `{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":150}`), 250, 100.0, 150.0},
	{"an unchanged total keeps the first pass's split", cacheWriteDelta(100, ""), 100, 100.0, 0.0},
}

func assertCacheWrites(t *testing.T, record map[string]interface{}, want cacheWriteCase) {
	t.Helper()
	assert.Equal(t, want.total, record["cacheCreationTokenCount"])
	assert.Equal(t, want.fiveMinute, record["cacheCreation5mTokenCount"])
	assert.Equal(t, want.oneHour, record["cacheCreation1hTokenCount"])
}

func TestStreamedCacheWriteSplitAlwaysDescribesTheRecordedTotal(t *testing.T) {
	streams := []turnSurface{turnSurfaces[1], turnSurfaces[3]}
	for _, surface := range streams {
		for _, tc := range cacheWriteCases {
			t.Run(surface.name+"/"+tc.name, func(t *testing.T) {
				serveStreamEvents(t, cacheWriteStreamEvents(tc.delta))
				r, mock := newMeteredAnthropic(t)

				surface.call(t, r, context.Background())

				assertCacheWrites(t, flushedRecords(t, r, mock, 1)[0], tc)
			})
		}
	}
}

func TestBedrockStreamedCacheWriteSplitAlwaysDescribesTheRecordedTotal(t *testing.T) {
	for _, tc := range cacheWriteCases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.streamWith(invokeStreamChunks(cacheWriteStreamEvents(tc.delta)...))
			mock := newMeteringMock(t)
			r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(executorModel))
			require.NoError(t, err)
			drainStream(t, stream)

			assertCacheWrites(t, onlyRecord(t, r, mock), tc)
		})
	}
}

package anthropic

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFailedCallRecord_CarriesAttributionOnEverySurface(t *testing.T) {
	for _, surface := range failingSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			rejectEveryRequest(t, http.StatusBadRequest, rejectedRequestReply)
			r, mock := newMeteredAnthropic(t)

			surface.call(t, r, attributedContext())

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, "ERROR", record["stopReason"])
			for field, want := range attributionMetadata() {
				assert.Equal(t, want, record[field], "record field %q", field)
			}
		})
	}
}

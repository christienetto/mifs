package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/christienetto/mifs/server/internal/audio"
	"github.com/christienetto/mifs/server/internal/catalog"
)

func TestPreparationMessages(t *testing.T) {
	for _, tc := range []struct {
		song     catalog.Song
		contains string
	}{
		{catalog.Song{Status: catalog.StatusPending}, "Waiting to prepare"},
		{catalog.Song{Status: catalog.StatusPending, Attempts: 1}, "Waiting to retry"},
		{catalog.Song{Status: catalog.StatusProcessing}, "Finding an audio source"},
		{catalog.Song{Status: catalog.StatusProcessing, DownloadedBytes: 10, DownloadTotalBytes: 100}, "Downloading"},
		{catalog.Song{Status: catalog.StatusProcessing, DownloadedBytes: 100, DownloadTotalBytes: 100}, "Finishing"},
		{catalog.Song{Status: catalog.StatusFailed, StatusDetail: audio.ErrAuthenticationRequired.Error()}, "requires sign-in"},
		{catalog.Song{Status: catalog.StatusFailed, StatusDetail: "private upstream error"}, "could not be downloaded"},
	} {
		got := (&server{}).songJSON(httptest.NewRequest("GET", "/", nil), tc.song)
		if !strings.Contains(got.StatusMessage, tc.contains) || strings.Contains(got.StatusMessage, "private upstream") {
			t.Errorf("status=%s: %q", tc.song.Status, got.StatusMessage)
		}
	}
}

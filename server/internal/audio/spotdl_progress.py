"""Run the pinned spotDL CLI while reporting actual yt-dlp transfer bytes."""
import json
from yt_dlp import YoutubeDL
from spotdl.download.progress_handler import SongTracker
from spotdl.console import console_entry_point

original = SongTracker.yt_dlp_progress_hook

def progress(self, data):
    total = data.get("total_bytes") or data.get("total_bytes_estimate") or 0
    downloaded = data.get("downloaded_bytes") or 0
    if total and data.get("status") in ("downloading", "finished"):
        print("\nMIFS_PROGRESS " + json.dumps({"downloaded": downloaded, "total": total}), flush=True)
    original(self, data)

SongTracker.yt_dlp_progress_hook = progress

# spotDL can swallow a yt-dlp exception and exit successfully without a file.
# Preserve the underlying failure before spotDL reduces it to a generic error.
original_error = YoutubeDL.report_error

def report_error(self, message, *args, **kwargs):
    print("\nMIFS_SOURCE_ERROR " + json.dumps({"message": str(message)}), flush=True)
    return original_error(self, message, *args, **kwargs)

YoutubeDL.report_error = report_error
console_entry_point()

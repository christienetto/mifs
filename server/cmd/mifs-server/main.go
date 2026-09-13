// Command mifs-server serves the MIFS song catalog and imports songs into it.
//
//	mifs-server serve               start the HTTP API (default)
//	mifs-server ingest <dir>...     import song folders (see internal/ingest)
//
// Configuration comes from flags or MIFS_* environment variables; run with -h for details.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/christienetto/mifs/server/internal/api"
	"github.com/christienetto/mifs/server/internal/blob"
	"github.com/christienetto/mifs/server/internal/catalog"
	"github.com/christienetto/mifs/server/internal/ingest"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	command, args := "serve", os.Args[1:]
	if len(args) > 0 && (args[0] == "serve" || args[0] == "ingest") {
		command, args = args[0], args[1:]
	}

	var err error
	switch command {
	case "serve":
		err = serve(logger, args)
	case "ingest":
		err = importSongs(logger, args)
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		logger.Error(command+" failed", "err", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func openData(dir string) (*catalog.Store, *blob.Store, error) {
	blobs, err := blob.Open(filepath.Join(dir, "media"))
	if err != nil {
		return nil, nil, err
	}
	store, err := catalog.Open(filepath.Join(dir, "mifs.db"))
	return store, blobs, err
}

func serve(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("addr", env("MIFS_ADDR", "127.0.0.1:8080"), "listen address; use :8080 to accept LAN devices (MIFS_ADDR)")
	data := flags.String("data", env("MIFS_DATA_DIR", "data"), "directory holding mifs.db and media/ (MIFS_DATA_DIR)")
	publicURL := flags.String("public-url", env("MIFS_PUBLIC_URL", ""), "external origin, e.g. https://api.example.com; default: the request's host (MIFS_PUBLIC_URL)")
	mediaURL := flags.String("media-url", env("MIFS_MEDIA_URL", ""), "base URL media is published at, e.g. a CDN; default: <public-url>/media (MIFS_MEDIA_URL)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	store, blobs, err := openData(*data)
	if err != nil {
		return err
	}
	defer store.Close()
	handler, err := api.New(api.Config{
		Store: store, MediaDir: blobs.Dir(), PublicURL: *publicURL, MediaURL: *mediaURL, Logger: logger,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	count, err := store.Count(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		logger.Warn("catalog is empty; import songs with: mifs-server ingest seed")
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logger.Info("MIFS server listening", "addr", *addr, "songs", count, "data", *data)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

func importSongs(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("ingest", flag.ContinueOnError)
	data := flags.String("data", env("MIFS_DATA_DIR", "data"), "directory holding mifs.db and media/ (MIFS_DATA_DIR)")
	ffmpeg := flags.String("ffmpeg", env("MIFS_FFMPEG", "ffmpeg"), "ffmpeg binary (MIFS_FFMPEG)")
	ffprobe := flags.String("ffprobe", env("MIFS_FFPROBE", "ffprobe"), "ffprobe binary (MIFS_FFPROBE)")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: mifs-server ingest [flags] <song folder or folder of song folders>...")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return flag.ErrHelp
	}

	store, blobs, err := openData(*data)
	if err != nil {
		return err
	}
	defer store.Close()
	ingester := &ingest.Ingester{Store: store, Blobs: blobs, FFmpeg: *ffmpeg, FFprobe: *ffprobe}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var failed int
	for _, root := range flags.Args() {
		dirs, err := ingest.SongDirs(root)
		if err != nil {
			return err
		}
		for _, dir := range dirs {
			start := time.Now()
			song, err := ingester.Ingest(ctx, dir)
			if err != nil {
				failed++
				logger.Error("ingest failed", "dir", dir, "err", err)
				continue
			}
			logger.Info("ingested", "id", song.ID, "title", song.Title, "artist", song.Artist,
				"duration", time.Duration(song.DurationMs)*time.Millisecond, "lyrics", song.HasLyrics,
				"took", time.Since(start).Round(time.Millisecond))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d song(s) failed to import", failed)
	}
	return nil
}

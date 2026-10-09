package calservice

import (
	envhelper "CalSync/EnvHelper"
	"CalSync/graphhelper"
	"CalSync/icalhelper"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
)

const ServiceName = "CalSyncService"

type CalService struct{}

const (
	syncInterval = 24 * time.Hour
	syncWindow   = 30 * 24 * time.Hour
)

const (
	exitConfig     = 1
	exitLoginNeeds = 2
	exitGraphInit  = 3
)

type Syncer struct {
	gh   *graphhelper.Helper
	cals []string
}

func (s *CalService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending, WaitHint: 30_000}

	cfg, err := loadConfig()
	if err != nil {
		log.Printf("config: %v", err)
		return true, exitConfig
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 25*time.Second)
	gh, err := graphhelper.New(initCtx, cfg.graph)
	initCancel()
	if err != nil {
		log.Printf("graph init: %v", err)
		if errors.Is(err, graphhelper.ErrLoginRequired) {
			return true, exitLoginNeeds
		}
		return true, exitGraphInit
	}

	syncer := &Syncer{gh: gh, cals: cfg.cals}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunWorker(ctx, syncer)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	log.Printf("%s started, %d feed(s)", ServiceName, len(cfg.cals))

	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 20_000}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
					log.Print("worker did not stop in time, exiting anyway")
				}
				return false, 0
			}
		case <-done:
			log.Print("worker exited unexpectedly")
			return true, exitGraphInit
		}
	}
}

type config struct {
	graph graphhelper.Config
	cals  []string
}

func loadConfig() (config, error) {
	cfg := config{
		graph: graphhelper.Config{
			TenantID:       os.Getenv("AZURE_TENANT_ID"),
			ClientID:       os.Getenv("AZURE_CLIENT_ID"),
			CacheName:      envOr("CALSYNC_CACHE_NAME", "mycalsvc"),
			AuthRecordPath: envOr("CALSYNC_AUTH_RECORD", `C:\ProgramData\CalSync\auth-record.json`),
			CalendarID:     os.Getenv("CALSYNC_CALENDAR_ID"), // optional
		},
		cals: envhelper.GetEnvList("ICAL_BUSY_SUBS", "|"),
	}
	if cfg.graph.TenantID == "" || cfg.graph.ClientID == "" {
		return cfg, errors.New("AZURE_TENANT_ID and AZURE_CLIENT_ID must be set")
	}
	if len(cfg.cals) == 0 {
		return cfg, errors.New("ICAL_BUSY_SUBS is empty")
	}
	return cfg, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func RunWorker(ctx context.Context, s *Syncer) {
	run := func() {
		if err := s.syncOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("sync failed: %v", err)
		}
	}
	run()

	t := time.NewTicker(syncInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func (s *Syncer) syncOnce(ctx context.Context) error {
	from := time.Now()
	to := from.Add(syncWindow)

	type block struct{ start, end time.Time }
	want := map[string]block{}
	sourcesOK := true

	for _, url := range s.cals {
		events, err := icalhelper.GetIcal(ctx, url, from, to)
		if err != nil {
			log.Printf("fetch %s: %v", redact(url), err)
			sourcesOK = false
			continue
		}
		for _, ev := range events {
			if ev.Start == nil || ev.End == nil {
				continue
			}
			want[key(url, ev.Uid, *ev.Start, *ev.End)] = block{*ev.Start, *ev.End}
		}
	}

	have, err := s.gh.ListBusyBlocks(ctx, from, to)
	if err != nil {
		return err
	}

	for k, b := range want {
		if _, ok := have[k]; ok {
			continue
		}
		if err := s.gh.CreateBusyBlock(ctx, k, b.start, b.end); err != nil {
			return err
		}
	}

	if !sourcesOK {
		return nil
	}
	for k, id := range have {
		if _, ok := want[k]; !ok {
			if err := s.gh.DeleteEvent(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func key(calURL, uid string, start, end time.Time) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d|%d", calURL, uid, start.Unix(), end.Unix()))
	return hex.EncodeToString(sum[:16])
}

func redact(u string) string {
	if len(u) > 24 {
		return u[:24] + "…"
	}
	return u
}

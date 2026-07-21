package server

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type desktopInstanceAcquirer func(chan<- struct{}) (bool, func(), error)

func Run() {
	runDesktopInstance(acquireDesktopInstance, runPrimaryInstance)
}

// runDesktopInstance is the hard startup boundary for normal launches. A
// duplicate launch may only signal the primary window; it must not continue to
// config, database, Codex auth, session history, routing, or UI initialization.
func runDesktopInstance(acquire desktopInstanceAcquirer, startPrimary func(chan struct{})) {
	desktopActivation := make(chan struct{}, 1)
	primary, releaseInstance, err := acquire(desktopActivation)
	if err != nil {
		log.Printf("single-instance initialization failed: %v", err)
		return
	}
	if !primary {
		return
	}
	if releaseInstance != nil {
		defer releaseInstance()
	}
	startPrimary(desktopActivation)
}

func runPrimaryInstance(desktopActivation chan struct{}) {
	cleanupUpdateHelper()
	cfg := defaultConfig()
	addrFlag := flag.String("addr", "", "listen address, for example 127.0.0.1:8787")
	noOpen := flag.Bool("no-open", false, "do not open a client window or browser on start")
	noWindow := flag.Bool("no-window", false, "do not open the desktop client window")
	browserFlag := flag.Bool("browser", false, "also open the default browser")
	configFlag := flag.String("config", "", "config file path")
	dbFlag := flag.String("db", "", "sqlite database file path")
	flag.Parse()

	configPath := *configFlag
	if configPath == "" {
		configPath = defaultConfigPath()
	}
	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = defaultDBPath()
	}
	db, err := openAppDB(dbPath)
	if err != nil {
		log.Fatalf("database open failed: %v", err)
	}
	defer db.Close()

	if loaded, ok, err := loadConfigFromDB(db); err == nil && ok {
		cfg = mergeConfig(cfg, loaded)
	} else if err != nil {
		log.Printf("database config load warning: %v", err)
	} else if loaded, ok, err := migrateLegacyDBIfNeeded(db, dbPath); err == nil && ok {
		cfg = mergeConfig(cfg, loaded)
		log.Printf("migrated database from legacy config directory")
	} else if err != nil {
		log.Printf("legacy database migration warning: %v", err)
	} else if loaded, err := loadConfig(configPath); err == nil {
		cfg = mergeConfig(cfg, loaded)
		if err := saveConfigToDB(db, cfg); err != nil {
			log.Printf("database config migration warning: %v", err)
		}
	} else if *configFlag == "" {
		if loaded, legacyErr := loadConfig(legacyConfigPath()); legacyErr == nil {
			cfg = mergeConfig(cfg, loaded)
			if err := saveConfigToDB(db, cfg); err != nil {
				log.Printf("database legacy config migration warning: %v", err)
			}
		} else if !errors.Is(legacyErr, os.ErrNotExist) {
			log.Printf("legacy config load warning: %v", legacyErr)
		} else if !errors.Is(err, os.ErrNotExist) {
			log.Printf("config load warning: %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("config load warning: %v", err)
	}
	if !cfg.ClientPathsDetected || cfg.ClientPathDetectionVersion < currentClientPathDetectionVersion {
		if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
			// A detection revision only fills missing values so an upgrade never
			// overwrites paths that the user has edited manually.
			cfg = detectClientPaths(cfg, homeDir, !cfg.ClientPathsDetected)
			if saveErr := saveConfigToDB(db, cfg); saveErr != nil {
				log.Printf("client path detection save warning: %v", saveErr)
			}
		} else {
			log.Printf("client path detection warning: %v", homeErr)
		}
	}
	if *addrFlag != "" {
		cfg.Addr = *addrFlag
	}
	if *noOpen {
		cfg.OpenWindow = false
		cfg.OpenBrowser = false
	}
	if *noWindow {
		cfg.OpenWindow = false
	}
	if *browserFlag {
		cfg.OpenBrowser = true
	}

	a := &app{
		cfg:        cfg,
		configPath: configPath,
		dbPath:     dbPath,
		db:         db,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/desktop/activate", desktopActivationHandler(desktopActivation))
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/update", a.handleUpdate)
	mux.HandleFunc("/api/update/progress", a.handleUpdateProgress)
	mux.HandleFunc("/api/client/configure", a.handleClientConfigure)
	mux.HandleFunc("/api/client/routes/apply", a.handleClientRoutesApply)
	mux.HandleFunc("/api/client/restore", a.handleClientRestore)
	mux.HandleFunc("/api/settings/detect-clients", a.handleClientPathDetection)
	mux.HandleFunc("/api/client/codex/history", a.handleCodexHistory)
	mux.HandleFunc("/api/break-armor/status", a.handleBreakArmorStatus)
	mux.HandleFunc("/api/break-armor/preview", a.handleBreakArmorPreview)
	mux.HandleFunc("/api/break-armor/apply", a.handleBreakArmorApply)
	mux.HandleFunc("/api/break-armor/restore", a.handleBreakArmorRestore)
	mux.HandleFunc("/api/break-armor/sessions", a.handleBreakArmorSessions)
	mux.HandleFunc("/api/break-armor/session/preview", a.handleBreakArmorSessionPreview)
	mux.HandleFunc("/api/break-armor/session/patch", a.handleBreakArmorSessionPatch)
	mux.HandleFunc("/api/break-armor/session/backups", a.handleBreakArmorSessionBackups)
	mux.HandleFunc("/api/break-armor/session/restore", a.handleBreakArmorSessionRestore)
	mux.HandleFunc("/api/break-armor/templates", a.handleBreakArmorTemplates)
	mux.HandleFunc("/api/dashboard", a.handleDashboard)
	mux.HandleFunc("/api/logs", a.handleLogs)
	mux.HandleFunc("/api/models", a.handleListModels)
	mux.HandleFunc("/api/model-test", a.handleModelTest)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "application": appSlug})
	})
	mux.HandleFunc("/", a.handleRoute)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withManagementAccess(withCORS(mux)),
		ReadHeaderTimeout: 15 * time.Second,
	}

	localURL := localManagementURL(cfg.Addr)
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if existingVisionRelayHealthy(localURL) {
			log.Printf("%s already running on %s", appSlug, localURL)
			if cfg.OpenWindow {
				if activateErr := activateExistingDesktop(localURL); activateErr != nil {
					log.Printf("existing window activation warning: %v", activateErr)
				}
			} else if cfg.OpenBrowser {
				_ = openBrowser(localURL)
			}
			return
		}
		log.Fatal(err)
	}
	log.Printf("%s listening on %s", appSlug, localURL)
	log.Printf("database: %s", dbPath)

	if cfg.OpenBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = openBrowser(localURL)
		}()
	}
	serverErr := make(chan error, 1)
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()
	// Session reconciliation and route synchronization can scan or rewrite a
	// number of client files. They are maintenance work, not prerequisites for
	// serving the management UI, so keep them off the critical startup path.
	go runStartupMaintenance(a, cfg, localURL)

	if cfg.OpenWindow {
		runTrayApp(localURL, desktopActivation, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			if err := <-serverErr; err != nil {
				log.Printf("server shutdown warning: %v", err)
			}
		})
		return
	}
	if err := <-serverErr; err != nil {
		log.Fatal(err)
	}
}

func runStartupMaintenance(a *app, cfg config, localURL string) {
	// Give the desktop shell and its first API requests priority over background
	// filesystem work. Maintenance remains sequential to avoid overlapping its
	// Codex history and client-route writes.
	time.Sleep(750 * time.Millisecond)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("startup maintenance warning: %v", err)
		return
	}
	a.breakArmorMu.Lock()
	defer a.breakArmorMu.Unlock()
	if cfg.UnifyCodexSessionHistory {
		if result, reconcileErr := reconcileCodexUnifiedHistory(cfg, homeDir); reconcileErr != nil {
			log.Printf("Codex unified history reconciliation warning: %v", reconcileErr)
		} else if result.ConfigUpdated || result.Sessions > 0 || result.Threads > 0 {
			log.Printf("Codex unified history reconciled (config_updated=%t, tracked_sessions=%d, tracked_threads=%d)", result.ConfigUpdated, result.Sessions, result.Threads)
		}
	}
	results, routeErrors := a.configureEnabledClientRoutes(localURL, homeDir)
	if len(results) > 0 {
		log.Printf("synchronized %d enabled client route(s)", len(results))
	}
	for _, routeErr := range routeErrors {
		log.Printf("client route synchronization warning: %s", routeErr)
	}
}
func localManagementURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/"
	}
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

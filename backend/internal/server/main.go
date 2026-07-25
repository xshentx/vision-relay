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
	if err := waitForUpdateParent(); err != nil {
		log.Printf("update restart wait failed: %v", err)
		return
	}
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
	cleanupUpdateFiles()
	cfg := defaultConfig()
	addrFlag := flag.String("addr", "", "relay API listen address, for example 127.0.0.1:8787")
	managementAddrFlag := flag.String("management-addr", "", "management UI listen address, for example 127.0.0.1:18473")
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
	if *managementAddrFlag != "" {
		cfg.ManagementAddr = *managementAddrFlag
	}
	relayAddr, err := normalizeListenAddress(cfg.Addr)
	if err != nil {
		log.Fatal(err)
	}
	managementAddr, err := normalizeManagementListenAddress(cfg.ManagementAddr)
	if err != nil {
		log.Fatal(err)
	}
	if listenPort(relayAddr) == listenPort(managementAddr) {
		log.Fatal("management UI and relay API must use different ports")
	}
	cfg.Addr = relayAddr
	cfg.ManagementAddr = managementAddr
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

	managementHandler := newManagementHandler(a, desktopActivation)
	relayHandler := newRelayHandler(a)
	managementServer := &http.Server{
		Addr:              cfg.ManagementAddr,
		Handler:           managementHandler,
		ReadHeaderTimeout: 15 * time.Second,
	}
	relayServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           relayHandler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	managementURL := localServerURL(cfg.ManagementAddr)
	relayURL := localServerURL(cfg.Addr)
	managementListener, err := net.Listen("tcp", cfg.ManagementAddr)
	if err != nil {
		if existingVisionRelayHealthy(managementURL) {
			log.Printf("%s already running on %s", appSlug, managementURL)
			if cfg.OpenWindow {
				if activateErr := activateExistingDesktop(managementURL); activateErr != nil {
					log.Printf("existing window activation warning: %v", activateErr)
				}
			} else if cfg.OpenBrowser {
				_ = openBrowser(managementURL)
			}
			return
		}
		log.Fatalf("management UI listen failed on %s: %v", cfg.ManagementAddr, err)
	}
	relayListener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		_ = managementListener.Close()
		log.Fatalf("relay API listen failed on %s: %v", cfg.Addr, err)
	}
	log.Printf("%s management UI listening on %s", appSlug, managementURL)
	log.Printf("%s relay API listening on %s", appSlug, relayURL)
	log.Printf("database: %s", dbPath)

	if cfg.OpenBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = openBrowser(managementURL)
		}()
	}
	type serverResult struct {
		name string
		err  error
	}
	serverErr := make(chan serverResult, 2)
	serve := func(name string, server *http.Server, listener net.Listener) {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- serverResult{name: name, err: err}
			return
		}
		serverErr <- serverResult{name: name}
	}
	go serve("management UI", managementServer, managementListener)
	go serve("relay API", relayServer, relayListener)

	// Client routes must always point at the relay API, never at the management UI.
	go runStartupMaintenance(a, cfg, relayURL)

	shutdownServers := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = managementServer.Shutdown(ctx)
		_ = relayServer.Shutdown(ctx)
	}
	if cfg.OpenWindow {
		runTrayApp(managementURL, desktopActivation, func() {
			shutdownServers()
			for range 2 {
				result := <-serverErr
				if result.err != nil {
					log.Printf("%s shutdown warning: %v", result.name, result.err)
				}
			}
		})
		return
	}

	result := <-serverErr
	shutdownServers()
	other := <-serverErr
	if result.err != nil {
		log.Fatalf("%s server failed: %v", result.name, result.err)
	}
	if other.err != nil {
		log.Fatalf("%s server failed: %v", other.name, other.err)
	}
}

func newManagementHandler(a *app, desktopActivation chan<- struct{}) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/desktop/activate", desktopActivationHandler(desktopActivation))
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/relay/status", a.handleRelayStatus)
	mux.HandleFunc("/api/provider-router/status", a.handleProviderRouterStatus)
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
	mux.HandleFunc("/healthz", healthHandler("management"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !isStaticRequest(r) {
			http.NotFound(w, r)
			return
		}
		a.handleWeb(w, r)
	})
	return withManagementAccess(withCORS(mux))
}

func newRelayHandler(a *app) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler("relay"))
	mux.HandleFunc("/", a.handleRoute)
	return withCORS(mux)
}

func healthHandler(surface string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":      "ok",
			"application": appSlug,
			"surface":     surface,
		})
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
func localServerURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/"
	}
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

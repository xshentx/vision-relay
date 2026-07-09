package server

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func Run() {
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
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/debug/vision", a.handleVisionDebug)
	mux.HandleFunc("/api/key", a.handleGenerateKey)
	mux.HandleFunc("/api/logs", a.handleLogs)
	mux.HandleFunc("/api/models", a.handleListModels)
	mux.HandleFunc("/api/test", a.handleTest)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", a.handleRoute)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 15 * time.Second,
	}

	localURL := "http://" + cfg.Addr + "/"
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if existingVisionRelayHealthy(localURL) {
			log.Printf("%s already running on %s", appSlug, localURL)
			if cfg.OpenWindow {
				runClientWindow(localURL)
				return
			}
			if cfg.OpenBrowser {
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

	if cfg.OpenWindow {
		runTrayApp(localURL, func() {
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

func existingVisionRelayHealthy(localURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(localURL + "healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"
	"time"

	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/skills"
	"icepick/internal/storage"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	paths := config.UserPaths()
	var initErr error
	modelConfig := config.Default()
	if err := config.EnsureDirs(paths); err != nil {
		initErr = err
	} else {
		logFile, err := os.OpenFile(filepath.Join(paths.Data, "icepick.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			initErr = err
		} else {
			defer logFile.Close()
			log.SetOutput(logFile)
		}
	}
	if initErr == nil {
		modelConfig, initErr = config.Load(paths.Config)
	}
	var store *storage.Store
	if initErr == nil {
		store, initErr = storage.Open(filepath.Join(paths.Data, "icepick.db"))
	}
	if store != nil {
		defer store.Close()
		if initErr == nil {
			initErr = store.RecoverInterrupted(context.Background())
		}
	}
	if initErr != nil {
		log.Printf("startup: %v", initErr)
	}
	runs := agent.NewRuns(store)
	defer runs.Close()
	catalog, err := skills.Discover(filepath.Join(filepath.Dir(paths.Config), "skills"))
	if err != nil {
		log.Fatal(err)
	}
	toolHub, err := mcptools.NewLocal(context.Background(), catalog)
	if err != nil {
		log.Fatal(err)
	}
	defer toolHub.Close()
	for _, server := range modelConfig.MCPServers {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := toolHub.ConnectCommand(ctx, server.Name, server.Command, server.Args)
		cancel()
		if err != nil {
			log.Printf("MCP %s: %v", server.Name, err)
		}
	}

	app := application.New(application.Options{
		Name:        "ICEPick",
		Description: "Local desktop agent",
		Services: []application.Service{
			application.NewService(&StatusService{store: store, initErr: initErr, configPath: paths.Config, config: modelConfig, runs: runs, tools: toolHub, skills: catalog, chat: model.ChatClient{}}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "ICEPick",
		Width:            1100,
		Height:           720,
		MinWidth:         760,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(245, 248, 250),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

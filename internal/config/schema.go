package config

func configSchema() map[string]interface{} {
	return map[string]interface{}{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"title":       "Clara Configuration",
		"description": "Configuration for the Clara personal assistant",
		"type":        "object",
		"properties": map[string]interface{}{
			"data_dir": map[string]interface{}{
				"type":        "string",
				"description": "Directory for Clara data files",
			},
			"log_level": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"debug", "info", "warn", "error"},
				"description": "Log verbosity level",
			},
			"log_file": map[string]interface{}{
				"type":        "string",
				"description": "Path to the log file",
			},
			"server": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"addr": map[string]interface{}{
						"type":        "string",
						"description": "gRPC server address (host:port)",
					},
				},
			},
			"ollama": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Ollama server URL",
					},
					"embed_model": map[string]interface{}{
						"type":        "string",
						"description": "Embedding model name",
					},
				},
			},
			"tui": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"theme_mode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"system", "dark", "light"},
						"description": "Theme selection mode",
					},
					"dark_theme": map[string]interface{}{
						"type":        "string",
						"description": "Bubbletint theme ID to use in dark mode (empty = native 16-color)",
					},
					"light_theme": map[string]interface{}{
						"type":        "string",
						"description": "Bubbletint theme ID to use in light mode (empty = native 16-color)",
					},
				},
			},
			"integrations": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filesystem": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"enabled": map[string]interface{}{
								"type":        "boolean",
								"description": "Enable filesystem watcher integration",
							},
							"watch_dirs": map[string]interface{}{
								"type":        "array",
								"items":       map[string]interface{}{"type": "string"},
								"description": "Directories to watch for file changes",
							},
							"ingest_concurrency": map[string]interface{}{
								"type":        "integer",
								"description": "Number of concurrent file ingest workers",
							},
						},
					},
					"reminders": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"enabled": map[string]interface{}{
								"type":        "boolean",
								"description": "Enable Apple Reminders integration (macOS only)",
							},
						},
					},
					"taskwarrior": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"enabled": map[string]interface{}{
								"type":        "boolean",
								"description": "Enable Taskwarrior integration",
							},
							"binary_path": map[string]interface{}{
								"type":        "string",
								"description": "Path to the task binary (default: task)",
							},
							"data_dir": map[string]interface{}{
								"type":        "string",
								"description": "Taskwarrior data directory (default: ~/.task)",
							},
						},
					},
				},
			},
		},
	}
}

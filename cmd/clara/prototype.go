package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
	"github.com/spf13/cobra"
)

var prototypeCmd = &cobra.Command{
	Use:   "prototype",
	Short: "Run the go-plugin Native architecture prototype",
	Run: func(cmd *cobra.Command, args []string) {
		runPrototype()
	},
}

func runPrototype() {
	// 1. Launch the Shell Integration Plugin
	shellClient := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"shell": &contract.ShellIntegrationPlugin{},
		},
		Cmd:        exec.Command("./bin/shell"),
		SyncStdout: os.Stdout,
		SyncStderr: os.Stderr,
	})
	defer shellClient.Kill()

	shellRpcClient, err := shellClient.Client()
	if err != nil {
		log.Fatal(err)
	}

	rawShell, err := shellRpcClient.Dispense("shell")
	if err != nil {
		log.Fatal(err)
	}
	shell := rawShell.(contract.ShellIntegration)

	// 2. Launch the Hello Intent Plugin
	intentClient := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"intent": &contract.IntentPlugin{},
		},
		Cmd:        exec.Command("./bin/hello"),
		SyncStdout: os.Stdout,
		SyncStderr: os.Stderr,
	})
	defer intentClient.Kill()

	intentRpcClient, err := intentClient.Client()
	if err != nil {
		log.Fatal(err)
	}

	rawIntent, err := intentRpcClient.Dispense("intent")
	if err != nil {
		log.Fatal(err)
	}
	intent := rawIntent.(contract.Intent)

	// 3. Execute the Intent
	fmt.Println("Clara: Executing 'hello' intent...")
	err = intent.Execute("World", shell)
	if err != nil {
		log.Fatal(err)
	}
}

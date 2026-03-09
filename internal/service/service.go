package service

import (
	"context"
	"fmt"

	"github.com/kardianos/service"
	"github.com/rs/zerolog"
)

// Config defines the service parameters.
type Config struct {
	Name        string
	DisplayName string
	Description string
	UserName    string
	Arguments   []string
}

// Runner is the interface that a service must implement to be run.
type Runner interface {
	Run(ctx context.Context) error
}

type program struct {
	runner Runner
	logger zerolog.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

func (p *program) Start(s service.Service) error {
	p.logger.Info().Msg("service starting")
	p.ctx, p.cancel = context.WithCancel(context.Background())
	go func() {
		if err := p.runner.Run(p.ctx); err != nil {
			p.logger.Error().Err(err).Msg("service failed")
		}
	}()
	return nil
}

func (p *program) Stop(s service.Service) error {
	p.logger.Info().Msg("service stopping")
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}

// HandleCommand processes service-related commands (install, uninstall, start, stop, restart).
// It returns true if the command was handled, false otherwise.
func HandleCommand(runner Runner, cfg Config, logger zerolog.Logger, cmd string) (bool, error) {
	svcConfig := &service.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		UserName:    cfg.UserName,
		Arguments:   cfg.Arguments,
	}

	prg := &program{
		runner: runner,
		logger: logger,
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		return false, fmt.Errorf("create service: %w", err)
	}

	if cmd == "" {
		// Run as a service.
		if err := s.Run(); err != nil {
			return true, fmt.Errorf("run service: %w", err)
		}
		return true, nil
	}

	switch cmd {
	case "install":
		if err := s.Install(); err != nil {
			return true, fmt.Errorf("install service: %w", err)
		}
		fmt.Printf("Service %s installed.\n", cfg.Name)
	case "uninstall":
		if err := s.Uninstall(); err != nil {
			return true, fmt.Errorf("uninstall service: %w", err)
		}
		fmt.Printf("Service %s uninstalled.\n", cfg.Name)
	case "start":
		if err := s.Start(); err != nil {
			return true, fmt.Errorf("start service: %w", err)
		}
		fmt.Printf("Service %s started.\n", cfg.Name)
	case "stop":
		if err := s.Stop(); err != nil {
			return true, fmt.Errorf("stop service: %w", err)
		}
		fmt.Printf("Service %s stopped.\n", cfg.Name)
	case "restart":
		if err := s.Restart(); err != nil {
			return true, fmt.Errorf("restart service: %w", err)
		}
		fmt.Printf("Service %s restarted.\n", cfg.Name)
	case "status":
		status, err := s.Status()
		if err != nil {
			return true, fmt.Errorf("get service status: %w", err)
		}
		fmt.Printf("Service %s status: %v\n", cfg.Name, status)
	default:
		return false, nil
	}

	return true, nil
}

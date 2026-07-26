// Package services contains trusted internal service composition,
// automation support, moderation helpers, and shared internal workflow logic.
//
// sdworkspace/sdbackend/internal/services/internal-services/container.go
//
// GTM:
//
//	Layer: 2.6 Internal Services / Automation Foundation
//	Release Class: SPINE
//	Reason:
//	  Defines the validated dependency container shared by internal automation,
//	  moderation, notification, and status-resolution workflows.
//
//	  This file owns composition contracts only. It contains no persistence,
//	  moderation policy, notification dispatch, or offer lifecycle logic.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Reject invalid service composition before internal workflows execute.
//	Never permit DB-bound operations with nil models, nil logging, nil
//	configuration, or a non-positive database timeout.
//	Preserve compatibility with established Service fields until composition
//	wiring is migrated deliberately.
//	Do not turn this container into a general service locator.
package services

import (
	"fmt"
	"time"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/logging"
)

// Config contains configuration owned by internal services.
type Config struct {
	DBTimeout time.Duration
}

// Validate verifies that the internal-services configuration is usable.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: config is nil", ErrInvalidServiceConfiguration)
	}
	if c.DBTimeout <= 0 {
		return fmt.Errorf(
			"%w: DBTimeout must be greater than zero",
			ErrInvalidServiceConfiguration,
		)
	}
	return nil
}

// Service contains the dependencies shared by internal automation and
// moderation services.
//
// The exported fields are retained for compatibility with existing composition
// code. New composition code should use NewService.
type Service struct {
	Logger       *logging.Logger
	Models       *data.Models
	Cfg          *Config
	ShutdownChan chan struct{}
}

// NewService constructs a validated internal service container.
func NewService(
	logger *logging.Logger,
	models *data.Models,
	cfg *Config,
	shutdownChan chan struct{},
) (*Service, error) {
	if logger == nil {
		return nil, fmt.Errorf(
			"%w: logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if models == nil {
		return nil, fmt.Errorf(
			"%w: models is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Service{
		Logger:       logger,
		Models:       models,
		Cfg:          cfg,
		ShutdownChan: shutdownChan,
	}, nil
}

// validate verifies dependencies required by DB-bound internal-service work.
func (s *Service) validate() error {
	if s == nil {
		return fmt.Errorf(
			"%w: service is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Logger == nil {
		return fmt.Errorf(
			"%w: logger is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	if s.Models == nil {
		return fmt.Errorf(
			"%w: models is nil",
			ErrInvalidServiceConfiguration,
		)
	}
	return s.Cfg.Validate()
}

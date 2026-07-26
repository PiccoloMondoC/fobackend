package services

import "errors"

var (
	ErrInvalidServiceConfiguration = errors.New("invalid internal service configuration")
	ErrNilContext                  = errors.New("context must not be nil")
	ErrNilOffer                    = errors.New("offer must not be nil")
	ErrInvalidDecimal              = errors.New("invalid decimal value")
	ErrOfferMetadataIncomplete     = errors.New("offer metadata incomplete")
	ErrStatusNameRequired          = errors.New("status name cannot be empty")
	ErrMaintenanceJobDisabled      = errors.New("system maintenance notification job is disabled")
)

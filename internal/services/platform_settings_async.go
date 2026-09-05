// Package services provides async wrappers for platform setting internal workflows.
//
// sdworkspace/sdbackend/internal/services/platform_settings_async.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_settings async support provides controlled completion-channel
//	  wrappers around internal platform setting service methods. It supports
//	  non-readiness-critical orchestration without creating background mutation
//	  loops or weakening startup guarantees.
//
//	  Platform setting defaults must still be ensured synchronously during
//	  startup before HTTP traffic is accepted.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve cancellation-aware async result reporting.
//	Do not introduce recurring background mutation loops.
//	Do not bypass internal service validation.
//	Block deployment if this file breaks async platform setting reads,
//	default-setting completion reporting, or service-layer orchestration.
package services

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)

// PlatformSettingAsyncResult carries an async platform setting read result.
type PlatformSettingAsyncResult struct {
	Setting *data.PlatformSetting
	Err     error
}

// PlatformSettingBoolAsyncResult carries an async boolean setting result.
type PlatformSettingBoolAsyncResult struct {
	Value bool
	Err   error
}

// PlatformSettingStringAsyncResult carries an async string setting result.
type PlatformSettingStringAsyncResult struct {
	Value string
	Err   error
}

// PlatformSettingIntegerAsyncResult carries an async integer setting result.
type PlatformSettingIntegerAsyncResult struct {
	Value int64
	Err   error
}

// PlatformSettingDecimalAsyncResult carries an async decimal setting result.
type PlatformSettingDecimalAsyncResult struct {
	Value *big.Rat
	Err   error
}

// PlatformSettingJSONAsyncResult carries an async JSON setting result.
type PlatformSettingJSONAsyncResult struct {
	Value json.RawMessage
	Err   error
}

// EnsureDefaultPlatformSettingsAsync ensures default platform settings
// asynchronously and reports completion.
//
// Production startup must call EnsureDefaultPlatformSettingsInternal
// synchronously. This helper is only for non-readiness-critical orchestration.
func EnsureDefaultPlatformSettingsAsync(ctx context.Context, s *Service) <-chan error {
	ch := make(chan error, 1)

	go func() {
		defer close(ch)
		ch <- s.EnsureDefaultPlatformSettingsInternal(ctx)
	}()

	return ch
}

// GetActivePlatformSettingAsync retrieves an active platform setting asynchronously.
func GetActivePlatformSettingAsync(ctx context.Context, s *Service, key string) <-chan PlatformSettingAsyncResult {
	ch := make(chan PlatformSettingAsyncResult, 1)

	go func() {
		defer close(ch)
		setting, err := s.GetActivePlatformSettingInternal(ctx, key)
		ch <- PlatformSettingAsyncResult{Setting: setting, Err: err}
	}()

	return ch
}

// GetBoolPlatformSettingAsync retrieves a boolean platform setting asynchronously.
func GetBoolPlatformSettingAsync(ctx context.Context, s *Service, key string) <-chan PlatformSettingBoolAsyncResult {
	ch := make(chan PlatformSettingBoolAsyncResult, 1)

	go func() {
		defer close(ch)
		value, err := s.GetBoolPlatformSettingInternal(ctx, key)
		ch <- PlatformSettingBoolAsyncResult{Value: value, Err: err}
	}()

	return ch
}

// GetStringPlatformSettingAsync retrieves a string platform setting asynchronously.
func GetStringPlatformSettingAsync(ctx context.Context, s *Service, key string) <-chan PlatformSettingStringAsyncResult {
	ch := make(chan PlatformSettingStringAsyncResult, 1)

	go func() {
		defer close(ch)
		value, err := s.GetStringPlatformSettingInternal(ctx, key)
		ch <- PlatformSettingStringAsyncResult{Value: value, Err: err}
	}()

	return ch
}

// GetIntegerPlatformSettingAsync retrieves an integer platform setting asynchronously.
func GetIntegerPlatformSettingAsync(ctx context.Context, s *Service, key string) <-chan PlatformSettingIntegerAsyncResult {
	ch := make(chan PlatformSettingIntegerAsyncResult, 1)

	go func() {
		defer close(ch)
		value, err := s.GetIntegerPlatformSettingInternal(ctx, key)
		ch <- PlatformSettingIntegerAsyncResult{Value: value, Err: err}
	}()

	return ch
}

// GetDecimalPlatformSettingAsync retrieves a decimal platform setting asynchronously.
func GetDecimalPlatformSettingAsync(ctx context.Context, s *Service, key string) <-chan PlatformSettingDecimalAsyncResult {
	ch := make(chan PlatformSettingDecimalAsyncResult, 1)

	go func() {
		defer close(ch)
		value, err := s.GetDecimalPlatformSettingInternal(ctx, key)
		ch <- PlatformSettingDecimalAsyncResult{Value: value, Err: err}
	}()

	return ch
}

// GetJSONPlatformSettingAsync retrieves a JSON platform setting asynchronously.
func GetJSONPlatformSettingAsync(ctx context.Context, s *Service, key string) <-chan PlatformSettingJSONAsyncResult {
	ch := make(chan PlatformSettingJSONAsyncResult, 1)

	go func() {
		defer close(ch)
		value, err := s.GetJSONPlatformSettingInternal(ctx, key)
		ch <- PlatformSettingJSONAsyncResult{Value: value, Err: err}
	}()

	return ch
}

// IsFutureOfferingEnabledAsync checks Future Offering workflow availability asynchronously.
func IsFutureOfferingEnabledAsync(ctx context.Context, s *Service) <-chan PlatformSettingBoolAsyncResult {
	return GetBoolPlatformSettingAsync(ctx, s, platformSettingFutureOfferingEnabled)
}

// IsPlatformSettingsAdminEnabledAsync checks platform settings administration asynchronously.
func IsPlatformSettingsAdminEnabledAsync(ctx context.Context, s *Service) <-chan PlatformSettingBoolAsyncResult {
	return GetBoolPlatformSettingAsync(ctx, s, platformSettingAdminEnabled)
}

// IsPlatformSettingsHardDeleteEnabledAsync checks platform settings hard-delete availability asynchronously.
func IsPlatformSettingsHardDeleteEnabledAsync(ctx context.Context, s *Service) <-chan PlatformSettingBoolAsyncResult {
	return GetBoolPlatformSettingAsync(ctx, s, platformSettingHardDeleteEnabled)
}

// IsMerchantDebitCardRequiredAtOnboardingAsync checks merchant onboarding debit-card requirement asynchronously.
func IsMerchantDebitCardRequiredAtOnboardingAsync(ctx context.Context, s *Service) <-chan PlatformSettingBoolAsyncResult {
	return GetBoolPlatformSettingAsync(ctx, s, platformSettingMerchantDebitCardRequiredAtOnboarding)
}

// IsMerchantDebitCardRequiredForFutureOfferingFeeAsync checks Future Offering fee debit-card requirement asynchronously.
func IsMerchantDebitCardRequiredForFutureOfferingFeeAsync(ctx context.Context, s *Service) <-chan PlatformSettingBoolAsyncResult {
	return GetBoolPlatformSettingAsync(ctx, s, platformSettingMerchantDebitCardRequiredForFutureOfferingFee)
}

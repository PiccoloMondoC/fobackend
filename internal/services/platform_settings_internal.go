// Package services provides internal service methods for platform setting
// bootstrap and operational helper access.
//
// sdworkspace/sdbackend/internal/services/platform_settings_internal.go
//
// GTM:
//
//	Layer: 2.1 Database / Governance Foundation
//	Release Class: SPINE
//	Reason:
//	  platform_settings internal service is release-critical platform
//	  governance infrastructure. It ensures canonical platform-level defaults
//	  before HTTP traffic is accepted and provides typed service-layer accessors
//	  for operational configuration reads.
//
//	  Platform settings are system configuration infrastructure, not user
//	  preferences, merchant settings, consumer product settings, pricing policy,
//	  fee configuration, waiver policy, credit policy, or a generic
//	  product-behavior escape hatch.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve synchronous startup seed behavior.
//	Preserve typed accessor correctness.
//	Preserve setting_value opacity in logs.
//	Preserve DB-owned lifecycle timestamps.
//	Block deployment if this file breaks startup readiness, platform setting
//	bootstrap, typed reads, or Future Offering governance gates.
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/PiccoloMondoC/sdworkspace/sdbackend/internal/data"
)

const (
	platformSettingFutureOfferingEnabled                 = "future_offering_enabled"
	platformSettingAdminEnabled                          = "platform_settings_admin_enabled"
	platformSettingHardDeleteEnabled                     = "platform_settings_hard_delete_enabled"
	platformSettingMerchantDebitCardRequiredAtOnboarding = "merchant_debit_card_required_at_onboarding"
	platformSettingMerchantDebitCardRequiredForFutureOfferingFee = "merchant_debit_card_required_for_future_offering_fee"
)

type platformSettingDefault struct {
	Key         string
	RawValue    json.RawMessage
	ValueType   data.PlatformSettingValueType
	Description string
	IsActive    bool
}

func canonicalPlatformSettingDefaults() []platformSettingDefault {
	return []platformSettingDefault{
		{
			Key:         platformSettingFutureOfferingEnabled,
			RawValue:    json.RawMessage(`true`),
			ValueType:   data.PlatformSettingValueTypeBoolean,
			Description: "Governs whether Future Offering workflows are enabled platform-wide.",
			IsActive:    true,
		},
		{
			Key:         platformSettingAdminEnabled,
			RawValue:    json.RawMessage(`true`),
			ValueType:   data.PlatformSettingValueTypeBoolean,
			Description: "Governs whether the platform settings administrative surface is enabled.",
			IsActive:    true,
		},
		{
			Key:         platformSettingHardDeleteEnabled,
			RawValue:    json.RawMessage(`false`),
			ValueType:   data.PlatformSettingValueTypeBoolean,
			Description: "Governs whether hard delete of platform settings is permitted. False by default.",
			IsActive:    true,
		},
		{
			Key:         platformSettingMerchantDebitCardRequiredAtOnboarding,
			RawValue:    json.RawMessage(`false`),
			ValueType:   data.PlatformSettingValueTypeBoolean,
			Description: "Controls whether a merchant must provide a debit card during onboarding.",
			IsActive:    true,
		},
		{
			Key:         platformSettingMerchantDebitCardRequiredForFutureOfferingFee,
			RawValue:    json.RawMessage(`true`),
			ValueType:   data.PlatformSettingValueTypeBoolean,
			Description: "Controls whether a merchant must have a debit card before Future Offering fee exposure.",
			IsActive:    true,
		},
	}
}

func validatePlatformSettingService(s *Service) error {
	if s == nil {
		return errors.New("platform setting service is required")
	}
	if s.Logger == nil {
		return errors.New("platform setting service logger is required")
	}
	if s.Models == nil {
		return errors.New("platform setting service models are required")
	}
	if s.Cfg == nil {
		return errors.New("platform setting service config is required")
	}
	if s.Cfg.DBTimeout <= 0 {
		return errors.New("platform setting service DB timeout must be greater than zero")
	}
	return nil
}

func platformSettingContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// EnsureDefaultPlatformSettingsInternal idempotently ensures canonical platform
// setting defaults before HTTP traffic is accepted.
func (s *Service) EnsureDefaultPlatformSettingsInternal(ctx context.Context) error {
	if err := validatePlatformSettingService(s); err != nil {
		return err
	}

	ctx = platformSettingContext(ctx)
	logger := s.Logger.GetLoggerWithContextFromContext(ctx).
		WithFunctionName("EnsureDefaultPlatformSettingsInternal")

	for _, def := range canonicalPlatformSettingDefaults() {
		dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)

		setting, err := s.Models.PlatformSetting.Ensure(
			dbCtx,
			def.Key,
			def.RawValue,
			def.ValueType,
			def.Description,
			def.IsActive,
			nil,
		)
		cancel()

		if err != nil {
			logger.Error("Ensure default platform setting failed",
				"setting_key", def.Key,
				"value_type", def.ValueType,
				"error", err,
			)
			return fmt.Errorf("ensure default platform setting %q: %w", def.Key, err)
		}

		logger.Info("Default platform setting ensured",
			"setting_id", setting.ID,
			"setting_key", setting.SettingKey,
			"value_type", setting.ValueType,
			"is_active", setting.IsActive,
		)
	}

	return nil
}

// GetActivePlatformSettingInternal retrieves an active, non-deleted platform setting.
func (s *Service) GetActivePlatformSettingInternal(ctx context.Context, key string) (*data.PlatformSetting, error) {
	if err := validatePlatformSettingService(s); err != nil {
		return nil, err
	}

	ctx = platformSettingContext(ctx)
	dbCtx, cancel := context.WithTimeout(ctx, s.Cfg.DBTimeout)
	defer cancel()

	setting, err := s.Models.PlatformSetting.GetActiveByKey(dbCtx, key)
	if err != nil {
		return nil, fmt.Errorf("get active platform setting %q: %w", key, err)
	}

	return setting, nil
}

// GetBoolPlatformSettingInternal returns a boolean platform setting value.
func (s *Service) GetBoolPlatformSettingInternal(ctx context.Context, key string) (bool, error) {
	setting, err := s.GetActivePlatformSettingInternal(ctx, key)
	if err != nil {
		return false, err
	}
	if setting == nil {
		return false, fmt.Errorf("active platform setting %q not found", key)
	}
	if setting.ValueType != data.PlatformSettingValueTypeBoolean {
		return false, fmt.Errorf("platform setting %q has value_type %q, expected boolean", key, setting.ValueType)
	}

	var value bool
	if err := json.Unmarshal(bytes.TrimSpace(setting.SettingValue), &value); err != nil {
		return false, fmt.Errorf("platform setting %q value is not a valid JSON boolean", key)
	}

	return value, nil
}

// GetStringPlatformSettingInternal returns a string platform setting value.
func (s *Service) GetStringPlatformSettingInternal(ctx context.Context, key string) (string, error) {
	setting, err := s.GetActivePlatformSettingInternal(ctx, key)
	if err != nil {
		return "", err
	}
	if setting == nil {
		return "", fmt.Errorf("active platform setting %q not found", key)
	}
	if setting.ValueType != data.PlatformSettingValueTypeString {
		return "", fmt.Errorf("platform setting %q has value_type %q, expected string", key, setting.ValueType)
	}

	var value string
	if err := json.Unmarshal(bytes.TrimSpace(setting.SettingValue), &value); err != nil {
		return "", fmt.Errorf("platform setting %q value is not a valid JSON string", key)
	}

	return value, nil
}

// GetIntegerPlatformSettingInternal returns an int64 platform setting value.
func (s *Service) GetIntegerPlatformSettingInternal(ctx context.Context, key string) (int64, error) {
	setting, err := s.GetActivePlatformSettingInternal(ctx, key)
	if err != nil {
		return 0, err
	}
	if setting == nil {
		return 0, fmt.Errorf("active platform setting %q not found", key)
	}
	if setting.ValueType != data.PlatformSettingValueTypeInteger {
		return 0, fmt.Errorf("platform setting %q has value_type %q, expected integer", key, setting.ValueType)
	}

	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(setting.SettingValue)))
	decoder.UseNumber()

	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, fmt.Errorf("platform setting %q value is not a valid JSON number", key)
	}

	integer, ok := new(big.Int).SetString(number.String(), 10)
	if !ok {
		return 0, fmt.Errorf("platform setting %q value is not a whole JSON number", key)
	}
	if !integer.IsInt64() {
		return 0, fmt.Errorf("platform setting %q integer value overflows int64", key)
	}

	return integer.Int64(), nil
}

// GetDecimalPlatformSettingInternal returns a decimal platform setting value.
func (s *Service) GetDecimalPlatformSettingInternal(ctx context.Context, key string) (*big.Rat, error) {
	setting, err := s.GetActivePlatformSettingInternal(ctx, key)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return nil, fmt.Errorf("active platform setting %q not found", key)
	}
	if setting.ValueType != data.PlatformSettingValueTypeDecimal {
		return nil, fmt.Errorf("platform setting %q has value_type %q, expected decimal", key, setting.ValueType)
	}

	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(setting.SettingValue)))
	decoder.UseNumber()

	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return nil, fmt.Errorf("platform setting %q value is not a valid JSON number", key)
	}

	value, ok := new(big.Rat).SetString(number.String())
	if !ok {
		return nil, fmt.Errorf("platform setting %q value is not a valid decimal number", key)
	}

	return value, nil
}

// GetJSONPlatformSettingInternal returns a defensive copy of a JSON setting value.
func (s *Service) GetJSONPlatformSettingInternal(ctx context.Context, key string) (json.RawMessage, error) {
	setting, err := s.GetActivePlatformSettingInternal(ctx, key)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return nil, fmt.Errorf("active platform setting %q not found", key)
	}
	if setting.ValueType != data.PlatformSettingValueTypeJSON {
		return nil, fmt.Errorf("platform setting %q has value_type %q, expected json", key, setting.ValueType)
	}

	raw := bytes.TrimSpace(setting.SettingValue)
	out := make(json.RawMessage, len(raw))
	copy(out, raw)

	return out, nil
}

// IsFutureOfferingEnabledInternal reports whether Future Offering workflows are enabled.
func (s *Service) IsFutureOfferingEnabledInternal(ctx context.Context) (bool, error) {
	return s.GetBoolPlatformSettingInternal(ctx, platformSettingFutureOfferingEnabled)
}

// IsPlatformSettingsAdminEnabledInternal reports whether platform settings administration is enabled.
func (s *Service) IsPlatformSettingsAdminEnabledInternal(ctx context.Context) (bool, error) {
	return s.GetBoolPlatformSettingInternal(ctx, platformSettingAdminEnabled)
}

// IsPlatformSettingsHardDeleteEnabledInternal reports whether platform settings hard delete is enabled.
func (s *Service) IsPlatformSettingsHardDeleteEnabledInternal(ctx context.Context) (bool, error) {
	return s.GetBoolPlatformSettingInternal(ctx, platformSettingHardDeleteEnabled)
}

// IsMerchantDebitCardRequiredAtOnboardingInternal reports whether merchants must provide a debit card during onboarding.
func (s *Service) IsMerchantDebitCardRequiredAtOnboardingInternal(ctx context.Context) (bool, error) {
	return s.GetBoolPlatformSettingInternal(ctx, platformSettingMerchantDebitCardRequiredAtOnboarding)
}

// IsMerchantDebitCardRequiredForFutureOfferingFeeInternal reports whether merchants must have a debit card before Future Offering fee exposure.
func (s *Service) IsMerchantDebitCardRequiredForFutureOfferingFeeInternal(ctx context.Context) (bool, error) {
	return s.GetBoolPlatformSettingInternal(ctx, platformSettingMerchantDebitCardRequiredForFutureOfferingFee)
}

// RequireFutureOfferingEnabledInternal fails if Future Offering workflows are disabled.
func (s *Service) RequireFutureOfferingEnabledInternal(ctx context.Context) error {
	enabled, err := s.IsFutureOfferingEnabledInternal(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return errors.New("future offering workflows are not enabled on this platform")
	}
	return nil
}

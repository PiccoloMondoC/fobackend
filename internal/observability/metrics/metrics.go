// Package metrics centralises Prometheus collectors shared by handlers,
// services, workers, and automation jobs.
//
// Importing this package registers collectors through package init, keeping
// collector ownership centralized and avoiding cmd/api ↔ service import cycles.
//
// sdworkspace/sdbackend/internal/observability/metrics/metrics.go
//
// GTM:
//   Layer: 2.1 Database / Governance Foundation
//   Release Class: SPINE
//   Reason:
//     Offer, favorite, notification, and profile metrics are release-critical
//     observability infrastructure.
//
// SPINE Rule:
//   Keep compiling.
//   Keep production-ready.
//   Preserve centralized Prometheus collector ownership.
//   Preserve single-registration behavior through package init.
//   Preserve handler/service/worker-safe metric access.
//   Block deployment if this file breaks build, metrics registration,
//   automation observability, release-critical workflow monitoring,
//   or operational traceability.
package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	// ResultSuccess is the canonical result label value for completed work.
	ResultSuccess = "success"

	// ResultFailure is the canonical result label value for failed work.
	ResultFailure = "failure"

	// OperationInsertOffer is the canonical operation label value for async curated-offer insertion.
	OperationInsertOffer = "insert_offer"

	// OperationApproveOffer is the canonical operation label value for async curated-offer approval.
	OperationApproveOffer = "approve_offer"

	// OperationRejectOffer is the canonical operation label value for async curated-offer rejection.
	OperationRejectOffer = "reject_offer"

	// OperationGenerateOfferDescription is the canonical operation label value for AI-assisted offer description generation.
	OperationGenerateOfferDescription = "generate_offer_description"

	// OperationFraudScanOffer is the canonical operation label value for async offer fraud scanning.
	OperationFraudScanOffer = "fraud_scan_offer"

	// OperationAutoExpireOffer is the canonical operation label value for async offer expiration.
	OperationAutoExpireOffer = "auto_expire_offer"

	// OperationRemoveExpiredOffer is the canonical operation label value for async expired-offer removal.
	OperationRemoveExpiredOffer = "remove_expired_offer"

	// OperationSuggestOffers is the canonical operation label value for async personalized offer suggestion work.
	OperationSuggestOffers = "suggest_offers"

	// OperationPlanSuggestions is the canonical operation label value for async suggestion planning batches.
	OperationPlanSuggestions = "plan_suggestions"

	// OperationUpdateOfferStatus is the canonical operation label value for async offer status updates.
	OperationUpdateOfferStatus = "update_offer_status"

	// OperationFlagOffer is the canonical operation label value for async offer flagging.
	OperationFlagOffer = "flag_offer"

	// OperationBatchAutoFlagOffers is the canonical operation label value for batch offer auto-flagging.
	OperationBatchAutoFlagOffers = "batch_auto_flag_offers"

	// OperationBlacklistOffer is the canonical operation label value for async offer blacklisting.
	OperationBlacklistOffer = "blacklist_offer"

	// OperationPersonalizeFavorites is the canonical operation label value for favorite-based personalization.
	OperationPersonalizeFavorites = "personalize_favorites"

	// OperationRecommendFavorites is the canonical operation label value for favorite-based recommendations.
	OperationRecommendFavorites = "recommend_favorites"

	// OperationAutoExpireFavorites is the canonical operation label value for async favorite expiration.
	OperationAutoExpireFavorites = "auto_expire_favorites"

	// OperationBatchExpireFavorites is the canonical operation label value for batch inactive-favorite expiration.
	OperationBatchExpireFavorites = "batch_expire_favorites"

	// OperationPurgeFavorites is the canonical operation label value for purging soft-deleted favorites.
	OperationPurgeFavorites = "purge_favorites"

	// OperationRestoreFavorite is the canonical operation label value for restoring a soft-deleted favorite.
	OperationRestoreFavorite = "restore_favorite"

	// OperationNotifyUser is the canonical operation label value for async user notification dispatch.
	OperationNotifyUser = "notify_user"

	// OperationAutoFlagProfile is the canonical operation label value for profile auto-moderation.
	OperationAutoFlagProfile = "auto_flag_profile"

	// OperationBatchAutoFlagProfiles is the canonical operation label value for batch profile auto-flagging.
	OperationBatchAutoFlagProfiles = "batch_auto_flag_profiles"

	// OperationValidateProfileHandle is the canonical operation label value for profile handle validation.
	OperationValidateProfileHandle = "validate_profile_handle"

	// OperationResolveProfileOwner is the canonical operation label value for profile owner resolution.
	OperationResolveProfileOwner = "resolve_profile_owner"
)

var (
	// offerAsyncBuckets calibrates offer-worker latency observations across fast
	// DB-backed operations, slower external/API work, and AI-assisted generation.
	offerAsyncBuckets = []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}

	// favoriteAsyncBuckets calibrates favorite personalization/recommendation
	// work where normal latency may exceed HTTP request ranges.
	favoriteAsyncBuckets = []float64{0.5, 1, 2.5, 5, 10, 30, 60}

	// notificationAsyncBuckets calibrates email/SMS notification dispatch latency.
	notificationAsyncBuckets = []float64{0.5, 1, 2, 5, 10, 30, 60}

	// profileAsyncBuckets calibrates profile moderation and batch governance work.
	profileAsyncBuckets = []float64{1, 5, 10, 30, 60, 120, 300}
)

var (
	// OfferInsertResult counts curated-offer insert attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - merchant_id: merchant UUID
	//
	// WARNING: merchant_id is high-cardinality.
	OfferInsertResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_insert_total",
			Help: "Curated-offer insert attempts, labelled by result and merchant.",
		},
		[]string{"result", "merchant_id"},
	)

	// OfferOperationDuration measures async offer-operation latency.
	//
	// Labels:
	//   - operation: canonical Operation* value
	OfferOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "offer_operation_duration_seconds",
			Help:    "Async offer operation latency.",
			Buckets: offerAsyncBuckets,
		},
		[]string{"operation"},
	)

	// OfferApprovalResult counts async offer approval attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - editor_id: approver UUID
	//
	// WARNING: editor_id is high-cardinality.
	OfferApprovalResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_approval_total",
			Help: "Curated-offer approval attempts, labelled by result and editor.",
		},
		[]string{"result", "editor_id"},
	)

	// OfferRejectionResult counts async offer rejection attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - editor_id: rejecting editor/moderator UUID
	//
	// WARNING: editor_id is high-cardinality.
	OfferRejectionResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_rejection_total",
			Help: "Curated-offer rejection attempts, labelled by result and editor.",
		},
		[]string{"result", "editor_id"},
	)

	// OfferDescriptionResult counts AI-assisted description generation attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - offer_id: offer UUID
	//
	// WARNING: offer_id is high-cardinality.
	OfferDescriptionResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_description_total",
			Help: "AI-generated offer description attempts, labelled by result and offer.",
		},
		[]string{"result", "offer_id"},
	)

	// OfferFraudScanResult counts async fraud-scan runs.
	//
	// Labels:
	//   - result: success | failure
	OfferFraudScanResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_fraud_scan_total",
			Help: "Fraud-detection attempts for curated offers, labelled by result.",
		},
		[]string{"result"},
	)

	// OfferAutoExpireResult counts async offer auto-expire attempts.
	//
	// Labels:
	//   - result: success | failure
	OfferAutoExpireResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_auto_expire_total",
			Help: "Auto-expire attempts for offers, labelled by result.",
		},
		[]string{"result"},
	)

	// OfferRemovalResult counts async expired-offer removal attempts.
	//
	// Labels:
	//   - result: success | failure
	OfferRemovalResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_removal_total",
			Help: "Expired-offer removal attempts, labelled by result.",
		},
		[]string{"result"},
	)

	// OfferSuggestionResult counts async personalized-offer suggestion runs.
	//
	// Labels:
	//   - result: success | failure
	//   - user_id: target user UUID
	//
	// WARNING: user_id is high-cardinality.
	OfferSuggestionResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_suggestion_total",
			Help: "Personalized offer-suggestion attempts, labelled by result and user.",
		},
		[]string{"result", "user_id"},
	)

	// SuggestionPlanResult counts batches that select users for offer suggestions.
	//
	// Labels:
	//   - result: success | failure
	SuggestionPlanResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "suggestion_plan_total",
			Help: "Batches that plan personalized suggestions, labelled by result.",
		},
		[]string{"result"},
	)

	// OfferStatusUpdateResult counts async offer status-update attempts.
	//
	// Labels:
	//   - result: success | failure
	OfferStatusUpdateResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_status_update_total",
			Help: "Offer status-update attempts, labelled by result.",
		},
		[]string{"result"},
	)

	// OfferFlagResult counts async offer-flag attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - offer_id: offer UUID
	//
	// WARNING: offer_id is high-cardinality.
	OfferFlagResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_flag_total",
			Help: "Offer-flag attempts, labelled by result and offer.",
		},
		[]string{"result", "offer_id"},
	)

	// OfferAutoFlagResult counts batch offer auto-flag operations.
	//
	// Labels:
	//   - result: success | failure
	OfferAutoFlagResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_auto_flag_total",
			Help: "Batches that select offers for automatic flagging, labelled by result.",
		},
		[]string{"result"},
	)

	// OfferBlacklistResult counts async offer blacklist attempts.
	//
	// Labels:
	//   - result: success | failure
	OfferBlacklistResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_blacklist_total",
			Help: "Offer blacklist attempts, labelled by result.",
		},
		[]string{"result"},
	)

	// UserFavoriteOperationDuration measures async favorite-operation latency.
	//
	// Labels:
	//   - operation: canonical Operation* value
	UserFavoriteOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "user_favorite_operation_duration_seconds",
			Help:    "Async user-favorite operation latency.",
			Buckets: favoriteAsyncBuckets,
		},
		[]string{"operation"},
	)

	// UserFavoritePersonalizationResult counts personalized-offer generation attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - user_id: target user UUID
	//
	// WARNING: user_id is high-cardinality.
	UserFavoritePersonalizationResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_personalization_total",
			Help: "Personalized offer generation attempts, labelled by result and user.",
		},
		[]string{"result", "user_id"},
	)

	// UserFavoriteAutoExpireResult counts async favorite auto-expire attempts.
	//
	// Labels:
	//   - result: success | failure
	UserFavoriteAutoExpireResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_auto_expire_total",
			Help: "Auto-expire attempts for user favorites, labelled by result.",
		},
		[]string{"result"},
	)

	// UserFavoriteBatchExpireResult counts batch inactive-favorite expiration.
	//
	// Labels:
	//   - result: success | failure
	UserFavoriteBatchExpireResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_batch_expire_total",
			Help: "Batch expiration attempts for inactive favorites, labelled by result.",
		},
		[]string{"result"},
	)

	// UserFavoritePurgeResult counts permanent purges of soft-deleted favorites.
	//
	// Labels:
	//   - result: success | failure
	UserFavoritePurgeResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_purge_total",
			Help: "Purge attempts for soft-deleted user favorites, labelled by result.",
		},
		[]string{"result"},
	)

	// UserFavoriteRestoreResult counts restore attempts for soft-deleted favorites.
	//
	// Labels:
	//   - result: success | failure
	//   - user_id: user UUID
	//   - offer_id: offer UUID
	//
	// WARNING: compound high-cardinality collector: O(users × offers).
	UserFavoriteRestoreResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_restore_total",
			Help: "Restore attempts for soft-deleted user favorites, labelled by result, user, and offer.",
		},
		[]string{"result", "user_id", "offer_id"},
	)

	// UserNotificationResult counts async notification attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - user_id: recipient user UUID
	//
	// WARNING: user_id is high-cardinality. Never include protected message
	// content, activation URLs, reset URLs, tokens, emails, or phone numbers.
	UserNotificationResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_notification_total",
			Help: "User-notification attempts, labelled by result and user.",
		},
		[]string{"result", "user_id"},
	)

	// UserNotificationOperationDuration measures async notification latency.
	//
	// Labels:
	//   - operation: canonical Operation* value
	UserNotificationOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "user_notification_operation_duration_seconds",
			Help:    "Async user-notification operation latency.",
			Buckets: notificationAsyncBuckets,
		},
		[]string{"operation"},
	)

	// ProfileAutoFlagResult counts profile auto-moderation attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - user_id: profile owner UUID
	//
	// WARNING: user_id is high-cardinality.
	ProfileAutoFlagResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_profile_auto_flag_total",
			Help: "Auto-flag attempts for user profiles, labelled by result and user.",
		},
		[]string{"result", "user_id"},
	)

	// ProfileOperationDuration measures async profile-governance latency.
	//
	// Labels:
	//   - operation: canonical Operation* value
	ProfileOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "user_profile_operation_duration_seconds",
			Help:    "Async user-profile moderation operation latency.",
			Buckets: profileAsyncBuckets,
		},
		[]string{"operation"},
	)

	// ProfileBatchAutoFlagResult counts batch profile auto-flag jobs.
	//
	// Labels:
	//   - result: success | failure
	ProfileBatchAutoFlagResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_profile_auto_flag_batch_total",
			Help: "Batch user-profile auto-flag attempts, labelled by result.",
		},
		[]string{"result"},
	)

	// ProfileHandleValidationResult counts async profile handle-validation attempts.
	//
	// Labels:
	//   - result: success | failure
	ProfileHandleValidationResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_profile_handle_validation_total",
			Help: "User-profile handle-validation attempts, labelled by result.",
		},
		[]string{"result"},
	)

	// ProfileOwnerResolveResult counts async profile owner-resolution attempts.
	//
	// Labels:
	//   - result: success | failure
	//   - user_id: supplied user UUID
	//
	// WARNING: user_id is high-cardinality.
	ProfileOwnerResolveResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_profile_owner_resolve_total",
			Help: "Ownership-resolution attempts for user profiles, labelled by result and user.",
		},
		[]string{"result", "user_id"},
	)
)

func init() {
	prometheus.MustRegister(
		OfferInsertResult,
		OfferOperationDuration,
		OfferApprovalResult,
		OfferRejectionResult,
		OfferDescriptionResult,
		OfferFraudScanResult,
		OfferAutoExpireResult,
		OfferRemovalResult,
		OfferSuggestionResult,
		SuggestionPlanResult,
		OfferStatusUpdateResult,
		OfferFlagResult,
		OfferAutoFlagResult,
		OfferBlacklistResult,
		UserFavoriteOperationDuration,
		UserFavoritePersonalizationResult,
		UserFavoriteAutoExpireResult,
		UserFavoriteBatchExpireResult,
		UserFavoritePurgeResult,
		UserFavoriteRestoreResult,
		UserNotificationResult,
		UserNotificationOperationDuration,
		ProfileAutoFlagResult,
		ProfileOperationDuration,
		ProfileBatchAutoFlagResult,
		ProfileHandleValidationResult,
		ProfileOwnerResolveResult,
	)
}
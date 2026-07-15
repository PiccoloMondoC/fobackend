// sdworkspace/sdbackend/internal/server/cmd/api/metrics.go
package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// OfferModel-Based

	// failedActivations tracks the number of failed activation attempts by user or IP.
	failedActivations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "failed_activation_attempts_total",
			Help: "Total number of failed activation attempts per IP/User.",
		},
		[]string{"identifier"}, // Can be user ID, email, or IP
	)
/*
	// MetricOfferInsertResult tracks insert attempts for curated offers,
	// labeled by result ("success" or "failure") and merchant ID.
	MetricOfferInsertResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_insert_total",
			Help: "Total number of curated offer insert attempts, labeled by result and merchant.",
		},
		[]string{"result", "merchant_id"},
	

	// MetricOfferApprovalResult tracks approval attempts for curated offers,
	// labeled by result ("success" or "failure") and editor (user) ID.
	MetricOfferApprovalResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_approval_total",
			Help: "Total number of curated offer approval attempts, labeled by result and editor.",
		},
		[]string{"result", "editor_id"},
	)

	// MetricOfferRejectionResult tracks rejection attempts for curated offers,
	// labeled by result ("success" or "failure") and editor (user) ID.
	MetricOfferRejectionResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_rejection_result_total",
			Help: "Total number of curated offer rejection attempts, labeled by result and editor.",
		},
		[]string{"result", "editor_id"},
	)

	// MetricOfferOperationDuration tracks durations of async offer operations.
	// Labelled by the operation type (e.g insert, approve, reject, suggest).
	MetricOfferOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "offer_operation_duration_seconds",
			Help:    "Duration of offer operations (insert/approve/reject/suggest).",
			Buckets: prometheus.DefBuckets, // [0.005, 0.01, ..., 10+ seconds]
		},
		[]string{"operation"}, // e.g., "insert", "approve", "reject""
	)

	// MetricMerchantPricingAnalysisResult tracks the outcome of merchant pricing pattern analysis.
	// Labeled by result ("success" or "failure").
	MetricMerchantPricingAnalysisResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "merchant_pricing_analysis_result_total",
			Help: "Total number of merchant pricing analysis runs, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricOfferDescriptionResult tracks the result of AI description generation attempts,
	// labeled by result ("success" or "failure") and offer ID.
	MetricOfferDescriptionResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_description_result_total",
			Help: "Total number of AI-generated description attempts, labeled by result and offer ID.",
		},
		[]string{"result", "offer_id"},
	)

	// MetricOfferFraudScanResult tracks the outcome of fraud detection scans,
	// labeled by result ("success" or "failure").
	MetricOfferFraudScanResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_fraud_scan_result_total",
			Help: "Total number of fraud detection runs, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricOfferAutoExpireResult tracks auto-expire attempts for offers,
	// labeled by result ("success" or "failure").
	MetricOfferAutoExpireResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_auto_expire_result_total",
			Help: "Total number of auto-expire offer attempts, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricOfferRemovalResult tracks expired offer removal attempts,
	// labeled by result ("success" or "failure").
	MetricOfferRemovalResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_removal_result_total",
			Help: "Total number of expired offer removal attempts, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricOfferSuggestionResult tracks suggestion attempts for personalized offers,
	// labeled by result ("success" or "failure") and user ID.
	MetricOfferSuggestionResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_suggestion_result_total",
			Help: "Total number of personalized offer suggestion attempts, labeled by result and user ID.",
		},
		[]string{"result", "user_id"},
	)

	// MetricOfferStatusUpdateResult tracks status update attempts for offers,
	// labeled by result ("success" or "failure").
	MetricOfferStatusUpdateResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_status_update_result_total",
			Help: "Total number of offer status update attempts, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricOfferFlagResult tracks the result of offer flagging attempts,
	// labeled by result ("success" or "failure") and offer ID.
	MetricOfferFlagResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_flag_result_total",
			Help: "Total number of offer flagging attempts, labeled by result and offer ID.",
		},
		[]string{"result", "offer_id"},
	)

	// MetricOfferAutoFlagResult tracks async auto-flag attempts for offers,
	// labeled by result ("success" or "failure").
	MetricOfferAutoFlagResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_auto_flag_result_total",
			Help: "Total number of auto-flag attempts, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricOfferBlacklistResult tracks offer blacklist attempts,
	// labeled by result ("success" or "failure").
	MetricOfferBlacklistResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "offer_blacklist_result_total",
			Help: "Total number of offer blacklist attempts, labeled by result.",
		},
		[]string{"result"},
	)*/

	// UserFavoriteModel-Based
/*
	// MetricUserFavoriteOperationDuration tracks durations of user-favorite async operations.
	MetricUserFavoriteOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "user_favorite_operation_duration_seconds",
			Help:    "Duration of user favorite operations (recommend, personalize).",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// MetricUserFavoriteRecommendationResult tracks the result of recommendation attempts
	// based on shared user favorite patterns (e.g., users who liked similar offers).
	MetricUserFavoriteRecommendationResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_recommendation_result_total",
			Help: "Total number of recommendation attempts based on shared favorite patterns, labeled by result and user ID.",
		},
		[]string{"result", "user_id"},
	)

	// MetricUserFavoritePersonalizationResult tracks the result of personalized offer generation
	// labeled by result ("success" or "failure") and user ID.
	MetricUserFavoritePersonalizationResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_personalization_result_total",
			Help: "Total number of personalized offer generation attempts, labeled by result and user ID.",
		},
		[]string{"result", "user_id"},
	)

	// MetricUserFavoriteAutoExpireResult tracks the result of auto-expiring old favorites,
	// labeled by result ("success" or "failure").
	MetricUserFavoriteAutoExpireResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
		Name: "user_favorite_auto_expire_result_total",
		Help: "Total number of old favorite auto-expire attempts, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricUserFavoriteBatchExpireResult tracks the result of system-wide batch expiration
	// of inactive favorites, labeled by result ("success" or "failure").
	MetricUserFavoriteBatchExpireResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_batch_expire_result_total",
			Help: "Total number of batch expiration attempts for inactive favorites, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricUserFavoritePurgeResult tracks the result of soft-deleted favorite purges,
	// labeled by result ("success" or "failure").
	MetricUserFavoritePurgeResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_purge_result_total",
			Help: "Total number of soft-deleted favorite purge attempts, labeled by result.",
		},
		[]string{"result"},
	)

	// MetricUserFavoriteRestoreResult tracks the outcome of restore attempts for soft-deleted favorites,
	// labeled by result ("success" or "failure"), user ID, and offer ID.
	MetricUserFavoriteRestoreResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_favorite_restore_result_total",
			Help: "Total number of restore attempts for user favorites, labeled by result, user ID, and offer ID.",
		},
		[]string{"result", "user_id", "offer_id"},
	)*/

	// NotificationSettingModel-Based
/*
	// Duration for async user notification tasks
	MetricUserNotificationOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "user_notification_operation_duration_seconds",
			Help:    "Duration of user notification async operations.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// MetricNotificationInsertResult tracks async user notification inserts,
	// labeled by result ("success" or "failure") and user ID.
	MetricNotificationInsertResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_insert_result_total",
			Help: "Total number of user notification insert attempts, labeled by result and user ID.",
		},
		[]string{"result", "user_id"},
	)*/

	// MetricUserNotificationResult tracks the result of NotifyUserAsync operations,
	// labeled by result ("success" or "failure") and user ID.
	MetricUserNotificationResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_notification_result_total",
			Help: "Total number of NotifyUserAsync operations, labeled by result and user ID.",
		},
		[]string{"result", "user_id"},
	)
)

func init() {
	// Register all metrics with Prometheus
	prometheus.MustRegister(
		failedActivations,
		// OfferModel-Based
		//MetricOfferInsertResult,
		//MetricOfferApprovalResult,
		//MetricOfferRejectionResult,
		//MetricOfferOperationDuration,
		//MetricMerchantPricingAnalysisResult,
		//MetricOfferDescriptionResult,
		//MetricOfferFraudScanResult,
		//MetricOfferAutoExpireResult,
		//MetricOfferRemovalResult,
		//MetricOfferSuggestionResult,
		//MetricOfferStatusUpdateResult,
		//MetricOfferFlagResult,
		//MetricOfferAutoFlagResult,
		//MetricOfferBlacklistResult,

		// UserFavoriteModel-Based
		//MetricUserFavoriteOperationDuration,
		//MetricUserFavoriteRecommendationResult,
		//MetricUserFavoritePersonalizationResult,
		//MetricUserFavoriteAutoExpireResult,
		//MetricUserFavoriteBatchExpireResult,
		//MetricUserFavoritePurgeResult,
		//MetricUserFavoriteRestoreResult,

		// NotificationSettingModel-Based
		//MetricUserNotificationOperationDuration,
		//MetricNotificationInsertResult,
		MetricUserNotificationResult,
	)
}

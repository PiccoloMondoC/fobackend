// focodebase/fobackend/internal/server/cmd/api/metrics.go
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

		// NotificationSettingModel-Based
		//MetricUserNotificationOperationDuration,
		//MetricNotificationInsertResult,
		MetricUserNotificationResult,
	)
}

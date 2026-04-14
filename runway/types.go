package runway

import "time"

// TaskStatus represents the status of a Runway task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "PENDING"
	TaskStatusRunning   TaskStatus = "RUNNING"
	TaskStatusSucceeded TaskStatus = "SUCCEEDED"
	TaskStatusFailed    TaskStatus = "FAILED"
	TaskStatusCanceled  TaskStatus = "CANCELED"
)

// ImageToVideoRequest represents a request to create an image-to-video task
type ImageToVideoRequest struct {
	PromptImage string `json:"promptImage"`          // Base64 encoded image or URL
	PromptText  string `json:"promptText,omitempty"`  // Optional text prompt
	Model       string `json:"model,omitempty"`       // Model version (default: gen3a_turbo)
	Duration    int    `json:"duration,omitempty"`     // Duration in seconds (5 or 10)
	Ratio       string `json:"ratio,omitempty"`        // Resolution ratio (e.g., "1280:768", "768:1280")
	Seed        *int   `json:"seed,omitempty"`         // Random seed for reproducibility
	Watermark   *bool  `json:"watermark,omitempty"`    // Whether to include watermark
}

// VideoToVideoRequest represents a request to create a video-to-video task
type VideoToVideoRequest struct {
	PromptVideo string `json:"promptVideo"`           // Base64 encoded video or URL
	PromptText  string `json:"promptText,omitempty"`  // Optional text prompt
	Model       string `json:"model,omitempty"`       // Model version
	Duration    int    `json:"duration,omitempty"`     // Duration in seconds
	Seed        *int   `json:"seed,omitempty"`         // Random seed for reproducibility
	Watermark   *bool  `json:"watermark,omitempty"`    // Whether to include watermark
}

// VideoUpscaleRequest represents a request to upscale a video
type VideoUpscaleRequest struct {
	PromptVideo string `json:"promptVideo"`           // Base64 encoded video or URL
	Model       string `json:"model,omitempty"`       // Upscale model version
}

// TaskResponse represents the response when creating a task
type TaskResponse struct {
	ID     string     `json:"id"`              // Task ID
	Status TaskStatus `json:"status"`          // Current status
	Error  *string    `json:"error,omitempty"` // Error message if failed
}

// TaskStatusResponse represents the response when polling task status
type TaskStatusResponse struct {
	ID             string                 `json:"id"`                       // Task ID
	Status         TaskStatus             `json:"status"`                   // Current status
	Progress       *float64               `json:"progress,omitempty"`       // Progress percentage (0-100)
	Output         []string               `json:"output,omitempty"`         // Output URLs when complete
	Error          *string                `json:"error,omitempty"`          // Error message if failed
	CreatedAt      time.Time              `json:"createdAt"`                // Task creation time
	UpdatedAt      *time.Time             `json:"updatedAt,omitempty"`      // Last update time
	FailureCode    *string                `json:"failureCode,omitempty"`    // Failure code if failed
	FailureMessage *string                `json:"failureMessage,omitempty"` // Failure message if failed
	Metadata       map[string]interface{} `json:"metadata,omitempty"`       // Additional metadata
}

// VideoGenerationResult contains the final result of a video generation task
type VideoGenerationResult struct {
	ID          string                 `json:"id"`                      // Task ID
	Status      TaskStatus             `json:"status"`                  // Final status
	OutputURLs  []string               `json:"outputUrls"`              // Generated video URLs
	Duration    time.Duration          `json:"duration"`                // Total time taken
	Model       string                 `json:"model"`                   // Model used
	Error       *string                `json:"error,omitempty"`         // Error if failed
	FailureCode *string                `json:"failureCode,omitempty"`   // Failure code if failed
	Metadata    map[string]interface{} `json:"metadata,omitempty"`      // Request metadata
}

// RunwayErrorResponse represents an error response from the Runway API
type RunwayErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// PollingConfig configures task polling behavior
type PollingConfig struct {
	MaxAttempts     int           // Maximum polling attempts
	InitialInterval time.Duration // Initial polling interval
	MaxInterval     time.Duration // Maximum polling interval
	Timeout         time.Duration // Overall timeout
}

// DefaultPollingConfig returns the default polling configuration
func DefaultPollingConfig() *PollingConfig {
	return &PollingConfig{
		MaxAttempts:     180,              // 180 attempts (30 min at 10s intervals)
		InitialInterval: 2 * time.Second,  // Start with 2 seconds
		MaxInterval:     10 * time.Second, // Max 10 seconds between polls
		Timeout:         30 * time.Minute, // 30 minute total timeout (allows for queue delays)
	}
}

// UsageMetadata represents metadata to be sent with metering data
type UsageMetadata struct {
	OrganizationID      string                 `json:"organizationId,omitempty"`
	ProductID           string                 `json:"productId,omitempty"`
	TaskType            string                 `json:"taskType,omitempty"`
	Agent               string                 `json:"agent,omitempty"`
	SubscriptionID      string                 `json:"subscriptionId,omitempty"`
	TraceID             string                 `json:"traceId,omitempty"`
	// Distributed tracing fields
	ParentTransactionID string                 `json:"parentTransactionId,omitempty"`
	TraceType           string                 `json:"traceType,omitempty"`
	TraceName           string                 `json:"traceName,omitempty"`
	Environment         string                 `json:"environment,omitempty"`
	Region              string                 `json:"region,omitempty"`
	RetryNumber         *int                   `json:"retryNumber,omitempty"`
	CredentialAlias     string                 `json:"credentialAlias,omitempty"`
	Subscriber          map[string]interface{} `json:"subscriber,omitempty"`
	TaskID              string                 `json:"taskId,omitempty"`
	ResponseQualityScore *float64              `json:"responseQualityScore,omitempty"`
	// Multimodal job identifiers
	VideoJobID          string                 `json:"videoJobId,omitempty"`
	AudioJobID          string                 `json:"audioJobId,omitempty"`
	Custom              map[string]interface{} `json:"custom,omitempty"`
}

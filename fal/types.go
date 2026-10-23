package fal

type FalRequest struct {
	Prompt              string                 `json:"prompt"`
	ImageSize           string                 `json:"image_size,omitempty"`
	NumInferenceSteps   int                    `json:"num_inference_steps,omitempty"`
	GuidanceScale       float64                `json:"guidance_scale,omitempty"`
	NumImages           int                    `json:"num_images,omitempty"`
	Seed                *int                   `json:"seed,omitempty"`
	EnableSafetyChecker bool                   `json:"enable_safety_checker,omitempty"`
	Duration            string                 `json:"duration,omitempty"`     // Video duration: "5" or "10" seconds
	AspectRatio         string                 `json:"aspect_ratio,omitempty"` // Video aspect ratio: "16:9", "9:16", "1:1"
	AdditionalParams    map[string]interface{} `json:"-"`
}

// FalImageResponse represents the response from Fal.ai image generation
type FalImageResponse struct {
	Images         []FalImage `json:"images"`
	Image          *FalImage  `json:"image,omitempty"`
	Seed           int        `json:"seed,omitempty"`
	TimeTaken      float64    `json:"timeTaken,omitempty"`
	HasNSFWContent []bool     `json:"has_nsfw_content,omitempty"`
	Prompt         string     `json:"prompt,omitempty"`
}

// OutputImages returns the produced images, whether the endpoint answers with an `images` list or a single `image`
func (r *FalImageResponse) OutputImages() []FalImage {
	if r == nil {
		return nil
	}
	if len(r.Images) > 0 {
		return r.Images
	}
	if r.Image != nil {
		return []FalImage{*r.Image}
	}
	return nil
}

// FalImage represents a single generated image
type FalImage struct {
	URL         string `json:"url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type,omitempty"`
}

// FalVideoResponse represents the response from Fal.ai video generation
type FalVideoResponse struct {
	Video     FalVideo `json:"video"`
	Prompt    string   `json:"prompt,omitempty"`
	TimeTaken float64  `json:"timeTaken,omitempty"`
}

// FalVideo represents a generated video
type FalVideo struct {
	URL         string  `json:"url"`
	Duration    float64 `json:"duration,omitempty"`
	FPS         float64 `json:"fps,omitempty"`
	NumFrames   float64 `json:"num_frames,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	ContentType string  `json:"content_type,omitempty"`
}

// Seconds returns the length fal reported for the video, read from its duration or from its frame
// count over its frame rate; 0 when fal reported neither.
func (v FalVideo) Seconds() float64 {
	if v.Duration > 0 {
		return v.Duration
	}
	if v.NumFrames > 0 && v.FPS > 0 {
		return v.NumFrames / v.FPS
	}
	return 0
}

// FalAudioResponse represents the response from Fal.ai audio generation
type FalAudioResponse struct {
	Audio      FalAudio `json:"audio"`
	DurationMs float64  `json:"duration_ms,omitempty"`
	Prompt     string   `json:"prompt,omitempty"`
	TimeTaken  float64  `json:"timeTaken,omitempty"`
}

// DurationSeconds returns the audio length, read from the audio file or from the top-level `duration_ms`
func (r *FalAudioResponse) DurationSeconds() float64 {
	if r == nil {
		return 0
	}
	if r.Audio.Duration > 0 {
		return r.Audio.Duration
	}
	return r.DurationMs / 1000
}

// FalAudio represents a generated audio file
type FalAudio struct {
	URL         string  `json:"url"`
	Duration    float64 `json:"duration,omitempty"`
	ContentType string  `json:"content_type,omitempty"`
}

// StreamEvent represents an item produced by a fal streaming response
type StreamEvent struct {
	Data    map[string]interface{}
	Partial bool
	Done    bool
	Error   error
}

// MiddlewareStatus reports the current state of the fal middleware client
type MiddlewareStatus struct {
	Initialized bool   `json:"initialized"`
	Enabled     bool   `json:"enabled"`
	HasConfig   bool   `json:"hasConfig"`
	BaseURL     string `json:"baseUrl,omitempty"`
}

// QueueStatus mirrors the lifecycle states returned by queue.fal.run
type QueueStatus string

const (
	QueueInQueue    QueueStatus = "IN_QUEUE"
	QueueInProgress QueueStatus = "IN_PROGRESS"
	QueueCompleted  QueueStatus = "COMPLETED"
	QueueFailed     QueueStatus = "FAILED"
	QueueError      QueueStatus = "ERROR"
)

// QueueSubmitResponse is the body returned when enqueueing a request
type QueueSubmitResponse struct {
	RequestID   string `json:"request_id"`
	ResponseURL string `json:"response_url,omitempty"`
	StatusURL   string `json:"status_url,omitempty"`
}

// QueueStatusResponse is the body returned when polling status
type QueueStatusResponse struct {
	Status      QueueStatus `json:"status"`
	RequestID   string      `json:"request_id,omitempty"`
	ResponseURL string      `json:"response_url,omitempty"`
}

// FalError represents an error response from Fal.ai
type FalError struct {
	ErrorText string `json:"error"`
	Message   string `json:"message,omitempty"`
	Status    int    `json:"status,omitempty"`
}

// Error implements the error interface
func (e *FalError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.ErrorText
}

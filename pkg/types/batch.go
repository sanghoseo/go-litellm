package types

type BatchCreateRequest struct {
	InputFileID      string `json:"input_file_id"`
	Endpoint         string `json:"endpoint"`
	CompletionWindow string `json:"completion_window"`
	Metadata         any    `json:"metadata,omitempty"`
}

type BatchRequestCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type Batch struct {
	ID               string             `json:"id"`
	Object           string             `json:"object"`
	Endpoint         string             `json:"endpoint"`
	InputFileID      string             `json:"input_file_id"`
	CompletionWindow string             `json:"completion_window"`
	Status           string             `json:"status"`
	CreatedAt        int64              `json:"created_at"`
	CompletedAt      *int64             `json:"completed_at,omitempty"`
	OutputFileID     string             `json:"output_file_id,omitempty"`
	ErrorFileID      string             `json:"error_file_id,omitempty"`
	RequestCounts    BatchRequestCounts `json:"request_counts"`
}

type BatchListResponse struct {
	Object string  `json:"object"`
	Data   []Batch `json:"data"`
}

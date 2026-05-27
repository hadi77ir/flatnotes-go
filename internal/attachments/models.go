package attachments

import "errors"

var (
	ErrInvalidFilename = errors.New("invalid attachment filename")
	ErrNotFound        = errors.New("attachment not found")
	ErrExists          = errors.New("attachment already exists")
)

type CreateResponse struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

type Service interface {
	Create(filename string, data []byte) (CreateResponse, error)
	Open(filename string) (string, error)
}

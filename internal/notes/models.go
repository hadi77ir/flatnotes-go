package notes

import "errors"

var (
	ErrInvalidTitle = errors.New("invalid note title")
	ErrNotFound     = errors.New("note not found")
	ErrExists       = errors.New("note already exists")
)

type Note struct {
	Title        string  `json:"title"`
	Content      *string `json:"content"`
	LastModified float64 `json:"lastModified"`
}

type CreateRequest struct {
	Title   string  `json:"title"`
	Content *string `json:"content"`
}

type UpdateRequest struct {
	NewTitle   *string `json:"newTitle"`
	NewContent *string `json:"newContent"`
}

type SearchResult struct {
	Title             string   `json:"title"`
	LastModified      float64  `json:"lastModified"`
	Score             *float64 `json:"score"`
	TitleHighlights   *string  `json:"titleHighlights"`
	ContentHighlights *string  `json:"contentHighlights"`
	TagMatches        []string `json:"tagMatches"`
}

type Service interface {
	Create(CreateRequest) (Note, error)
	Get(title string) (Note, error)
	Update(title string, data UpdateRequest) (Note, error)
	Delete(title string) error
	Search(term, sort, order string, limit int) ([]SearchResult, error)
	GetTags() ([]string, error)
	Close() error
}

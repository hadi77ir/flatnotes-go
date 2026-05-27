package notes

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/mapping"
	blevesearch "github.com/blevesearch/bleve/v2/search"
	htmlhighlighter "github.com/blevesearch/bleve/v2/search/highlight/highlighter/html"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/hadi77ir/flatnotes-go/internal/storage"
)

const markdownExt = ".md"

var (
	invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*]`)
	tagRE                = regexp.MustCompile(`(?m)(^|\s)#([a-zA-Z0-9_-]+)`)
	codeBlockRE          = regexp.MustCompile("(?s)`{1,3}.*?`{1,3}")
)

type FileSystemService struct {
	fs        storage.FileSystem
	index     bleve.Index
	indexPath string
	log       *slog.Logger
	mu        sync.Mutex
}

type indexDoc struct {
	Filename     string    `json:"filename"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	Tags         []string  `json:"tags"`
	LastModified time.Time `json:"lastModified"`
}

func NewFileSystemService(fileSystem storage.FileSystem, logger *slog.Logger) (*FileSystemService, error) {
	info, err := fileSystem.Stat(".")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", fileSystem.Root())
	}
	svc := &FileSystemService{
		fs:        fileSystem,
		indexPath: filepath.Join(fileSystem.Root(), ".flatnotes", "bleve-v1"),
		log:       logger,
	}
	if err := os.MkdirAll(filepath.Dir(svc.indexPath), 0o755); err != nil {
		return nil, err
	}
	index, err := bleve.Open(svc.indexPath)
	if err != nil {
		if errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
			index, err = bleve.New(svc.indexPath, noteIndexMapping())
		}
	}
	if err != nil {
		return nil, err
	}
	svc.index = index
	if err := svc.SyncIndex(); err != nil {
		_ = index.Close()
		return nil, err
	}
	return svc, nil
}

func (s *FileSystemService) Create(data CreateRequest) (Note, error) {
	title := strings.TrimSpace(data.Title)
	if !validFilename(title) {
		return Note{}, ErrInvalidTitle
	}
	content := valueOrEmpty(data.Content)
	name := filenameFromTitle(title)
	file, err := s.fs.CreateExclusive(name, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Note{}, ErrExists
		}
		return Note{}, err
	}
	if _, err := file.Write([]byte(content)); err != nil {
		_ = file.Close()
		return Note{}, err
	}
	if err := file.Close(); err != nil {
		return Note{}, err
	}
	note, err := s.Get(title)
	if err != nil {
		return Note{}, err
	}
	return note, s.indexNote(note)
}

func (s *FileSystemService) Get(title string) (Note, error) {
	title = strings.TrimSpace(title)
	if !validFilename(title) {
		return Note{}, ErrInvalidTitle
	}
	name := filenameFromTitle(title)
	content, err := s.fs.ReadFile(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Note{}, ErrNotFound
		}
		return Note{}, err
	}
	info, err := s.fs.Stat(name)
	if err != nil {
		return Note{}, err
	}
	text := string(content)
	return Note{Title: title, Content: &text, LastModified: modifiedUnix(info)}, nil
}

func (s *FileSystemService) Update(title string, data UpdateRequest) (Note, error) {
	title = strings.TrimSpace(title)
	if !validFilename(title) {
		return Note{}, ErrInvalidTitle
	}
	oldName := filenameFromTitle(title)
	if _, err := s.fs.Stat(oldName); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Note{}, ErrNotFound
		}
		return Note{}, err
	}
	if data.NewTitle != nil {
		newTitle := strings.TrimSpace(*data.NewTitle)
		if !validFilename(newTitle) {
			return Note{}, ErrInvalidTitle
		}
		newName := filenameFromTitle(newTitle)
		if newName != oldName {
			if _, err := s.fs.Stat(newName); err == nil {
				return Note{}, ErrExists
			}
			if err := s.fs.Rename(oldName, newName); err != nil {
				return Note{}, err
			}
			_ = s.index.Delete(title)
			title = newTitle
			oldName = newName
		}
	}
	if data.NewContent != nil {
		if err := s.fs.WriteFile(oldName, []byte(*data.NewContent), 0o644); err != nil {
			return Note{}, err
		}
	}
	note, err := s.Get(title)
	if err != nil {
		return Note{}, err
	}
	return note, s.indexNote(note)
}

func (s *FileSystemService) Delete(title string) error {
	title = strings.TrimSpace(title)
	if !validFilename(title) {
		return ErrInvalidTitle
	}
	err := s.fs.Remove(filenameFromTitle(title))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return s.index.Delete(title)
}

func (s *FileSystemService) Search(term, sortBy, order string, limit int) ([]SearchResult, error) {
	if err := s.SyncIndex(); err != nil {
		return nil, err
	}
	term = preprocessSearchTerm(strings.TrimSpace(term))
	if term == "" {
		term = "*"
	}
	var searchQuery query.Query
	if term == "*" {
		searchQuery = bleve.NewMatchAllQuery()
	} else {
		searchQuery = bleve.NewQueryStringQuery(term)
	}
	if limit <= 0 {
		limit = 10000
	}
	req := bleve.NewSearchRequestOptions(searchQuery, limit, 0, false)
	req.Fields = []string{"title", "lastModified"}
	req.IncludeLocations = true
	req.Highlight = bleve.NewHighlightWithStyle(htmlhighlighter.Name)
	req.Highlight.AddField("title")
	req.Highlight.AddField("content")
	req.Sort = sortOrder(sortBy, order)
	results, err := s.index.Search(req)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(results.Hits))
	for _, hit := range results.Hits {
		title := hit.ID
		if value, ok := hit.Fields["title"].(string); ok {
			title = value
		}
		lastModified := float64(0)
		if value, ok := hit.Fields["lastModified"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				lastModified = float64(parsed.UnixNano()) / 1e9
			}
		}
		score := hit.Score
		result := SearchResult{Title: title, LastModified: lastModified, Score: &score}
		if sortBy == "title" || sortBy == "lastModified" {
			result.Score = nil
		}
		if fragments := hit.Fragments["title"]; len(fragments) > 0 {
			result.TitleHighlights = &fragments[0]
		}
		if fragments := hit.Fragments["content"]; len(fragments) > 0 {
			joined := strings.Join(fragments, " ... ")
			result.ContentHighlights = &joined
		}
		result.TagMatches = tagMatches(hit)
		out = append(out, result)
	}
	return out, nil
}

func (s *FileSystemService) GetTags() ([]string, error) {
	if err := s.SyncIndex(); err != nil {
		return nil, err
	}
	dict, err := s.index.FieldDict("tags")
	if err != nil {
		return nil, err
	}
	defer dict.Close()
	var tags []string
	for {
		entry, err := dict.Next()
		if err != nil {
			return nil, err
		}
		if entry == nil {
			break
		}
		tags = append(tags, entry.Term)
	}
	sort.Strings(tags)
	return tags, nil
}

func (s *FileSystemService) Close() error {
	if s.index != nil {
		return s.index.Close()
	}
	return nil
}

func (s *FileSystemService) SyncIndex() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.fs.ReadDir(".")
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	batch := s.index.NewBatch()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != markdownExt {
			continue
		}
		title := strings.TrimSuffix(entry.Name(), markdownExt)
		note, err := s.Get(title)
		if err != nil {
			return err
		}
		seen[title] = struct{}{}
		doc := docFromNote(note)
		batch.Index(title, doc)
	}
	req := bleve.NewSearchRequestOptions(bleve.NewMatchAllQuery(), 100000, 0, false)
	req.Fields = []string{"title"}
	current, err := s.index.Search(req)
	if err != nil {
		return err
	}
	for _, hit := range current.Hits {
		if _, ok := seen[hit.ID]; !ok {
			batch.Delete(hit.ID)
		}
	}
	if batch.Size() == 0 {
		return nil
	}
	s.log.Debug("syncing note index", "operations", batch.Size())
	return s.index.Batch(batch)
}

func (s *FileSystemService) indexNote(note Note) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index.Index(note.Title, docFromNote(note))
}

func noteIndexMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultAnalyzer = "en"
	indexMapping.DefaultField = "content"
	indexMapping.IndexDynamic = false
	docMapping := bleve.NewDocumentMapping()
	docMapping.Dynamic = false
	title := bleve.NewTextFieldMapping()
	title.Store = true
	title.DocValues = true
	content := bleve.NewTextFieldMapping()
	content.Store = true
	tags := bleve.NewTextFieldMapping()
	tags.Analyzer = keyword.Name
	tags.Store = true
	tags.DocValues = true
	lastModified := bleve.NewDateTimeFieldMapping()
	lastModified.Store = true
	lastModified.DocValues = true
	filename := bleve.NewKeywordFieldMapping()
	filename.Store = true
	docMapping.AddFieldMappingsAt("filename", filename)
	docMapping.AddFieldMappingsAt("title", title)
	docMapping.AddFieldMappingsAt("content", content)
	docMapping.AddFieldMappingsAt("tags", tags)
	docMapping.AddFieldMappingsAt("lastModified", lastModified)
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func docFromNote(note Note) indexDoc {
	content := valueOrEmpty(note.Content)
	contentNoTags, tags := extractTags(content)
	return indexDoc{
		Filename:     filenameFromTitle(note.Title),
		Title:        note.Title,
		Content:      contentNoTags,
		Tags:         tags,
		LastModified: time.Unix(0, int64(note.LastModified*1e9)).UTC(),
	}
}

func extractTags(content string) (string, []string) {
	withoutCode := codeBlockRE.ReplaceAllString(content, "")
	tagSet := map[string]struct{}{}
	for _, tag := range tagsIn(withoutCode) {
		tagSet[strings.ToLower(tag)] = struct{}{}
	}
	withoutTags := stripTags(content)
	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return withoutTags, tags
}

func preprocessSearchTerm(term string) string {
	var out strings.Builder
	last := 0
	for _, idx := range tagRE.FindAllStringSubmatchIndex(term, -1) {
		tagStart, tagEnd := idx[4], idx[5]
		hashStart := tagStart - 1
		if hashStart < 0 || !tagTerminated(term, tagEnd) {
			continue
		}
		out.WriteString(term[last:hashStart])
		out.WriteString("tags:")
		out.WriteString(term[tagStart:tagEnd])
		last = tagEnd
	}
	if last == 0 {
		return term
	}
	out.WriteString(term[last:])
	return out.String()
}

func tagsIn(content string) []string {
	var tags []string
	for _, idx := range tagRE.FindAllStringSubmatchIndex(content, -1) {
		tagStart, tagEnd := idx[4], idx[5]
		if tagTerminated(content, tagEnd) {
			tags = append(tags, content[tagStart:tagEnd])
		}
	}
	return tags
}

func stripTags(content string) string {
	var out strings.Builder
	last := 0
	for _, idx := range tagRE.FindAllStringSubmatchIndex(content, -1) {
		tagStart, tagEnd := idx[4], idx[5]
		hashStart := tagStart - 1
		if hashStart < 0 || !tagTerminated(content, tagEnd) {
			continue
		}
		out.WriteString(content[last:hashStart])
		last = tagEnd
	}
	if last == 0 {
		return content
	}
	out.WriteString(content[last:])
	return out.String()
}

func tagTerminated(content string, end int) bool {
	if end >= len(content) {
		return true
	}
	switch content[end] {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func sortOrder(sortBy, order string) blevesearch.SortOrder {
	desc := order != "asc"
	switch sortBy {
	case "title":
		return blevesearch.SortOrder{&blevesearch.SortField{Field: "title", Desc: desc}}
	case "lastModified":
		return blevesearch.SortOrder{&blevesearch.SortField{Field: "lastModified", Desc: desc, Type: blevesearch.SortFieldAsDate}}
	default:
		return blevesearch.SortOrder{&blevesearch.SortScore{Desc: desc}}
	}
}

func tagMatches(hit *blevesearch.DocumentMatch) []string {
	terms := map[string]struct{}{}
	if hit.Locations != nil {
		if tagLocations, ok := hit.Locations["tags"]; ok {
			for term := range tagLocations {
				terms[html.UnescapeString(term)] = struct{}{}
			}
		}
	}
	if len(terms) == 0 {
		return nil
	}
	tags := make([]string, 0, len(terms))
	for tag := range terms {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func validFilename(value string) bool {
	return value != "" && !invalidFilenameChars.MatchString(value)
}

func filenameFromTitle(title string) string {
	return title + markdownExt
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func modifiedUnix(info fs.FileInfo) float64 {
	return float64(info.ModTime().UnixNano()) / 1e9
}

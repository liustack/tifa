package photos

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/liustack/tifa"

	"uploads"
)

// BodyLimit lets the requests of this route be as large as upload.limit. The other routes keep
// server.body_limit.
var BodyLimit = uploads.UploadLimit

// ReadTimeout gives a phone on a slow line the time to send a large picture.
const ReadTimeout = 5 * time.Minute

// Photo is one stored picture. ID is also its file name.
type Photo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func Load(c *tifa.Ctx) ([]Photo, error) {
	entries, err := os.ReadDir(uploads.UploadDir)
	if err != nil {
		return nil, err
	}
	// Newest first: the names are random, so sort by modification time.
	sort.Slice(entries, func(i, j int) bool {
		a, _ := entries[i].Info()
		b, _ := entries[j].Info()
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	photos := []Photo{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".title") || e.IsDir() {
			continue
		}
		title, _ := os.ReadFile(filepath.Join(uploads.UploadDir, e.Name()+".title"))
		photos = append(photos, Photo{ID: e.Name(), Title: string(title)})
	}
	return photos, nil
}

// UploadInput is the form: a title and the picture.
type UploadInput struct {
	Title string                `json:"title"`
	File  *multipart.FileHeader `json:"file"`
}

func (in UploadInput) Validate() error {
	fields := tifa.Fields{}
	if strings.TrimSpace(in.Title) == "" {
		fields["title"] = "Give the picture a title."
	}
	if in.File == nil {
		fields["file"] = "Choose a picture."
	}
	return fields.Err()
}

// The kinds of picture the app accepts, by what the bytes say they are.
var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// POST stores the picture. The framework has refused a body over BodyLimit with 413 and run
// Validate before this runs.
func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	file, err := in.File.Open()
	if err != nil {
		return Photo{}, err
	}
	defer file.Close()

	// Decide the type from the first bytes, not from the name or the Content-Type the browser sent.
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Photo{}, err
	}
	ext, ok := extensions[http.DetectContentType(head[:n])]
	if !ok {
		return Photo{}, tifa.Fields{"file": "Only JPEG, PNG, WebP and GIF pictures."}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Photo{}, err
	}

	// The name is ours: random, with the extension we decided. Nothing from the client is in the path.
	id, err := newID()
	if err != nil {
		return Photo{}, err
	}
	name := id + ext
	dst, err := os.OpenFile(filepath.Join(uploads.UploadDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Photo{}, err
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(dst.Name())
		return Photo{}, err
	}
	if err := dst.Close(); err != nil {
		return Photo{}, err
	}
	title := strings.TrimSpace(in.Title)
	if err := os.WriteFile(dst.Name()+".title", []byte(title), 0o644); err != nil {
		return Photo{}, err
	}
	return Photo{ID: name, Title: title}, nil
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
